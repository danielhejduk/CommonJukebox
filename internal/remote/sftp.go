package remote

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/danielhejduk/commonjukebox/internal/library"
)

// SFTP is a Remote on an SSH server.
type SFTP struct {
	ssh  *ssh.Client // nil when the client runs over a plain pipe, as in tests
	c    *sftp.Client
	root string
}

func dialSFTP(u *url.URL, opts Options) (*SFTP, error) {
	if u.User == nil {
		return nil, fmt.Errorf("%s: sftp needs a user, as in sftp://user@%s", u.Redacted(), u.Host)
	}
	host := u.Host
	if u.Port() == "" {
		host = net.JoinHostPort(u.Hostname(), "22")
	}
	hostKey, algos, err := hostKeyCallback(opts.Insecure, host)
	if err != nil {
		return nil, err
	}

	conn, err := ssh.Dial("tcp", host, &ssh.ClientConfig{
		User:              u.User.Username(),
		Auth:              authMethods(opts),
		HostKeyCallback:   hostKey,
		HostKeyAlgorithms: algos,
		Timeout:           30 * time.Second,
	})
	if err != nil {
		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) && len(keyErr.Want) == 0 {
			return nil, fmt.Errorf("%s: host key not in ~/.ssh/known_hosts; connect once with ssh -p %s %s@%s to check and add it", host, cmpPort(u), u.User.Username(), u.Hostname())
		}
		return nil, fmt.Errorf("%s: %w", host, err)
	}
	c, err := sftp.NewClient(conn)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%s: start sftp: %w", host, err)
	}
	s, err := newSFTP(c, u.Path)
	if err != nil {
		_ = c.Close()
		_ = conn.Close()
		return nil, err
	}
	s.ssh = conn
	return s, nil
}

func cmpPort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	return "22"
}

// newSFTP wraps c; an empty root means the directory the server starts in.
func newSFTP(c *sftp.Client, root string) (*SFTP, error) {
	root = strings.TrimSuffix(root, "/")
	if root == "" {
		var err error
		if root, err = c.Getwd(); err != nil {
			return nil, err
		}
	}
	return &SFTP{c: c, root: root}, nil
}

// authMethods tries the SSH agent, then the usual unencrypted keys in
// ~/.ssh, then a password, which is only looked up if the server asks.
func authMethods(opts Options) []ssh.AuthMethod {
	var methods []ssh.AuthMethod
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if conn, err := net.Dial("unix", sock); err == nil {
			methods = append(methods, ssh.PublicKeysCallback(agent.NewClient(conn).Signers))
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		var signers []ssh.Signer
		for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
			data, err := os.ReadFile(filepath.Join(home, ".ssh", name))
			if err != nil {
				continue
			}
			// Encrypted keys are skipped; load them into ssh-agent instead.
			if s, err := ssh.ParsePrivateKey(data); err == nil {
				signers = append(signers, s)
			}
		}
		if len(signers) > 0 {
			methods = append(methods, ssh.PublicKeys(signers...))
		}
	}
	if opts.AskPassword != nil {
		methods = append(methods,
			ssh.RetryableAuthMethod(ssh.PasswordCallback(opts.AskPassword), 1),
			ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
				// Servers that only allow keyboard-interactive still just ask for the password.
				answers := make([]string, len(questions))
				for i := range questions {
					p, err := opts.AskPassword()
					if err != nil {
						return nil, err
					}
					answers[i] = p
				}
				return answers, nil
			}))
	}
	return methods
}

// hostKeyCallback checks host keys against ~/.ssh/known_hosts. It also
// returns the key algorithms known for host, so the server is asked for a
// key we can check rather than whichever type it prefers.
func hostKeyCallback(insecure bool, host string) (ssh.HostKeyCallback, []string, error) {
	if insecure {
		return ssh.InsecureIgnoreHostKey(), nil, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil, err
	}
	cb, err := knownhosts.New(filepath.Join(home, ".ssh", "known_hosts"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, errors.New("~/.ssh/known_hosts is missing; connect once with ssh to add the server's key")
	}
	if err != nil {
		return nil, nil, err
	}
	return cb, knownAlgorithms(cb, host), nil
}

// knownAlgorithms lists the host key algorithms of the keys known for host,
// found by checking a throwaway key and reading what was wanted instead.
func knownAlgorithms(cb ssh.HostKeyCallback, host string) []string {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil
	}
	probe, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return nil
	}
	addr := &net.TCPAddr{IP: net.IPv4zero}
	var keyErr *knownhosts.KeyError
	if !errors.As(cb(host, addr, probe.PublicKey()), &keyErr) {
		return nil
	}
	var algos []string
	for _, k := range keyErr.Want {
		switch t := k.Key.Type(); t {
		case ssh.KeyAlgoRSA:
			// An RSA key signs with any of these; prefer SHA-2.
			algos = append(algos, ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA)
		default:
			algos = append(algos, t)
		}
	}
	return algos
}

func (s *SFTP) full(p string) string {
	return path.Join(s.root, p)
}

func (s *SFTP) List() ([]Entry, error) {
	var entries []Entry
	w := s.c.Walk(s.root)
	for w.Step() {
		if err := w.Err(); err != nil {
			return nil, err
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(w.Path(), s.root), "/")
		info := w.Stat()
		if info.IsDir() {
			if rel != "" && skipDir(rel) {
				w.SkipDir()
			}
			continue
		}
		if info.Mode().IsRegular() && library.IsAudio(rel) {
			entries = append(entries, Entry{Path: rel, Size: info.Size(), ModTime: info.ModTime()})
		}
	}
	return entries, nil
}

func (s *SFTP) Get(p string, w io.Writer) error {
	f, err := s.c.Open(s.full(p))
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteTo(w)
	return err
}

func (s *SFTP) Put(p string, r io.Reader, mtime time.Time) (Entry, error) {
	dst, tmp := s.full(p), s.full(tempName(p))
	if err := s.c.MkdirAll(path.Dir(dst)); err != nil {
		return Entry{}, err
	}
	f, err := s.c.Create(tmp)
	if err != nil {
		return Entry{}, err
	}
	if _, err := f.ReadFrom(r); err != nil {
		_ = f.Close()
		_ = s.c.Remove(tmp)
		return Entry{}, err
	}
	if err := f.Close(); err != nil {
		_ = s.c.Remove(tmp)
		return Entry{}, err
	}
	_ = s.c.Chtimes(tmp, time.Now(), mtime)

	// Plain SFTP rename won't replace a file; the posix extension will.
	if err := s.c.PosixRename(tmp, dst); err != nil {
		if s.c.Remove(dst) != nil || s.c.Rename(tmp, dst) != nil {
			_ = s.c.Remove(tmp)
			return Entry{}, err
		}
	}
	info, err := s.c.Stat(dst)
	if err != nil {
		return Entry{}, err
	}
	return Entry{Path: p, Size: info.Size(), ModTime: info.ModTime()}, nil
}

func (s *SFTP) Remove(p string) error {
	if err := s.c.Remove(s.full(p)); err != nil {
		return err
	}
	// RemoveDirectory fails on a directory that isn't empty, which ends the climb.
	for dir := path.Dir(p); dir != "."; dir = path.Dir(dir) {
		if s.c.RemoveDirectory(s.full(dir)) != nil {
			break
		}
	}
	return nil
}

func (s *SFTP) Close() error {
	err := s.c.Close()
	if s.ssh != nil {
		err = errors.Join(err, s.ssh.Close())
	}
	return err
}
