package transcode_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danielhejduk/commonjukebox/internal/tags"
	"github.com/danielhejduk/commonjukebox/internal/transcode"
)

func TestFlacTranscode(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}

	for _, name := range []string{"song.flac", "song.ogg", "song.opus"} {
		t.Run(name, func(t *testing.T) {
			src := filepath.Join("..", "tags", "testdata", name)
			dst := filepath.Join(t.TempDir(), "out.flac")

			if err := transcode.Transcode(src, dst, "flac", nil); err != nil {
				t.Fatal(err)
			}

			data, err := os.ReadFile(dst)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(data, []byte("fLaC")) {
				t.Fatalf("%s is not a FLAC file", dst)
			}

			want, err := tags.Read(src)
			if err != nil {
				t.Fatal(err)
			}
			got, err := tags.Read(dst)
			if err != nil {
				t.Fatal(err)
			}
			want.Path = dst
			if got != want {
				t.Errorf("tags not preserved\ngot  %+v\nwant %+v", got, want)
			}
		})
	}
}

func TestTranscodeArgs(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	src := filepath.Join("..", "tags", "testdata", "song.flac")

	for _, tt := range []struct {
		name string
		args []string
		want int // sample rate; 0 means the source's
	}{
		{name: "defaults", args: nil},
		{name: "custom", args: []string{"-ar", "8000"}, want: 8000},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dst := filepath.Join(t.TempDir(), "out.flac")
			if err := transcode.Transcode(src, dst, "flac", tt.args); err != nil {
				t.Fatal(err)
			}
			want := tt.want
			if want == 0 {
				want = flacSampleRate(t, src)
			}
			if got := flacSampleRate(t, dst); got != want {
				t.Errorf("sample rate = %d, want %d", got, want)
			}
		})
	}
}

func TestTranscodeBadArgs(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	src := filepath.Join("..", "tags", "testdata", "song.flac")
	dst := filepath.Join(t.TempDir(), "out.flac")
	if err := transcode.Transcode(src, dst, "flac", []string{"-no-such-option"}); err == nil {
		t.Fatal("bad ffmpeg args did not fail")
	}
}

// flacSampleRate reads the sample rate from the STREAMINFO block, which
// always comes first after the "fLaC" marker and the block header.
func flacSampleRate(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 21 || !bytes.HasPrefix(data, []byte("fLaC")) {
		t.Fatalf("%s is not a FLAC file", path)
	}
	// marker 4, block header 4, block sizes 2+2, frame sizes 3+3, then 20 bits.
	return int(data[18])<<12 | int(data[19])<<4 | int(data[20])>>4
}

func TestCheck(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	for _, tt := range []struct {
		name    string
		codec   string
		args    []string
		wantErr string // empty means no error
	}{
		{name: "built-in encoder", codec: "flac"},
		{name: "unsupported codec", codec: "mp3", wantErr: `unsupported codec "mp3"`},
		{name: "missing encoder", codec: "flac", args: []string{"-c:a", "no_such_encoder"}, wantErr: "no_such_encoder"},
		// ffmpeg.vorbis may pick ffmpeg's own (experimental) encoder over libvorbis.
		{name: "overridden encoder", codec: "vorbis", args: []string{"-c:a", "vorbis", "-strict", "-2"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := transcode.Check(tt.codec, tt.args)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("error = %v, want one mentioning %q", err, tt.wantErr)
			}
		})
	}
}

func TestCheckWithoutFFmpeg(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	err := transcode.Check("opus", nil)
	if err == nil || !strings.Contains(err.Error(), "needs ffmpeg") {
		t.Fatalf("error = %v, want one saying ffmpeg is needed", err)
	}
}
