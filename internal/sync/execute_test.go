package sync

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/danielhejduk/commonjukebox/internal/remote"
)

// syncOnce runs a full sync of root against rem and returns the actions
// that changed files.
func syncOnce(t *testing.T, root string, rem remote.Remote) []string {
	t.Helper()
	local := remote.NewLocal(root)
	lf, err := local.List()
	if err != nil {
		t.Fatal(err)
	}
	rf, err := rem.List()
	if err != nil {
		t.Fatal(err)
	}
	snap, err := LoadSnapshot(root, "test")
	if err != nil {
		t.Fatal(err)
	}
	actions, conflicts := Plan(lf, rf, snap, PreferNone)
	if len(conflicts) > 0 {
		t.Fatalf("conflicts: %v", conflicts)
	}
	var done []string
	errs := Execute(root, "test", local, rem, actions, snap, func(a Action) {
		if a.Transfers() {
			done = append(done, a.Kind.String()+" "+a.Path)
		}
	})
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	return done
}

func write(t *testing.T, path, data string, mtime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func listing(t *testing.T, r remote.Remote) []string {
	t.Helper()
	entries, err := r.List()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Path)
	}
	slices.Sort(out)
	return out
}

func TestSyncDirectories(t *testing.T) {
	lib, dev := t.TempDir(), t.TempDir()
	rem := remote.NewLocal(dev)
	old := time.Unix(1_700_000_000, 0)

	write(t, filepath.Join(lib, "A/x/1.flac"), "one", old)
	write(t, filepath.Join(lib, "A/z/2.flac"), "two", old)
	write(t, filepath.Join(dev, "B/y/3.opus"), "three", old)
	write(t, filepath.Join(lib, "cover.jpg"), "not music", old)

	got := syncOnce(t, lib, rem)
	want := []string{"push A/x/1.flac", "push A/z/2.flac", "pull B/y/3.opus"}
	if !slices.Equal(got, want) {
		t.Fatalf("first sync = %v, want %v", got, want)
	}
	if got := syncOnce(t, lib, rem); len(got) != 0 {
		t.Fatalf("second sync = %v, want nothing", got)
	}

	// Change and delete on both sides.
	write(t, filepath.Join(lib, "A/x/1.flac"), "one, retagged", old.Add(time.Hour))
	if err := os.Remove(filepath.Join(lib, "A/z/2.flac")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dev, "B/y/3.opus"), "three, retagged", old.Add(time.Hour))
	write(t, filepath.Join(dev, "C/4.ogg"), "four", old)

	got = syncOnce(t, lib, rem)
	want = []string{"push A/x/1.flac", "delete remote A/z/2.flac", "pull B/y/3.opus", "pull C/4.ogg"}
	if !slices.Equal(got, want) {
		t.Fatalf("third sync = %v, want %v", got, want)
	}
	if got := syncOnce(t, lib, rem); len(got) != 0 {
		t.Fatalf("fourth sync = %v, want nothing", got)
	}

	if l, r := listing(t, remote.NewLocal(lib)), listing(t, rem); !slices.Equal(l, r) {
		t.Fatalf("library %v and remote %v differ", l, r)
	}
	data, err := os.ReadFile(filepath.Join(lib, "B/y/3.opus"))
	if err != nil || string(data) != "three, retagged" {
		t.Fatalf("pulled file = %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(dev, "A/z")); !os.IsNotExist(err) {
		t.Errorf("empty directory left on remote: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dev, ".jukebox")); !os.IsNotExist(err) {
		t.Errorf("snapshot written to remote: %v", err)
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	root := t.TempDir()
	s := NewSnapshot()
	s.Local["a\tb.flac"] = State{Size: 3, ModTime: time.Unix(5, 123)}
	s.Remote["c.flac"] = State{Size: 4, ModTime: time.Unix(6, 0)}
	if err := s.Save(root, "x"); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSnapshot(root, "x")
	if err != nil {
		t.Fatal(err)
	}
	for p, st := range s.Local {
		if g := got.Local[p]; g.Size != st.Size || !g.ModTime.Equal(st.ModTime) {
			t.Errorf("Local[%q] = %v, want %v", p, g, st)
		}
	}
	if g := got.Remote["c.flac"]; g.Size != 4 || len(got.Remote) != 1 {
		t.Errorf("Remote = %v", got.Remote)
	}
}
