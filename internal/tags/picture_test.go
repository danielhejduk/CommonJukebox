package tags

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func testImage(t *testing.T, format string, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		img.Set(x, x%h, color.RGBA{R: 200, A: 255})
	}
	var b bytes.Buffer
	var err error
	if format == "png" {
		err = png.Encode(&b, img)
	} else {
		err = jpeg.Encode(&b, img, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestNewCover(t *testing.T) {
	p, err := NewCover(testImage(t, "png", 30, 20))
	if err != nil {
		t.Fatal(err)
	}
	if p.Type != FrontCover || p.MIME != "image/png" || p.Width != 30 || p.Height != 20 || p.Depth != 32 || p.Ext() != ".png" {
		t.Errorf("png: got %+v", p)
	}

	p, err = NewCover(testImage(t, "jpeg", 30, 20))
	if err != nil {
		t.Fatal(err)
	}
	if p.MIME != "image/jpeg" || p.Width != 30 || p.Height != 20 || p.Depth != 24 || p.Ext() != ".jpg" {
		t.Errorf("jpeg: got %+v", p)
	}

	if _, err := NewCover([]byte("GIF89a not really")); err == nil {
		t.Error("expected an error for a non-JPEG, non-PNG image")
	}
}

func TestPictureRoundTrip(t *testing.T) {
	p := Picture{Type: 4, MIME: "image/png", Description: "back", Width: 1, Height: 2, Depth: 3, Colors: 4, Data: []byte("data")}
	got, err := parsePicture(encodePicture(p))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, p) {
		t.Errorf("got %+v, want %+v", got, p)
	}
	if _, err := parsePicture(encodePicture(p)[:20]); err == nil {
		t.Error("expected an error for a truncated picture")
	}
}

func TestSetCover(t *testing.T) {
	// Big enough to span several Ogg pages.
	front, err := NewCover(testImage(t, "png", 400, 400))
	if err != nil {
		t.Fatal(err)
	}
	back := Picture{Type: 4, MIME: "image/jpeg", Data: testImage(t, "jpeg", 10, 10)}

	for _, name := range []string{"song.flac", "song.ogg", "song.opus"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name)
			data, _ := os.ReadFile("testdata/" + name)
			os.WriteFile(path, data, 0o644)

			if p, err := Cover(path); err != nil || p != nil {
				t.Fatalf("Cover before = %v, %v; want nil", p, err)
			}

			// A back cover added by another program must survive.
			err := modify(path, func(m *meta) { m.pictures = append(m.pictures, back) })
			if err != nil {
				t.Fatal(err)
			}
			if err := SetCover(path, &front); err != nil {
				t.Fatal(err)
			}
			got, err := Cover(path)
			if err != nil {
				t.Fatal(err)
			}
			if got == nil || !reflect.DeepEqual(*got, front) {
				t.Fatalf("Cover = %+v, want %+v", got, front)
			}

			// Tags and pictures don't disturb each other.
			if err := Write(path, map[string]string{"TITLE": "New"}); err != nil {
				t.Fatal(err)
			}
			tr, err := Read(path)
			if err != nil {
				t.Fatal(err)
			}
			if tr.Title != "New" || tr.Artist != "Kanye West" || tr.Art != Sum(front.Data) {
				t.Errorf("Read = %+v", tr)
			}
			comments, err := Comments(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range comments {
				if strings.HasPrefix(c, pictureKey) {
					t.Errorf("Comments includes a picture")
				}
			}

			if err := SetCover(path, nil); err != nil {
				t.Fatal(err)
			}
			if p, err := Cover(path); err != nil || p != nil {
				t.Errorf("Cover after removing = %v, %v; want nil", p, err)
			}
			m, err := readMeta(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(m.pictures) != 1 || !reflect.DeepEqual(m.pictures[0], back) {
				t.Errorf("pictures after removing the front cover = %+v, want only the back cover", m.pictures)
			}
			if filepath.Ext(name) != ".flac" {
				checkOggPages(t, path)
			}
		})
	}
}

// TestCoverFFprobe checks that other programs find the cover we embed.
func TestCoverFFprobe(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}
	front, err := NewCover(testImage(t, "jpeg", 64, 48))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"song.flac", "song.ogg", "song.opus"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name)
			data, _ := os.ReadFile("testdata/" + name)
			os.WriteFile(path, data, 0o644)
			if err := SetCover(path, &front); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v",
				"-show_entries", "stream=codec_name,width,height:stream_disposition=attached_pic",
				"-of", "csv=p=0", path).CombinedOutput()
			if err != nil {
				t.Fatalf("ffprobe: %v: %s", err, out)
			}
			if got := strings.TrimSpace(string(out)); got != "mjpeg,64,48,1" {
				t.Errorf("ffprobe found %q, want mjpeg,64,48,1", got)
			}
		})
	}
}

// TestReadFFmpegCover reads a cover embedded by ffmpeg.
func TestReadFFmpegCover(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	img := filepath.Join(dir, "cover.png")
	os.WriteFile(img, testImage(t, "png", 32, 32), 0o644)
	path := filepath.Join(dir, "song.flac")
	out, err := exec.Command("ffmpeg", "-nostdin", "-loglevel", "error", "-i", "testdata/song.flac", "-i", img,
		"-map", "0:a", "-map", "1", "-c:a", "copy", "-c:v", "copy", "-disposition:v", "attached_pic", path).CombinedOutput()
	if err != nil {
		t.Fatalf("ffmpeg: %v: %s", err, out)
	}
	p, err := Cover(path)
	if err != nil {
		t.Fatal(err)
	}
	if p == nil || p.MIME != "image/png" || p.Width != 32 || p.Height != 32 {
		t.Errorf("Cover = %+v", p)
	}
}
