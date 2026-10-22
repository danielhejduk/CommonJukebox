package remote

import (
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
	"time"
)

// Entry is an audio file on one side of a sync.
type Entry struct {
	Path    string // slash-separated, relative to the root
	Size    int64
	ModTime time.Time
}

// Remote is a tree of audio files that can be listed, read and written.
type Remote interface {
	// List returns every audio file, skipping the .jukebox directory.
	List() ([]Entry, error)
	Get(path string, w io.Writer) error
	// Put writes the file, creating parent directories, without leaving a
	// partial file under path if it fails. It tries to give the file mtime
	// and returns the file as a later List would report it.
	Put(path string, r io.Reader, mtime time.Time) (Entry, error)
	// Remove deletes the file and any parent directories it leaves empty.
	Remove(path string) error
	Close() error
}

// Schemes lists the URL schemes Open accepts.
var Schemes = []string{"ftp", "ftps", "sftp", "file"}

// Options are the connection settings that don't belong in the URL.
type Options struct {
	Password string // for FTP
	// AskPassword gives the SFTP password, and is only called if the server
	// wants one after the SSH agent and keys were tried. Nil means keys only.
	AskPassword func() (string, error)
	Insecure    bool // skip TLS certificate or SSH host key verification
}

// Open connects to the remote at rawURL.
func Open(rawURL string, opts Options) (Remote, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "ftp", "ftps":
		return dialFTP(u, opts)
	case "sftp":
		return dialSFTP(u, opts)
	case "file":
		return NewLocal(u.Path), nil
	}
	return nil, fmt.Errorf("%s: unsupported scheme %q (want %s)", rawURL, u.Scheme, strings.Join(Schemes, ", "))
}

// skipDir reports whether the directory rel (relative to the root) holds
// jukebox's own files rather than music.
func skipDir(rel string) bool {
	return path.Base(rel) == ".jukebox"
}

// tempName is where Put writes before renaming. It doesn't end in an audio
// extension, so a leftover one is never listed.
func tempName(p string) string {
	dir, file := path.Split(p)
	return dir + "." + file + ".jukebox-part"
}
