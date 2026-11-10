package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/danielhejduk/commonjukebox/internal/library"
	"github.com/danielhejduk/commonjukebox/internal/tags"
)

func TestCompareTracks(t *testing.T) {
	want := []tags.Track{
		{Path: "a1", Artist: "abba", Album: "Gold", Track: 1},
		{Path: "c1", Artist: "Bowie", AlbumArtist: "Various Artists", Album: "Hits", Track: 1},
		{Path: "c2", Artist: "Abba", AlbumArtist: "Various Artists", Album: "Hits", Track: 2},
		{Path: "z1", Artist: "Zappa", Album: "Apostrophe", Disc: 1, Track: 1},
		{Path: "z2", Artist: "Zappa", Album: "Apostrophe", Disc: 1, Track: 2},
		{Path: "z3", Artist: "Zappa", Album: "Apostrophe", Disc: 2, Track: 1},
		{Path: "z4", Artist: "Zappa", Album: "Apostrophe", Disc: 2, Track: 2},
	}
	got := slices.Clone(want)
	slices.Reverse(got)
	slices.SortFunc(got, compareTracks)
	for i := range want {
		if got[i].Path != want[i].Path {
			t.Fatalf("position %d: got %s, want %s", i, got[i].Path, want[i].Path)
		}
	}
}

// Only add takes WAV files; the commands that work on library files don't.
func TestCollectFilesWAV(t *testing.T) {
	dir := t.TempDir()
	wav := filepath.Join(dir, "rip.wav")
	if err := os.WriteFile(wav, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	files, errs := collectFiles(library.ScanSources, []string{dir})
	if len(errs) > 0 || !slices.Equal(files, []string{wav}) {
		t.Errorf("sources: got %v, errors %v, want %v", files, errs, []string{wav})
	}
	files, errs = collectFiles(library.Scan, []string{dir})
	if len(files) > 0 || len(errs) != 1 {
		t.Errorf("library files: got %v, errors %v, want one error", files, errs)
	}
}
