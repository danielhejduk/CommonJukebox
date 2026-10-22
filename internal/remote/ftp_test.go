package remote

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	server "goftp.io/server/v2"
	"goftp.io/server/v2/driver/file"
)

// startServer serves dir over FTP on a free local port and returns its URL.
func startServer(t *testing.T, dir string) string {
	t.Helper()
	if testing.Short() {
		t.Skip("starts an FTP server")
	}
	driver, err := file.NewDriver(dir)
	if err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := server.NewServer(&server.Options{
		Driver:       driver,
		Auth:         &server.SimpleAuth{Name: "user", Password: "secret"},
		Perm:         server.NewSimplePerm("user", "user"),
		Hostname:     "127.0.0.1",
		PassivePorts: "40000-40100",
		Logger:       &server.DiscardLogger{},
	})
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(l)
	t.Cleanup(func() { _ = srv.Shutdown() })
	return fmt.Sprintf("ftp://user@%s/music", l.Addr())
}

func TestFTP(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "music/Old/.jukebox"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"music/Old/a.flac", "music/Old/notes.txt", "music/Old/.jukebox/b.flac"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	u := startServer(t, dir)

	if _, err := Open(u, Options{Password: "wrong"}); err == nil {
		t.Fatal("login with a wrong password succeeded")
	}
	r, err := Open(u, Options{Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	e, err := r.Put("New/Album/c.opus", strings.NewReader("hello"), time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatal(err)
	}
	if e.Size != 5 {
		t.Errorf("Put size = %d, want 5", e.Size)
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
	if i := slices.IndexFunc(entries, func(x Entry) bool { return x.Path == e.Path }); i < 0 || !entries[i].ModTime.Equal(e.ModTime) {
		t.Errorf("List doesn't report the mtime Put returned (%v)", e.ModTime)
	}

	var buf bytes.Buffer
	if err := r.Get("New/Album/c.opus", &buf); err != nil || buf.String() != "hello" {
		t.Errorf("Get = %q, %v", buf.String(), err)
	}

	if err := r.Remove("New/Album/c.opus"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "music/New")); !os.IsNotExist(err) {
		t.Errorf("empty directories left behind: %v", err)
	}
}
