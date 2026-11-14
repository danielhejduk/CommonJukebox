package transcode_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
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
