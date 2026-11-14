package add

import (
	"bytes"
	"cmp"
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
		{"wav to flac", "song.wav", "flac", "hip-hop/Kanye West/ye [E]/06. Ghost Town.flac", true},
		{"wav to opus", "song.wav", "opus", "hip-hop/Kanye West/ye [E]/06. Ghost Town.opus", true},
		{"wav to vorbis", "song.wav", "vorbis", "hip-hop/Kanye West/ye [E]/06. Ghost Town.ogg", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := fixture(t, t.TempDir(), tt.file, nil)
			args := []string{"-b:a", "96k"}
			s := config.Settings{Codec: tt.codec, FFmpegArgs: map[string][]string{tt.codec: args}}
			items, errs := Plan("lib", s, []string{src}, nil)
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
			if !tt.transcode {
				args = nil
			}
			if !slices.Equal(it.FFmpegArgs, args) {
				t.Errorf("FFmpegArgs = %q, want %q", it.FFmpegArgs, args)
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

func TestGuessFromName(t *testing.T) {
	tests := []struct {
		path  string
		track int
		title string
	}{
		{"rip/03 - Yikes.wav", 3, "Yikes"},
		{"03. Yikes.flac", 3, "Yikes"},
		{"03 Yikes.wav", 3, "Yikes"},
		{"03_Yikes.wav", 3, "Yikes"},
		{"03-Yikes.wav", 3, "Yikes"},
		{"3 - All Mine.opus", 3, "All Mine"},
		{"123. Outro.wav", 123, "Outro"},
		{"2-03. Yikes.flac", 3, "Yikes"}, // the library's own name for disc 2, track 3
		{"Yikes.wav", 0, "Yikes"},
		{"1999.wav", 0, "1999"},
		{"1999 - Remaster.wav", 0, "1999 - Remaster"},
		{"03.wav", 0, "03"},
		{"00 - Intro.wav", 0, "00 - Intro"},
		{"No Mistakes v2.wav", 0, "No Mistakes v2"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			track, title := guessFromName(tt.path)
			if track != tt.track || title != tt.title {
				t.Errorf("got %d, %q, want %d, %q", track, title, tt.track, tt.title)
			}
		})
	}
}

// A WAV file often has no tags at all, so the file name is offered instead.
func TestPlanOffersFileName(t *testing.T) {
	dir := t.TempDir()
	data, err := os.ReadFile(filepath.Join("..", "tags", "testdata", "song.wav"))
	if err != nil {
		t.Fatal(err)
	}
	// Cut the file off before its tags, which ffmpeg writes ahead of the audio.
	untagged := filepath.Join(dir, "03 - Yikes.wav")
	if err := os.WriteFile(untagged, data[:bytes.Index(data, []byte("LIST"))], 0o644); err != nil {
		t.Fatal(err)
	}
	if comments, err := tags.Comments(untagged); err != nil || len(comments) > 0 {
		t.Fatalf("fixture has tags %q, error %v", comments, err)
	}

	defaults := map[string]string{}
	ask := func(question, def string) (string, error) {
		name, _, _ := strings.Cut(question, " for ")
		defaults[name] = def
		if def == "" && name != "Track number" { // an empty track number is not asked for again
			return "x", nil
		}
		return def, nil
	}
	items, errs := Plan("lib", config.Settings{Codec: "flac"}, []string{untagged}, ask)
	if len(errs) > 0 || len(items) != 1 {
		t.Fatalf("got %d items, errors %v", len(items), errs)
	}
	if defaults["Title"] != "Yikes" || defaults["Track number"] != "3" {
		t.Errorf("defaults = %v, want title Yikes and track number 3", defaults)
	}
	if want := filepath.Join("lib", "x", "x", "x", "03. Yikes.flac"); items[0].Dest != want {
		t.Errorf("Dest %q, want %q", items[0].Dest, want)
	}
	if got := items[0].Changes; got["TITLE"] != "Yikes" || got["TRACKNUMBER"] != "3" {
		t.Errorf("Changes = %v", got)
	}
}

// Tags that are there win over the file name.
func TestPlanKeepsTagsOverFileName(t *testing.T) {
	dir := t.TempDir()
	src := fixture(t, dir, "song.opus", map[string]string{"GENRE": ""})
	renamed := filepath.Join(dir, "09 - Wrong.opus")
	if err := os.Rename(src, renamed); err != nil {
		t.Fatal(err)
	}
	ask := func(question, def string) (string, error) {
		if strings.HasPrefix(question, "Title") || strings.HasPrefix(question, "Track number") {
			t.Errorf("unexpected question %q", question)
		}
		return cmp.Or(def, "rap"), nil
	}
	items, errs := Plan("lib", config.Settings{Codec: "opus"}, []string{renamed}, ask)
	if len(errs) > 0 || len(items) != 1 {
		t.Fatalf("got %d items, errors %v", len(items), errs)
	}
	if want := filepath.Join("lib", "rap", "Kanye West", "ye [E]", "06. Ghost Town.opus"); items[0].Dest != want {
		t.Errorf("Dest %q, want %q", items[0].Dest, want)
	}
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

func TestExecuteWAV(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	for _, codec := range config.Codecs {
		t.Run(codec, func(t *testing.T) {
			src := fixture(t, t.TempDir(), "song.wav", nil)
			before, err := os.ReadFile(src)
			if err != nil {
				t.Fatal(err)
			}
			items, errs := Plan(t.TempDir(), config.Settings{Codec: codec}, []string{src}, nil)
			if len(errs) > 0 {
				t.Fatal(errs)
			}
			items[0].Changes = map[string]string{"ALBUMARTIST": "Kanye West"} // as if prompted
			if err := Execute(items[0], false); err != nil {
				t.Fatal(err)
			}

			got, err := tags.Comments(items[0].Dest)
			if err != nil {
				t.Fatal(err)
			}
			// Only the tags jukebox reads from the WAV, not ffmpeg's own (ENCODER).
			want := []string{"ARTIST=Kanye West", "DATE=2018", "GENRE=hip-hop", "TITLE=Ghost Town", "ALBUM=ye [E]", "TRACKNUMBER=6", "ALBUMARTIST=Kanye West"}
			if !slices.Equal(got, want) {
				t.Errorf("tags\n got %q\nwant %q", got, want)
			}
			if after, _ := os.ReadFile(src); !bytes.Equal(before, after) {
				t.Error("source was changed")
			}
		})
	}
}

func TestExecuteTranscodeKeepsRepeatedTags(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	src := fixture(t, t.TempDir(), "song.flac", nil)
	comments, err := tags.Comments(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := tags.Replace(src, append(comments, "ARTIST=Ye")); err != nil {
		t.Fatal(err)
	}
	items, errs := Plan(t.TempDir(), config.Settings{Codec: "opus"}, []string{src}, nil)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	if err := Execute(items[0], false); err != nil {
		t.Fatal(err)
	}
	got, err := tags.Comments(items[0].Dest)
	if err != nil {
		t.Fatal(err)
	}
	var artists []string
	for _, c := range got {
		if key, value, _ := strings.Cut(c, "="); strings.EqualFold(key, "ARTIST") {
			artists = append(artists, value)
		}
	}
	if want := []string{"Kanye West", "Ye"}; !slices.Equal(artists, want) {
		t.Errorf("ARTIST = %q, want %q", artists, want)
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
