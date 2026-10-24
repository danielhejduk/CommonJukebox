package sync

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/danielhejduk/commonjukebox/internal/remote"
)

// State is a file as one side reported it after the last sync.
type State struct {
	Size    int64
	ModTime time.Time
}

func stateOf(e remote.Entry) State {
	return State{Size: e.Size, ModTime: e.ModTime}
}

// Snapshot records both sides as they were after the last sync, keyed by
// path. Each side is only ever compared with its own record, so clocks and
// timestamp precision don't need to agree between them.
type Snapshot struct {
	Local, Remote map[string]State
}

func NewSnapshot() Snapshot {
	return Snapshot{Local: map[string]State{}, Remote: map[string]State{}}
}

// SnapshotPath returns where the snapshot for the named remote is stored.
func SnapshotPath(root, name string) string {
	return filepath.Join(root, ".jukebox", "sync", name)
}

// LoadSnapshot reads the snapshot for the named remote. A missing file means
// the remote was never synced.
func LoadSnapshot(root, name string) (Snapshot, error) {
	s := NewSnapshot()
	path := SnapshotPath(root, name)
	f, err := os.Open(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return s, nil
	case err != nil:
		return Snapshot{}, err
	}
	defer f.Close()

	// Each line is: side, size, mtime in Unix nanoseconds, path. The path goes
	// last so it may contain tabs.
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		fields := strings.SplitN(sc.Text(), "\t", 4)
		if len(fields) != 4 {
			return Snapshot{}, fmt.Errorf("%s:%d: malformed line", path, n)
		}
		size, err1 := strconv.ParseInt(fields[1], 10, 64)
		nanos, err2 := strconv.ParseInt(fields[2], 10, 64)
		if err1 != nil || err2 != nil {
			return Snapshot{}, fmt.Errorf("%s:%d: malformed line", path, n)
		}
		st := State{Size: size, ModTime: time.Unix(0, nanos)}
		switch fields[0] {
		case "L":
			s.Local[fields[3]] = st
		case "R":
			s.Remote[fields[3]] = st
		default:
			return Snapshot{}, fmt.Errorf("%s:%d: unknown side %q", path, n, fields[0])
		}
	}
	return s, sc.Err()
}

// Save writes the snapshot for the named remote, replacing the old one only
// once the new one is complete.
func (s Snapshot) Save(root, name string) error {
	path := SnapshotPath(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+name+"-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op once renamed

	w := bufio.NewWriter(tmp)
	for _, side := range []struct {
		tag    string
		states map[string]State
	}{{"L", s.Local}, {"R", s.Remote}} {
		for p, st := range side.states {
			fmt.Fprintf(w, "%s\t%d\t%d\t%s\n", side.tag, st.Size, st.ModTime.UnixNano(), p)
		}
	}
	if err := w.Flush(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
