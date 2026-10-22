package remote

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// serveSFTP serves dir over SSH with password login on a free local port and
// returns the server's address and host key.
func serveSFTP(t *testing.T, dir, password string) (string, ssh.PublicKey) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{PasswordCallback: func(_ ssh.ConnMetadata, p []byte) (*ssh.Permissions, error) {
		if string(p) != password {
			return nil, fmt.Errorf("wrong password")
		}
		return nil, nil
	}}
	cfg.AddHostKey(signer)
	// Also offer an ECDSA key, which Go clients prefer, while the test only
	// trusts the ed25519 one, as when known_hosts holds a single key type.
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ecSigner, err := ssh.NewSignerFromKey(ecKey)
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddHostKey(ecSigner)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go serveConn(conn, cfg, dir)
		}
	}()
	return l.Addr().String(), signer.PublicKey()
}

func serveConn(conn net.Conn, cfg *ssh.ServerConfig, dir string) {
	_, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		ch, reqs, err := nc.Accept()
		if err != nil {
			return
		}
		go func() {
			for req := range reqs {
				ok := req.Type == "subsystem" && string(req.Payload[4:]) == "sftp"
				_ = req.Reply(ok, nil)
				if ok {
					srv, err := sftp.NewServer(ch, sftp.WithServerWorkingDirectory(dir))
					if err == nil {
						_ = srv.Serve()
					}
					_ = ch.Close()
				}
			}
		}()
	}
}

// pipeSFTP connects a client to an SFTP server for dir without SSH.
func pipeSFTP(t *testing.T, dir string) *SFTP {
	t.Helper()
	cr, sw := io.Pipe()
	sr, cw := io.Pipe()
	srv, err := sftp.NewServer(struct {
		io.Reader
		io.WriteCloser
	}{sr, sw}, sftp.WithServerWorkingDirectory(dir))
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		_ = srv.Serve()
		sw.Close() // lets the client's Close finish
	}()
	c, err := sftp.NewClientPipe(cr, cw)
	if err != nil {
		t.Fatal(err)
	}
	s, err := newSFTP(c, dir+"/music")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSFTP(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "music/Old/.jukebox"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"music/Old/a.flac", "music/Old/notes.txt", "music/Old/.jukebox/b.flac"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r := pipeSFTP(t, dir)

	mtime := time.Unix(1_700_000_000, 0)
	for _, data := range []string{"hello", "replaced"} { // the second Put overwrites
		e, err := r.Put("New/Album/c.opus", strings.NewReader(data), mtime)
		if err != nil {
			t.Fatal(err)
		}
		if e.Size != int64(len(data)) || !e.ModTime.Equal(mtime) {
			t.Errorf("Put = %+v, want size %d and mtime %v", e, len(data), mtime)
		}
	}

	entries, err := r.List()
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, e := range entries {
		paths = append(paths, e.Path)
	}
	slices.Sort(paths)
	if want := []string{"New/Album/c.opus", "Old/a.flac"}; !slices.Equal(paths, want) {
		t.Errorf("List = %v, want %v", paths, want)
	}

	var buf bytes.Buffer
	if err := r.Get("New/Album/c.opus", &buf); err != nil || buf.String() != "replaced" {
		t.Errorf("Get = %q, %v", buf.String(), err)
	}
	if err := r.Remove("New/Album/c.opus"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "music/New")); !os.IsNotExist(err) {
		t.Errorf("empty directories left behind: %v", err)
	}
}

func TestSFTPDial(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.flac"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	addr, hostKey := serveSFTP(t, dir, "secret")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SSH_AUTH_SOCK", "")
	u := "sftp://me@" + addr + dir
	ask := func(p string) func() (string, error) {
		return func() (string, error) { return p, nil }
	}

	// No known_hosts yet: refused unless insecure.
	if _, err := Open(u, Options{AskPassword: ask("secret")}); err == nil || !strings.Contains(err.Error(), "known_hosts") {
		t.Fatalf("unknown host: err = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	line := fmt.Sprintf("[127.0.0.1]:%s %s", strings.TrimPrefix(addr, "127.0.0.1:"), ssh.MarshalAuthorizedKey(hostKey))
	if err := os.WriteFile(filepath.Join(home, ".ssh", "known_hosts"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Open(u, Options{AskPassword: ask("wrong")}); err == nil {
		t.Fatal("login with a wrong password succeeded")
	}
	r, err := Open(u, Options{AskPassword: ask("secret")})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	entries, err := r.List()
	if err != nil || len(entries) != 1 || entries[0].Path != "a.flac" {
		t.Fatalf("List = %v, %v", entries, err)
	}
}
