package tags

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The testdata files were made with ffmpeg; see the -metadata values below.
func TestRead(t *testing.T) {
	for _, path := range []string{
		"testdata/song.flac",
		"testdata/song.ogg",
		"testdata/song.opus",
	} {
		t.Run(path, func(t *testing.T) {
			got, err := Read(path)
			if err != nil {
				t.Fatal(err)
			}
			want := Track{
				Path:   path,
				Genre:  "hip-hop",
				Artist: "Kanye West",
				Album:  "ye [E]",
				Title:  "Ghost Town",
				Track:  6,
				Year:   "2018",
			}
			if got != want {
				t.Errorf("got  %+v\nwant %+v", got, want)
			}
		})
	}
}

func TestWrite(t *testing.T) {
	long := strings.Repeat("x", 70000) // spans several Ogg pages

	for _, name := range []string{"song.flac", "song.ogg", "song.opus"} {
		for _, title := range []string{"New Title", long} {
			t.Run(name, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), name)
				data, _ := os.ReadFile("testdata/" + name)
				os.WriteFile(path, data, 0o644)

				err := Write(path, map[string]string{"TITLE": title, "GENRE": "", "DATE": "1999"})
				if err != nil {
					t.Fatal(err)
				}

				got, err := Read(path)
				if err != nil {
					t.Fatal(err)
				}
				want := Track{Path: path, Artist: "Kanye West", Album: "ye [E]", Title: title, Track: 6, Year: "1999"}
				if got != want {
					t.Errorf("got  %+v\nwant %+v", got, want)
				}
				if filepath.Ext(name) != ".flac" {
					checkOggPages(t, path)
				}
			})
		}
	}
}

// checkOggPages verifies every page's CRC and that sequence numbers count up from 0.
func checkOggPages(t *testing.T, path string) {
	data, _ := os.ReadFile(path)
	r := bytes.NewReader(data)
	for seq := uint32(0); ; seq++ {
		page, err := readPage(r)
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		if got := binary.LittleEndian.Uint32(page[18:]); got != seq {
			t.Fatalf("page %d has sequence number %d", seq, got)
		}
		crc := binary.LittleEndian.Uint32(page[22:])
		setCRC(page)
		if binary.LittleEndian.Uint32(page[22:]) != crc {
			t.Fatalf("page %d has a bad CRC", seq)
		}
	}
}

func TestReadNotAudio(t *testing.T) {
	if _, err := Read("tags.go"); err == nil {
		t.Error("expected error for unsupported file")
	}
	if _, err := Read("testdata/missing.flac"); err == nil {
		t.Error("expected error for missing file")
	}
}
