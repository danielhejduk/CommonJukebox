package remote

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/jlaffaye/ftp"

	"github.com/danielhejduk/commonjukebox/internal/library"
)

// FTP is a Remote on an FTP server, with explicit TLS for ftps:// URLs.
type FTP struct {
	conn *ftp.ServerConn
	root string // absolute directory on the server
}

func dialFTP(u *url.URL, opts Options) (*FTP, error) {
	host := u.Host
	if u.Port() == "" {
		host = net.JoinHostPort(u.Hostname(), "21")
	}
	dialOpts := []ftp.DialOption{ftp.DialWithTimeout(30 * time.Second)}
	if u.Scheme == "ftps" {
		dialOpts = append(dialOpts, ftp.DialWithExplicitTLS(&tls.Config{
			ServerName:         u.Hostname(),
			InsecureSkipVerify: opts.Insecure,
		}))
	}

	conn, err := ftp.Dial(host, dialOpts...)
	if err != nil {
		return nil, err
	}
	user, pass := "anonymous", "anonymous"
	if u.User != nil {
		user, pass = u.User.Username(), opts.Password
	}
	if err := conn.Login(user, pass); err != nil {
		_ = conn.Quit()
		return nil, fmt.Errorf("%s: login as %s: %w", u.Host, user, err)
	}

	// A URL without a path means the directory the server logged us into.
	root := strings.TrimSuffix(u.Path, "/")
	if root == "" {
		if root, err = conn.CurrentDir(); err != nil {
			_ = conn.Quit()
			return nil, err
		}
	}
	return &FTP{conn: conn, root: root}, nil
}

func (f *FTP) full(p string) string {
	return path.Join(f.root, p)
}

func (f *FTP) List() ([]Entry, error) {
	var entries []Entry
	return entries, f.walk("", &entries)
}

// walk appends the audio files under the directory rel to entries.
func (f *FTP) walk(rel string, entries *[]Entry) error {
	list, err := f.conn.List(f.full(rel))
	if err != nil {
		return fmt.Errorf("%s: %w", f.full(rel), err)
	}
	for _, e := range list {
		if e.Name == "." || e.Name == ".." {
			continue
		}
		p := path.Join(rel, e.Name)
		switch {
		case e.Type == ftp.EntryTypeFolder && !skipDir(p):
			if err := f.walk(p, entries); err != nil {
				return err
			}
		case e.Type == ftp.EntryTypeFile && library.IsAudio(e.Name):
			*entries = append(*entries, Entry{Path: p, Size: int64(e.Size), ModTime: e.Time})
		}
	}
	return nil
}

func (f *FTP) Get(p string, w io.Writer) error {
	resp, err := f.conn.Retr(f.full(p))
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, resp); err != nil {
		_ = resp.Close()
		return err
	}
	return resp.Close()
}

func (f *FTP) Put(p string, r io.Reader, mtime time.Time) (Entry, error) {
	// MakeDir fails for directories that exist; a missing one fails Stor below.
	if dir := path.Dir(p); dir != "." {
		parts := strings.Split(dir, "/")
		for i := range parts {
			_ = f.conn.MakeDir(f.full(path.Join(parts[:i+1]...)))
		}
	}

	tmp, dst := f.full(tempName(p)), f.full(p)
	if err := f.conn.Stor(tmp, r); err != nil {
		_ = f.conn.Delete(tmp)
		return Entry{}, err
	}
	if err := f.conn.Rename(tmp, dst); err != nil {
		// Some servers won't rename over an existing file.
		if f.conn.Delete(dst) != nil || f.conn.Rename(tmp, dst) != nil {
			_ = f.conn.Delete(tmp)
			return Entry{}, err
		}
	}
	if f.conn.IsSetTimeSupported() {
		_ = f.conn.SetTime(dst, mtime)
	}
	return f.stat(p)
}

// stat lists p's directory, so the result matches what List will report later.
func (f *FTP) stat(p string) (Entry, error) {
	list, err := f.conn.List(f.full(path.Dir(p)))
	if err != nil {
		return Entry{}, err
	}
	for _, e := range list {
		if e.Name == path.Base(p) {
			return Entry{Path: p, Size: int64(e.Size), ModTime: e.Time}, nil
		}
	}
	return Entry{}, fmt.Errorf("%s: missing after upload", f.full(p))
}

func (f *FTP) Remove(p string) error {
	if err := f.conn.Delete(f.full(p)); err != nil {
		return err
	}
	// RemoveDir fails on a directory that isn't empty, which ends the climb.
	for dir := path.Dir(p); dir != "."; dir = path.Dir(dir) {
		if f.conn.RemoveDir(f.full(dir)) != nil {
			break
		}
	}
	return nil
}

func (f *FTP) Close() error {
	return f.conn.Quit()
}
