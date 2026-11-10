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
		"rip.wav",
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

// WAV files can be added to a library, but are never stored in one.
func TestScanSources(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.flac", "b.ogg", "c.opus", "rip/d.WAV", "rip/._d.WAV", "cover.jpg"} {
		path := filepath.Join(root, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, nil, 0o644)
	}

	got, err := ScanSources(root)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		filepath.Join(root, "a.flac"),
		filepath.Join(root, "b.ogg"),
		filepath.Join(root, "c.opus"),
		filepath.Join(root, "rip/d.WAV"),
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
