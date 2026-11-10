package tags

import (
	"bytes"
	"encoding/binary"
	"regexp"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// An ID3v2 tag is a 10-byte header ("ID3", version, revision, flags, size)
// followed by frames. Each frame has a 10-byte header of its own (4-byte id,
// size, 2 bytes of flags) and, for the text frames read here, a byte naming
// the encoding followed by the text. Sizes are big-endian; the tag's size
// and, from version 2.4 on, the frames' sizes use only 7 bits of each byte.
//
// id3Keys maps frame ids to Vorbis comment keys. Other frames are dropped.
var id3Keys = map[string]string{
	"TIT2": "TITLE",
	"TPE1": "ARTIST",
	"TPE2": "ALBUMARTIST",
	"TALB": "ALBUM",
	"TCON": "GENRE",
	"TRCK": "TRACKNUMBER",
	"TPOS": "DISCNUMBER",
	"TYER": "DATE", // version 2.3
	"TDRC": "DATE", // version 2.4
}

// id3Genre matches a reference to the numbered genre list of ID3v1, which
// version 2.3 puts in front of a genre: "(17)Rock".
var id3Genre = regexp.MustCompile(`^(\(\d+\))+`)

// parseID3 returns the tags of an ID3v2.3 or ID3v2.4 tag as Vorbis comments.
// Anything it can't read is left out: an older version or a broken tag gives
// no tags rather than an error.
func parseID3(b []byte) []string {
	if len(b) < 10 || string(b[:3]) != "ID3" || b[3] != 3 && b[3] != 4 {
		return nil
	}
	v4, flags, size := b[3] == 4, b[5], id3Size(b[6:], true)
	b = b[10:]
	b = b[:min(size, len(b))]

	// Before version 2.4 the tag is unsynchronised as a whole, after it frame
	// by frame.
	if !v4 && flags&0x80 != 0 {
		b = id3Unsync(b)
	}
	if flags&0x40 != 0 { // extended header
		if len(b) < 4 {
			return nil
		}
		n := id3Size(b, v4)
		if !v4 {
			n += 4 // the size doesn't count itself
		}
		if n > len(b) {
			return nil
		}
		b = b[n:]
	}

	var comments []string
	seen := map[string]bool{}
	for len(b) >= 10 && b[0] != 0 { // zero bytes pad the tag
		id, n, format := string(b[:4]), id3Size(b[4:], v4), b[9]
		b = b[10:]
		if n > len(b) {
			break
		}
		data := b[:n]
		b = b[n:]

		key := id3Keys[id]
		if key == "" || seen[key] {
			continue
		}
		if v4 {
			if format&0x4c != 0 { // grouped, compressed or encrypted
				continue
			}
			if format&0x01 != 0 { // the data starts with its length
				if len(data) < 4 {
					continue
				}
				data = data[4:]
			}
			if format&0x02 != 0 {
				data = id3Unsync(data)
			}
		} else if format != 0 { // compressed, encrypted or grouped
			continue
		}

		for _, value := range id3Text(data, v4) {
			value = strings.TrimSpace(value)
			if key == "GENRE" {
				// A number alone would need the genre list to mean anything.
				value = strings.TrimSpace(id3Genre.ReplaceAllString(value, ""))
				if strings.Trim(value, "0123456789") == "" {
					continue
				}
			}
			if value == "" {
				continue
			}
			seen[key] = true
			comments = append(comments, key+"="+value)
		}
	}
	return comments
}

// id3Size reads a 4-byte size, which uses 7 bits of each byte if synchsafe.
func id3Size(b []byte, synchsafe bool) int {
	if synchsafe {
		return int(b[0]&0x7f)<<21 | int(b[1]&0x7f)<<14 | int(b[2]&0x7f)<<7 | int(b[3]&0x7f)
	}
	return int(binary.BigEndian.Uint32(b) & 0x7fffffff)
}

// id3Unsync undoes unsynchronisation, which puts a zero byte after every 0xff
// so that players don't mistake the tag for audio.
func id3Unsync(b []byte) []byte {
	return bytes.ReplaceAll(b, []byte{0xff, 0}, []byte{0xff})
}

// id3Text decodes the values of a text frame. Version 2.4 separates several
// values with a NUL; before that, a NUL ends the text.
func id3Text(b []byte, v4 bool) []string {
	if len(b) == 0 {
		return nil
	}
	var s string
	encoding, b := b[0], b[1:]
	switch encoding {
	case 0: // ISO 8859-1
		runes := make([]rune, len(b))
		for i, c := range b {
			runes[i] = rune(c)
		}
		s = string(runes)
	case 1, 2: // UTF-16 with a byte order mark, UTF-16 big endian without
		var order binary.ByteOrder = binary.BigEndian
		if encoding == 1 && len(b) >= 2 && b[0] == 0xff && b[1] == 0xfe {
			order = binary.LittleEndian
		}
		units := make([]uint16, len(b)/2)
		for i := range units {
			units[i] = order.Uint16(b[2*i:])
		}
		// Every value can start with a byte order mark.
		s = strings.ReplaceAll(string(utf16.Decode(units)), "\ufeff", "")
	case 3: // UTF-8
		if !utf8.Valid(b) {
			return nil
		}
		s = string(b)
	default:
		return nil
	}

	values := strings.Split(s, "\x00")
	if !v4 {
		values = values[:1]
	}
	return values
}
