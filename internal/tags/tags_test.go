package tags

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The testdata files were made with ffmpeg; see the -metadata values below.
func TestRead(t *testing.T) {
	for _, path := range []string{
		"testdata/song.flac",
		"testdata/song.ogg",
		"testdata/song.opus",
		"testdata/song.wav",
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

				err := Write(path, map[string]string{"TITLE": title, "GENRE": "", "DATE": "1999", "LYRICS": "line one\nline two"})
				if err != nil {
					t.Fatal(err)
				}

				got, err := Read(path)
				if err != nil {
					t.Fatal(err)
				}
				want := Track{Path: path, Artist: "Kanye West", Album: "ye [E]", Title: title, Track: 6, Year: "1999", Lyrics: "line one\nline two"}
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

func TestCopyComments(t *testing.T) {
	multi := []string{"ARTIST=Kanye West", "ARTIST=Lil Baby", "GENRE=Rap", "GENRE=Hip-Hop", "GENRE=Experimental Hip-Hop"}
	for _, name := range []string{"song.flac", "song.ogg", "song.opus"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			src, dst := filepath.Join(dir, "src"+filepath.Ext(name)), filepath.Join(dir, name)
			data, _ := os.ReadFile("testdata/" + name)
			os.WriteFile(src, data, 0o644)
			os.WriteFile(dst, data, 0o644)
			if err := modify(src, func(m *meta) { m.comments = multi }); err != nil {
				t.Fatal(err)
			}

			if err := CopyComments(src, dst); err != nil {
				t.Fatal(err)
			}
			got, err := Comments(dst)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, multi) {
				t.Errorf("got  %q\nwant %q", got, multi)
			}
			if filepath.Ext(name) != ".flac" {
				checkOggPages(t, dst)
			}
		})
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

// riffChunk encodes one RIFF chunk, padded to an even length.
func riffChunk(id string, data []byte) []byte {
	b := binary.LittleEndian.AppendUint32([]byte(id), uint32(len(data)))
	b = append(b, data...)
	if len(data)%2 == 1 {
		b = append(b, 0)
	}
	return b
}

// infoList encodes a LIST/INFO chunk from id, value pairs.
func infoList(fields ...string) []byte {
	body := []byte("INFO")
	for i := 0; i < len(fields); i += 2 {
		body = append(body, riffChunk(fields[i], []byte(fields[i+1]+"\x00"))...)
	}
	return riffChunk("LIST", body)
}

func wavFile(chunks ...[]byte) []byte {
	body := append([]byte("WAVE"), bytes.Join(chunks, nil)...)
	return append(binary.LittleEndian.AppendUint32([]byte("RIFF"), uint32(len(body))), body...)
}

func TestCommentsWAV(t *testing.T) {
	format := riffChunk("fmt ", make([]byte, 16))
	audio := riffChunk("data", make([]byte, 7)) // odd, so a pad byte follows
	info := infoList("INAM", "Ghost Town", "IART", "Kanye West")
	// ffmpeg writes this size when it can't seek back to fill in the real one.
	streamed := append(binary.LittleEndian.AppendUint32([]byte("data"), 0xFFFFFFFF), make([]byte, 8)...)

	tests := []struct {
		name string
		data []byte
		want []string
	}{
		{"info before audio", wavFile(format, info, audio), []string{"TITLE=Ghost Town", "ARTIST=Kanye West"}},
		{"info after audio", wavFile(format, audio, info), []string{"TITLE=Ghost Town", "ARTIST=Kanye West"}},
		{
			"core fields",
			wavFile(format, infoList("INAM", "Yikes", "IART", "Ye", "IPRD", "ye", "IGNR", "rap", "ICRD", "2018-06-01", "ITRK", "2"), audio),
			[]string{"TITLE=Yikes", "ARTIST=Ye", "ALBUM=ye", "GENRE=rap", "DATE=2018-06-01", "TRACKNUMBER=2"},
		},
		{"ffmpeg's track field", wavFile(format, infoList("IPRT", "6")), []string{"TRACKNUMBER=6"}},
		{"first track number wins", wavFile(format, infoList("ITRK", "6", "IPRT", "7")), []string{"TRACKNUMBER=6"}},
		{"other fields are dropped", wavFile(format, infoList("ISFT", "Lavf61.1.100", "INAM", "Yikes", "ICMT", "ripped")), []string{"TITLE=Yikes"}},
		{"value that is not UTF-8 is dropped", wavFile(format, infoList("INAM", "Caf\xe9", "IART", "Ye")), []string{"ARTIST=Ye"}},
		{"blank value is dropped", wavFile(format, infoList("INAM", "  ", "IART", " Ye ")), []string{"ARTIST=Ye"}},
		{"other lists are ignored", wavFile(format, riffChunk("LIST", []byte("adtlINAM\x04\x00\x00\x00abc\x00")), audio), nil},
		{"no tags", wavFile(format, audio), nil},
		{"field cut off by the end of the file", wavFile(format, audio, info)[:len(wavFile(format, audio, info))-4], []string{"TITLE=Ghost Town"}},
		{"chunk header cut off", append(wavFile(format, info), "da"...), []string{"TITLE=Ghost Town", "ARTIST=Kanye West"}},
		{"audio of unknown size", wavFile(format, streamed), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "song.wav")
			if err := os.WriteFile(path, tt.data, 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := Comments(path)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCommentsNotWAV(t *testing.T) {
	for name, data := range map[string][]byte{
		"empty":      nil,
		"other RIFF": []byte("RIFF\x04\x00\x00\x00AVI "),
		"flac":       []byte("fLaC\x00\x00\x00\x00\x00\x00\x00\x00"),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "song.wav")
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := Comments(path); err == nil || !strings.Contains(err.Error(), "not a WAV file") {
				t.Errorf("got error %v, want not a WAV file", err)
			}
		})
	}
}

// WAV files are only read: they are converted when added to a library.
func TestWriteWAV(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.wav")
	data, err := os.ReadFile("testdata/song.wav")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, map[string]string{"TITLE": "New Title"}); err == nil {
		t.Error("expected error for writing WAV tags")
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, data) {
		t.Error("file was changed")
	}
}
