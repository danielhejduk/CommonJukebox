package main

import (
	"slices"
	"testing"

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
