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

			if err := transcode.Transcode(src, dst, "flac"); err != nil {
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
