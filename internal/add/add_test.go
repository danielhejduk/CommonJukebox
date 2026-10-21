package add

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/danielhejduk/commonjukebox/internal/config"
	"github.com/danielhejduk/commonjukebox/internal/tags"
)

func TestDestPath(t *testing.T) {
	tests := []struct {
		name      string
		track     tags.Track
		multiDisc bool
		want      string
	}{
		{
			name:  "album artist",
			track: tags.Track{Genre: "hip-hop", Artist: "Kid Cudi", AlbumArtist: "Kanye West", Album: "ye [E]", Title: "Ghost Town", Track: 6},
			want:  "hip-hop/Kanye West/ye [E]/06. Ghost Town.opus",
		},
		{
			name:  "artist when no album artist",
			track: tags.Track{Genre: "rock", Artist: "Tool", Album: "Lateralus", Title: "Schism", Track: 7},
			want:  "rock/Tool/Lateralus/07. Schism.opus",
		},
		{
			name:  "three digit track",
			track: tags.Track{Genre: "g", Artist: "a", Album: "b", Title: "t", Track: 123},
			want:  "g/a/b/123. t.opus",
		},
		{
			name:      "multi-disc",
			track:     tags.Track{Genre: "g", Artist: "a", Album: "b", Title: "t", Disc: 2, Track: 3},
			multiDisc: true,
			want:      "g/a/b/2-03. t.opus",
		},
		{
			name:      "multi-disc without disc number",
			track:     tags.Track{Genre: "g", Artist: "a", Album: "b", Title: "t", Track: 3},
			multiDisc: true,
			want:      "g/a/b/1-03. t.opus",
		},
		{
			name:  "sanitized",
			track: tags.Track{Genre: " g ", Artist: "AC/DC", Album: "..hidden", Title: "a\x00b", Track: 1},
			want:  "g/AC_DC/hidden/01. a_b.opus",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := destPath("lib", tt.track, tt.multiDisc, ".opus")
			if want := filepath.Join("lib", tt.want); got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

// fixture copies a test file from the tags package into dir, applying changes.
func fixture(t *testing.T, dir, name string, changes map[string]string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "tags", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if len(changes) > 0 {
		if err := tags.Write(path, changes); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestPlan(t *testing.T) {
	tests := []struct {
		name      string
		file      string
		codec     string
		want      string
		transcode bool
	}{
		{"same codec", "song.opus", "opus", "hip-hop/Kanye West/ye [E]/06. Ghost Town.opus", false},
		{"lossy is not converted", "song.ogg", "flac", "hip-hop/Kanye West/ye [E]/06. Ghost Town.ogg", false},
		{"lossless is converted", "song.flac", "opus", "hip-hop/Kanye West/ye [E]/06. Ghost Town.opus", true},
		{"lossless to vorbis", "song.flac", "vorbis", "hip-hop/Kanye West/ye [E]/06. Ghost Town.ogg", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := fixture(t, t.TempDir(), tt.file, nil)
			items, errs := Plan("lib", config.Settings{Codec: tt.codec}, []string{src}, nil)
			if len(errs) > 0 || len(items) != 1 {
				t.Fatalf("got %d items, errors %v", len(items), errs)
			}
			it := items[0]
			if want := filepath.Join("lib", tt.want); it.Dest != want || it.Track.Path != want {
				t.Errorf("Dest %q, Track.Path %q, want %q", it.Dest, it.Track.Path, want)
			}
			if it.Transcode != tt.transcode {
				t.Errorf("Transcode = %v, want %v", it.Transcode, tt.transcode)
			}
		})
	}
}

func TestPlanMissingTags(t *testing.T) {
	dir := t.TempDir()
	a := fixture(t, filepath.Join(dir), "song.opus", map[string]string{"GENRE": "", "TITLE": ""})
	b := fixture(t, mkdir(t, dir, "b"), "song.ogg", map[string]string{"GENRE": "", "TRACKNUMBER": ""})
	settings := config.Settings{Codec: "opus"}

	t.Run("nobody to ask", func(t *testing.T) {
		items, errs := Plan("lib", settings, []string{a}, nil)
		if len(items) != 0 || len(errs) != 1 || !strings.Contains(errs[0].Error(), "missing-tag: GENRE, TITLE not set") {
			t.Errorf("got items %v, errors %v", items, errs)
		}
	})

	t.Run("asked", func(t *testing.T) {
		var asked []string
		ask := func(question, def string) (string, error) {
			asked = append(asked, question)
			switch {
			case strings.HasPrefix(question, "Genre"):
				return "rap", nil
			case strings.HasPrefix(question, "Album artist"):
				return def, nil
			case strings.HasPrefix(question, "Title"):
				return "Yikes", nil
			case strings.HasPrefix(question, "Track number"):
				if !slices.Contains(asked[:len(asked)-1], question) {
					return "x", nil // not a number, so asked again
				}
				return "3", nil
			}
			return "", nil
		}
		items, errs := Plan("lib", settings, []string{a, b}, ask)
		if len(errs) > 0 || len(items) != 2 {
			t.Fatalf("got %d items, errors %v", len(items), errs)
		}

		wantAsked := []string{
			"Genre for " + dir,
			"Album artist for " + dir,
			"Title for " + a,
			"Genre for " + filepath.Dir(b),
			"Album artist for " + filepath.Dir(b),
			"Track number for " + b,
			"Track number for " + b,
		}
		if !slices.Equal(asked, wantAsked) {
			t.Errorf("asked\n%s\nwant\n%s", strings.Join(asked, "\n"), strings.Join(wantAsked, "\n"))
		}
		if want := filepath.Join("lib", "rap", "Kanye West", "ye [E]", "06. Yikes.opus"); items[0].Dest != want {
			t.Errorf("Dest %q, want %q", items[0].Dest, want)
		}
		if want := filepath.Join("lib", "rap", "Kanye West", "ye [E]", "03. Ghost Town.ogg"); items[1].Dest != want {
			t.Errorf("Dest %q, want %q", items[1].Dest, want)
		}
		if got := items[1].Changes; got["GENRE"] != "rap" || got["ALBUMARTIST"] != "Kanye West" || got["TRACKNUMBER"] != "3" {
			t.Errorf("Changes = %v", got)
		}
	})
}

func mkdir(t *testing.T, parts ...string) string {
	t.Helper()
	dir := filepath.Join(parts...)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestExecute(t *testing.T) {
	src := fixture(t, t.TempDir(), "song.opus", nil)
	before, _ := os.ReadFile(src)
	lib := t.TempDir()
	items, errs := Plan(lib, config.Settings{Codec: "opus"}, []string{src}, nil)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	it := items[0]
	it.Changes["ALBUMARTIST"] = "Kanye West"

	if err := Execute(it, false); err != nil {
		t.Fatal(err)
	}
	got, err := tags.Read(it.Dest)
	if err != nil {
		t.Fatal(err)
	}
	if got.AlbumArtist != "Kanye West" || got.Title != "Ghost Town" {
		t.Errorf("library copy has %+v", got)
	}
	if after, _ := os.ReadFile(src); !bytes.Equal(before, after) {
		t.Error("source file was changed")
	}

	err = Execute(it, false)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("second add without force: got %v", err)
	}
	if err := Execute(it, true); err != nil {
		t.Errorf("second add with force: %v", err)
	}

	entries, _ := os.ReadDir(filepath.Dir(it.Dest))
	if len(entries) != 1 {
		t.Errorf("album directory holds %d entries, want only the track", len(entries))
	}
}

func TestExecuteTranscode(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	src := fixture(t, t.TempDir(), "song.flac", nil)
	items, errs := Plan(t.TempDir(), config.Settings{Codec: "opus"}, []string{src}, nil)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	if err := Execute(items[0], false); err != nil {
		t.Fatal(err)
	}
	got, err := tags.Read(items[0].Dest)
	if err != nil {
		t.Fatal(err)
	}
	if got.Artist != "Kanye West" || got.Album != "ye [E]" || got.Title != "Ghost Town" || got.Track != 6 || got.Genre != "hip-hop" {
		t.Errorf("tags not kept: %+v", got)
	}
}

func TestValidate(t *testing.T) {
	lib := t.TempDir()
	existing := fixture(t, mkdir(t, lib, "hip-hop", "Kanye West", "ye [E]"), "song.opus", map[string]string{"ALBUMARTIST": "Kanye West"})

	// The same album with a different title still uses track 6.
	src := fixture(t, t.TempDir(), "song.ogg", map[string]string{"ALBUMARTIST": "Kanye West", "TITLE": "Other"})
	items, errs := Plan(lib, config.Settings{Codec: "vorbis"}, []string{src}, nil)
	if len(errs) > 0 {
		t.Fatal(errs)
	}

	problems := Validate(items)
	var rules []string
	for _, p := range problems {
		rules = append(rules, p.Rule)
		if p.Path == existing {
			t.Errorf("reported a problem on an existing track: %v", p)
		}
	}
	if !slices.Contains(rules, "duplicate-track") {
		t.Errorf("got %v, want a duplicate-track problem", problems)
	}
}
