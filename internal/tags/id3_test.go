package tags

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"
)

// synchsafe encodes n as ID3 stores sizes: 4 bytes of 7 bits each.
func synchsafe(n int) []byte {
	return []byte{byte(n >> 21 & 0x7f), byte(n >> 14 & 0x7f), byte(n >> 7 & 0x7f), byte(n & 0x7f)}
}

// id3Frame encodes a frame of an ID3v2.3 or ID3v2.4 tag (version 3 or 4).
// flags are the frame's format flags.
func id3Frame(version byte, id, data string, flags byte) []byte {
	size := binary.BigEndian.AppendUint32(nil, uint32(len(data)))
	if version == 4 {
		size = synchsafe(len(data))
	}
	return slices.Concat([]byte(id), size, []byte{0, flags}, []byte(data))
}

// id3Tag encodes an ID3v2 tag. flags are the tag's header flags.
func id3Tag(version, flags byte, body ...[]byte) []byte {
	b := slices.Concat(body...)
	return slices.Concat([]byte{'I', 'D', '3', version, 0, flags}, synchsafe(len(b)), b)
}

// utf16Text encodes s as UTF-16 in the given byte order, with or without a
// byte order mark.
func utf16Text(order binary.AppendByteOrder, mark bool, s string) string {
	var b []byte
	if mark {
		b = order.AppendUint16(b, 0xfeff)
	}
	for _, u := range utf16.Encode([]rune(s)) {
		b = order.AppendUint16(b, u)
	}
	return string(b)
}

