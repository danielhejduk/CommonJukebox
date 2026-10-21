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

// numbered returns a complete track at path with the given disc and track number.
func numbered(path string, disc, number int) tags.Track {
	tr := track(path)
	tr.Disc, tr.Track = disc, number
	return tr
}

func TestDuplicateTrack(t *testing.T) {
	tests := []struct {
		name   string
		tracks []tags.Track
		want   []Problem
	}{
		{
			name:   "distinct",
			tracks: []tags.Track{numbered("a/1.flac", 0, 1), numbered("a/2.flac", 0, 2)},
		},
		{
			name:   "duplicate",
			tracks: []tags.Track{numbered("a/1.flac", 0, 1), numbered("a/2.flac", 0, 1)},
			want:   []Problem{{"a/2.flac", "duplicate-track", "track 1 is also used by a/1.flac"}},
		},
		{
			name:   "duplicate on disc",
			tracks: []tags.Track{numbered("a/1.flac", 2, 1), numbered("a/2.flac", 2, 1)},
			want:   []Problem{{"a/2.flac", "duplicate-track", "disc 2 track 1 is also used by a/1.flac"}},
		},
		{
			name:   "same number on other discs",
			tracks: []tags.Track{numbered("a/1.flac", 1, 1), numbered("a/2.flac", 2, 1)},
		},
		{
			name:   "same number in other albums",
			tracks: []tags.Track{numbered("a/1.flac", 0, 1), numbered("b/1.flac", 0, 1)},
		},
		{
			name:   "three times",
			tracks: []tags.Track{numbered("a/1.flac", 0, 1), numbered("a/2.flac", 0, 1), numbered("a/3.flac", 0, 1)},
			want: []Problem{
				{"a/2.flac", "duplicate-track", "track 1 is also used by a/1.flac"},
				{"a/3.flac", "duplicate-track", "track 1 is also used by a/1.flac"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Check(tt.tracks)
			if !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMissingTrack(t *testing.T) {
	tests := []struct {
		name   string
		tracks []tags.Track
		want   []Problem
	}{
		{
			name:   "complete",
			tracks: []tags.Track{numbered("a/2.flac", 0, 2), numbered("a/1.flac", 0, 1)},
		},
		{
			name:   "one missing",
			tracks: []tags.Track{numbered("a/1.flac", 0, 1), numbered("a/3.flac", 0, 3)},
			want:   []Problem{{"a", "missing-track", "track 2 is missing"}},
		},
		{
			name:   "several missing",
			tracks: []tags.Track{numbered("a/2.flac", 0, 2), numbered("a/5.flac", 0, 5)},
			want:   []Problem{{"a", "missing-track", "tracks 1, 3, 4 are missing"}},
		},
		{
			name: "per disc",
			tracks: []tags.Track{
				numbered("a/1-1.flac", 1, 1),
				numbered("a/1-2.flac", 1, 2),
				numbered("a/2-2.flac", 2, 2),
			},
			want: []Problem{{"a", "missing-track", "disc 2 track 1 is missing"}},
		},
		{
			// checkTrack reports the missing number; it is not counted as a gap.
			name:   "no track number",
			tracks: []tags.Track{numbered("a/1.flac", 0, 1), numbered("a/x.flac", 0, 0)},
			want:   []Problem{{"a/x.flac", "missing-tag", "TRACKNUMBER is not set or not a number"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Check(tt.tracks)
			if !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInconsistentTag(t *testing.T) {
	with := func(path string, number int, set func(*tags.Track)) tags.Track {
		tr := numbered(path, 0, number)
		set(&tr)
		return tr
	}
	same := func(*tags.Track) {}

	tests := []struct {
		name   string
		tracks []tags.Track
		want   []Problem
	}{
		{
			name:   "consistent",
			tracks: []tags.Track{numbered("a/1.flac", 0, 1), numbered("a/2.flac", 0, 2)},
		},
		{
			name: "odd one out",
			tracks: []tags.Track{
				with("a/1.flac", 1, same),
				with("a/2.flac", 2, func(t *tags.Track) { t.Album = "OK Computr" }),
				with("a/3.flac", 3, same),
			},
			want: []Problem{{"a/2.flac", "inconsistent-tag", `ALBUM is "OK Computr", other tracks use "OK Computer"`}},
		},
		{
			name: "tie goes to the first value",
			tracks: []tags.Track{
				with("a/1.flac", 1, func(t *tags.Track) { t.Year = "1997" }),
				with("a/2.flac", 2, func(t *tags.Track) { t.Year = "1998" }),
			},
			want: []Problem{{"a/2.flac", "inconsistent-tag", `year is "1998", other tracks use "1997"`}},
		},
		{
			// A blank value is not a different value; missing-tag covers it.
			name: "blank",
			tracks: []tags.Track{
				with("a/1.flac", 1, func(t *tags.Track) { t.Year = "1997" }),
				with("a/2.flac", 2, same),
			},
		},
		{
			name: "other albums",
			tracks: []tags.Track{
				with("a/1.flac", 1, same),
				with("b/1.flac", 1, func(t *tags.Track) { t.AlbumArtist = "Portishead" }),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Check(tt.tracks)
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
