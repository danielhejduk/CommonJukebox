package tags

import (
	"bytes"
	"errors"
	"io"
	"slices"
)

// A FLAC file is "fLaC" followed by metadata blocks, then the audio. Each block
// has a 4-byte header: 1 bit "last block" flag, 7 bits type, 24 bits
// big-endian length.
const flacVorbisComment = 4

// readFLAC returns the comments from a FLAC file's VORBIS_COMMENT block.
func readFLAC(r io.Reader) ([]string, error) {
	magic := make([]byte, 4)
	if _, err := io.ReadFull(r, magic); err != nil || string(magic) != "fLaC" {
		return nil, errors.New("not a FLAC file")
	}

	for {
		header := make([]byte, 4)
		if _, err := io.ReadFull(r, header); err != nil {
			return nil, err
		}
		last := header[0]&0x80 != 0
		kind := header[0] & 0x7f
		length := int(header[1])<<16 | int(header[2])<<8 | int(header[3])

		block := make([]byte, length)
		if _, err := io.ReadFull(r, block); err != nil {
			return nil, err
		}
		if kind == flacVorbisComment {
			_, comments, err := parseComments(block)
			return comments, err
		}
		if last {
			return nil, nil // no tags
		}
	}
}

// writeFLAC returns a copy of a FLAC file with its comments replaced by
// update(comments). If the file has no comment block, one is added.
func writeFLAC(data []byte, update func([]string) []string) ([]byte, error) {
	if !bytes.HasPrefix(data, []byte("fLaC")) {
		return nil, errors.New("not a FLAC file")
	}

	type block struct {
		kind byte
		data []byte
	}
	var blocks []block
	pos := 4
	for {
		if pos+4 > len(data) {
			return nil, errors.New("truncated FLAC metadata")
		}
		h := data[pos : pos+4]
		length := int(h[1])<<16 | int(h[2])<<8 | int(h[3])
		pos += 4
		if pos+length > len(data) {
			return nil, errors.New("truncated FLAC metadata")
		}
		blocks = append(blocks, block{h[0] & 0x7f, data[pos : pos+length]})
		pos += length
		if h[0]&0x80 != 0 {
			break
		}
	}
	audio := data[pos:]

	i := slices.IndexFunc(blocks, func(b block) bool { return b.kind == flacVorbisComment })
	vendor, comments := "CommonJukebox", []string(nil)
	if i >= 0 {
		var err error
		if vendor, comments, err = parseComments(blocks[i].data); err != nil {
			return nil, err
		}
	} else {
		// STREAMINFO must stay first, so the new block goes right after it.
		i = 1
		blocks = slices.Insert(blocks, i, block{kind: flacVorbisComment})
	}
	blocks[i].data = encodeComments(vendor, update(comments))
	if len(blocks[i].data) >= 1<<24 {
		return nil, errors.New("tags too large")
	}

	var out bytes.Buffer
	out.WriteString("fLaC")
	for j, b := range blocks {
		h := b.kind
		if j == len(blocks)-1 {
			h |= 0x80
		}
		n := len(b.data)
		out.Write([]byte{h, byte(n >> 16), byte(n >> 8), byte(n)})
		out.Write(b.data)
	}
	out.Write(audio)
	return out.Bytes(), nil
}