func TestCommentsWAVID3(t *testing.T) {
	format := riffChunk("fmt ", make([]byte, 16))
	audio := riffChunk("data", make([]byte, 7))
	f3 := func(id, data string) []byte { return id3Frame(3, id, data, 0) }
	f4 := func(id, data string) []byte { return id3Frame(4, id, data, 0) }
	id3 := func(tag []byte) []byte { return riffChunk("id3 ", tag) }
	le := utf16Text(binary.LittleEndian, true, "João Luís")
	long := strings.Repeat("é", 100) // 200 bytes: the two size encodings differ from 128 up

	// How the WAV files of a Steam soundtrack are tagged: INFO in Latin-1,
	// and an ID3v2.3 tag with more in it.
	steamInfo := infoList("INAM", "Dark Symphony", "IART", "Jo\xe3o Lu\xeds", "IPRD", "Nocturne Soundtrack",
		"IMUS", "Jo\xe3o Lu\xeds", "ITRK", "1", "ICRD", "2026", "IGNR", "Soundtrack")
	steamID3 := id3(id3Tag(3, 0,
		f3("TIT2", "\x00Dark Symphony\x00"),
		f3("TPE1", "\x01"+le+"\x00\x00"),
		f3("TPE2", "\x01"+le+"\x00\x00"),
		f3("TALB", "\x00Nocturne Soundtrack\x00"),
		f3("TCOM", "\x01"+le+"\x00\x00"),
		f3("TRCK", "\x001/165\x00"),
		f3("TPOS", "\x001/9\x00"),
		f3("TYER", "\x002026\x00"),
		f3("TCON", "\x00Soundtrack\x00"),
		f3("COMM", "\x00eng\x00Mixed by Nimrod Sound"),
		make([]byte, 64), // padding
	))
	steam := []string{
		"TITLE=Dark Symphony", "ARTIST=João Luís", "ALBUMARTIST=João Luís", "ALBUM=Nocturne Soundtrack",
		"TRACKNUMBER=1/165", "DISCNUMBER=1/9", "DATE=2026", "GENRE=Soundtrack",
	}

	unsynchronised := bytes.ReplaceAll(f3("TPE1", "\x01"+le), []byte{0xff, 0xfe}, []byte{0xff, 0x00, 0xfe})

	tests := []struct {
		name   string
		chunks [][]byte
		want   []string
	}{
		{"ID3 after INFO", [][]byte{format, audio, steamInfo, steamID3}, steam},
		{"ID3 ahead of the audio", [][]byte{format, steamID3, steamInfo, audio}, steam},
		{
			"INFO fills in what ID3 lacks",
			[][]byte{format, audio, infoList("INAM", "Old", "IGNR", "rap", "IPRD", "ye"), id3(id3Tag(3, 0, f3("TIT2", "\x00Yikes"), f3("TPOS", "\x002")))},
			[]string{"TITLE=Yikes", "DISCNUMBER=2", "GENRE=rap", "ALBUM=ye"},
		},
		{"upper case chunk id", [][]byte{format, riffChunk("ID3 ", id3Tag(3, 0, f3("TIT2", "\x00Yikes")))}, []string{"TITLE=Yikes"}},
		{
			"ID3v2.4",
			[][]byte{format, id3(id3Tag(4, 0, f4("TIT2", "\x03"+long), f4("TPE1", "\x03Kanye West\x00Ye\x00"), f4("TDRC", "\x032018-06-01")))},
			[]string{"TITLE=" + long, "ARTIST=Kanye West", "ARTIST=Ye", "DATE=2018-06-01"},
		},
		{
			"ID3v2.3 frame of 128 bytes or more",
			[][]byte{format, id3(id3Tag(3, 0, f3("TIT2", "\x03"+long), f3("TALB", "\x00ye")))},
			[]string{"TITLE=" + long, "ALBUM=ye"},
		},
		{
			"only the first value in ID3v2.3",
			[][]byte{format, id3(id3Tag(3, 0, f3("TPE1", "\x00Kanye West\x00Ye")))},
			[]string{"ARTIST=Kanye West"},
		},
		{
			"UTF-16 big endian",
			[][]byte{format, id3(id3Tag(4, 0,
				f4("TPE1", "\x01"+utf16Text(binary.BigEndian, true, "João Luís")),
				f4("TPE2", "\x02"+utf16Text(binary.BigEndian, false, "João Luís")),
			))},
			[]string{"ARTIST=João Luís", "ALBUMARTIST=João Luís"},
		},
		{
			"UTF-16 values each with a byte order mark",
			[][]byte{format, id3(id3Tag(4, 0, f4("TPE1", "\x01"+utf16Text(binary.LittleEndian, true, "Kanye West\x00")+utf16Text(binary.LittleEndian, true, "Ye"))))},
			[]string{"ARTIST=Kanye West", "ARTIST=Ye"},
		},
		{
			"ID3v2.3 extended header",
			[][]byte{format, id3(id3Tag(3, 0x40, []byte{0, 0, 0, 6, 0, 0, 0, 0, 0, 0}, f3("TIT2", "\x00Yikes")))},
			[]string{"TITLE=Yikes"},
		},
		{
			"ID3v2.4 extended header",
			[][]byte{format, id3(id3Tag(4, 0x40, []byte{0, 0, 0, 6, 1, 0}, f4("TIT2", "\x00Yikes")))},
			[]string{"TITLE=Yikes"},
		},
		{
			"unsynchronised ID3v2.3 tag",
			[][]byte{format, id3(id3Tag(3, 0x80, unsynchronised, f3("TALB", "\x00ye")))},
			[]string{"ARTIST=João Luís", "ALBUM=ye"},
		},
		{
			"unsynchronised ID3v2.4 frame",
			[][]byte{format, id3(id3Tag(4, 0x80, id3Frame(4, "TPE1", "\x00\x00\x00\x14\x01\xff\x00\xfeY\x00e\x00", 0x03), f4("TALB", "\x00ye")))},
			[]string{"ARTIST=Ye", "ALBUM=ye"},
		},
		{
			"compressed and encrypted frames are skipped",
			[][]byte{format, id3(id3Tag(3, 0, id3Frame(3, "TIT2", "\x00\x00\x00\x09zzzz", 0x80), f3("TALB", "\x00ye"))),
				id3(id3Tag(4, 0, id3Frame(4, "TPE1", "zzzz", 0x04)))},
			[]string{"ALBUM=ye"},
		},
		{
			"genre number is dropped",
			[][]byte{format, infoList("IGNR", "rap"), id3(id3Tag(3, 0, f3("TCON", "\x00(17)"), f3("TALB", "\x00ye")))},
			[]string{"ALBUM=ye", "GENRE=rap"},
		},
		{"genre number in front of a name is dropped", [][]byte{format, id3(id3Tag(3, 0, f3("TCON", "\x00(17)Rock")))}, []string{"GENRE=Rock"}},
		{"bare genre number is dropped", [][]byte{format, id3(id3Tag(4, 0, f4("TCON", "\x0317")))}, nil},
		{"blank value is dropped", [][]byte{format, id3(id3Tag(3, 0, f3("TIT2", "\x00  \x00"), f3("TALB", "\x00 ye ")))}, []string{"ALBUM=ye"}},
		{
			"value that is not UTF-8 is dropped",
			[][]byte{format, id3(id3Tag(4, 0, f4("TIT2", "\x03Caf\xe9"), f4("TALB", "\x03ye")))},
			[]string{"ALBUM=ye"},
		},
		{
			"frame cut off by the end of the tag",
			[][]byte{format, infoList("IPRD", "ye"), id3(id3Tag(3, 0, f3("TIT2", "\x00Yikes"), f3("TALB", "\x00Nocturne")[:14]))},
			[]string{"TITLE=Yikes", "ALBUM=ye"},
		},
		{"older ID3 version falls back to INFO", [][]byte{format, infoList("INAM", "Yikes"), id3([]byte("ID3\x02\x00\x00\x00\x00\x00\x0aTT2\x00\x00\x04\x00Old"))}, []string{"TITLE=Yikes"}},
		{"chunk that is not ID3 falls back to INFO", [][]byte{format, infoList("INAM", "Yikes"), id3([]byte("nope"))}, []string{"TITLE=Yikes"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "song.wav")
			if err := os.WriteFile(path, wavFile(tt.chunks...), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := Comments(path)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("\n got %q\nwant %q", got, tt.want)
			}
		})
	}
}

// The disc number is what a multi-disc album's file names are built from.
func TestReadWAVID3(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.wav")
	tag := id3Tag(3, 0,
		id3Frame(3, "TPE2", "\x00Kanye West", 0),
		id3Frame(3, "TRCK", "\x0040/165", 0),
		id3Frame(3, "TPOS", "\x002/9", 0),
	)
	if err := os.WriteFile(path, wavFile(riffChunk("fmt ", make([]byte, 16)), riffChunk("id3 ", tag)), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := (Track{Path: path, AlbumArtist: "Kanye West", Disc: 2, Track: 40}); got != want {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}
