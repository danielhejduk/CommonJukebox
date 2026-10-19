package library

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestScan(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{
		"a.flac",
		"b.OGG",
		"Artist/Album/1. Song.opus",
		"cover.jpg",
		"notes.txt",
	} {
		path := filepath.Join(root, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, nil, 0o644)
	}

	got, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		filepath.Join(root, "Artist/Album/1. Song.opus"),
		filepath.Join(root, "a.flac"),
		filepath.Join(root, "b.OGG"),
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
