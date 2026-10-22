package remote

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/danielhejduk/commonjukebox/internal/library"
)

// Local is a Remote backed by a directory, such as the library itself or a
// mounted SD card.
type Local struct {
	Root string
}

func NewLocal(root string) *Local {
	return &Local{Root: root}
}

func (l *Local) full(p string) string {
	return filepath.Join(l.Root, filepath.FromSlash(p))
}

func (l *Local) List() ([]Entry, error) {
	var entries []Entry
	err := filepath.WalkDir(l.Root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(l.Root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "." && skipDir(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !library.IsAudio(p) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		entries = append(entries, Entry{Path: rel, Size: info.Size(), ModTime: info.ModTime()})
		return nil
	})
	return entries, err
}

func (l *Local) Get(p string, w io.Writer) error {
	f, err := os.Open(l.full(p))
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}

func (l *Local) Put(p string, r io.Reader, mtime time.Time) (Entry, error) {
	dst := l.full(p)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return Entry{}, err
	}
	tmp := l.full(tempName(p))
	f, err := os.Create(tmp)
	if err != nil {
		return Entry{}, err
	}
	defer os.Remove(tmp) // no-op once renamed

	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		return Entry{}, err
	}
	if err := f.Close(); err != nil {
		return Entry{}, err
	}
	if err := os.Chtimes(tmp, time.Now(), mtime); err != nil {
		return Entry{}, err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return Entry{}, err
	}
	info, err := os.Stat(dst)
	if err != nil {
		return Entry{}, err
	}
	return Entry{Path: p, Size: info.Size(), ModTime: info.ModTime()}, nil
}

func (l *Local) Remove(p string) error {
	if err := os.Remove(l.full(p)); err != nil {
		return err
	}
	// Remove fails on a directory that isn't empty, which ends the climb.
	for dir := filepath.Dir(l.full(p)); dir != filepath.Clean(l.Root); dir = filepath.Dir(dir) {
		if os.Remove(dir) != nil {
			break
		}
	}
	return nil
}

func (l *Local) Close() error { return nil }
