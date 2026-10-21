package check

import (
	"slices"
	"testing"

	"github.com/danielhejduk/commonjukebox/internal/tags"
)

// track returns a track with every checked tag set.
func track(path string) tags.Track {
	return tags.Track{
		Path:        path,
		Artist:      "Radiohead",
		AlbumArtist: "Radiohead",
		Album:       "OK Computer",
		Title:       "Airbag",
		Track:       1,
	}
}

func TestCheck(t *testing.T) {
	tests := []struct {
		name  string
		track func(*tags.Track)
		want  []Problem
	}{
		{name: "complete", track: func(*tags.Track) {}},
		{
			name:  "missing artist",
			track: func(t *tags.Track) { t.Artist = "" },
			want:  []Problem{{"a/1.flac", "missing-tag", "ARTIST is not set"}},
		},
		{
			name:  "blank title",
			track: func(t *tags.Track) { t.Title = "  " },
			want:  []Problem{{"a/1.flac", "missing-tag", "TITLE is not set"}},
		},
		{
			name:  "missing track number",
			track: func(t *tags.Track) { t.Track = 0 },
			want:  []Problem{{"a/1.flac", "missing-tag", "TRACKNUMBER is not set or not a number"}},
		},
		{
			name:  "missing album artist",
			track: func(t *tags.Track) { t.AlbumArtist = "" },
			want:  []Problem{{"a/1.flac", "missing-tag", "ALBUMARTIST is not set"}},
		},
		{
			// Album checks skip tracks without an album, so ALBUMARTIST is not reported.
			name:  "missing album",
			track: func(t *tags.Track) { t.Album, t.AlbumArtist = "", "" },
			want:  []Problem{{"a/1.flac", "missing-tag", "ALBUM is not set"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := track("a/1.flac")
			tt.track(&tr)
			got := Check([]tags.Track{tr})
			if !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGroupAlbums(t *testing.T) {
	noAlbum := track("c/1.flac")
	noAlbum.Album = ""
	got := groupAlbums([]tags.Track{
		track("b/1.flac"),
		track("a/1.flac"),
		noAlbum,
		track("b/2.flac"),
	})

	want := []struct {
		dir   string
		paths []string
	}{
		{"a", []string{"a/1.flac"}},
		{"b", []string{"b/1.flac", "b/2.flac"}},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d albums, want %d: %v", len(got), len(want), got)
	}
	for i, a := range got {
		var paths []string
		for _, tr := range a.Tracks {
			paths = append(paths, tr.Path)
		}
		if a.Dir != want[i].dir || !slices.Equal(paths, want[i].paths) {
			t.Errorf("album %d = %s %v, want %s %v", i, a.Dir, paths, want[i].dir, want[i].paths)
		}
	}
}

func TestProblemString(t *testing.T) {
	p := Problem{"a/1.flac", "missing-tag", "TITLE is not set"}
	if got, want := p.String(), "a/1.flac: missing-tag: TITLE is not set"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
