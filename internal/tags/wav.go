package tags

import (
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"unicode/utf8"
)

// A WAV file is "RIFF", a size and "WAVE", followed by chunks. Each chunk has
// a 4-byte id and a little-endian uint32 length, and is padded to an even
// length. Tags are in a "LIST" chunk whose data starts with "INFO" and holds
// chunks of its own: the id names the tag, the data is its value, ending in a
// NUL byte.
//
// Some programs also put an ID3v2 tag in an "id3 " chunk, which can say more:
// INFO has no album artist or disc number, and doesn't record its encoding.
//
// wavInfoKeys maps INFO ids to Vorbis comment keys. Other ids are dropped.
var wavInfoKeys = map[string]string{
	"INAM": "TITLE",
	"IART": "ARTIST",
	"IPRD": "ALBUM",
	"IGNR": "GENRE",
	"ICRD": "DATE",
	"ITRK": "TRACKNUMBER",
	"IPRT": "TRACKNUMBER", // what ffmpeg writes
}

// readWAV returns the tags of a WAV file as Vorbis comments: those of its ID3
// tag, then the INFO tags that the ID3 tag doesn't have. The first INFO value
// of a key wins, and one that isn't UTF-8 is dropped rather than guessed at.
// A file that is cut off gives the tags found up to that point.
func readWAV(r io.ReadSeeker) ([]string, error) {
	header := make([]byte, 12)
	if _, err := io.ReadFull(r, header); err != nil || string(header[:4]) != "RIFF" || string(header[8:]) != "WAVE" {
		return nil, errors.New("not a WAV file")
	}

	var id3, info []string
	seen := map[string]bool{}
	for {
		if _, err := io.ReadFull(r, header[:8]); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return nil, err
		}
		id := string(header[:4])
		size := int64(binary.LittleEndian.Uint32(header[4:]))
		size += size & 1 // padding

		// The audio is far too large to read just to get past it.
		isID3 := strings.EqualFold(id, "id3 ")
		if id != "LIST" && !isID3 {
			if _, err := r.Seek(size, io.SeekCurrent); err != nil {
				return nil, err
			}
			continue
		}
		// The size is whatever the file claims, so don't allocate it up front.
		list, err := io.ReadAll(io.LimitReader(r, size))
		if err != nil {
			return nil, err
		}
		if isID3 {
			if id3 == nil {
				id3 = parseID3(list)
			}
			continue
		}
		if len(list) < 4 || string(list[:4]) != "INFO" {
			continue
		}

		for b := list[4:]; len(b) >= 8; {
			id, n := string(b[:4]), uint64(binary.LittleEndian.Uint32(b[4:]))
			b = b[8:]
			if n > uint64(len(b)) {
				break
			}
			value, _, _ := strings.Cut(string(b[:n]), "\x00")
			value = strings.TrimSpace(value)
			b = b[min(n+n&1, uint64(len(b))):]

			key := wavInfoKeys[id]
			if key == "" || value == "" || seen[key] || !utf8.ValidString(value) {
				continue
			}
			seen[key] = true
			info = append(info, key+"="+value)
		}
	}

	inID3 := map[string]bool{}
	for _, c := range id3 {
		key, _, _ := strings.Cut(c, "=")
		inID3[key] = true
	}
	comments := id3
	for _, c := range info {
		if key, _, _ := strings.Cut(c, "="); !inID3[key] {
			comments = append(comments, c)
		}
	}
	return comments, nil
}
