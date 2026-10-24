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
const (
	flacVorbisComment = 4
	flacPicture       = 6
)

// readFLAC returns the comments from a FLAC file's VORBIS_COMMENT block and
// the pictures from its PICTURE blocks.
func readFLAC(r io.Reader) (meta, error) {
	magic := make([]byte, 4)
	if _, err := io.ReadFull(r, magic); err != nil || string(magic) != "fLaC" {
		return meta{}, errors.New("not a FLAC file")
	}

	var m meta
	for {
		header := make([]byte, 4)
		if _, err := io.ReadFull(r, header); err != nil {
			return meta{}, err
		}
		last := header[0]&0x80 != 0
		kind := header[0] & 0x7f
		length := int(header[1])<<16 | int(header[2])<<8 | int(header[3])

		block := make([]byte, length)
		if _, err := io.ReadFull(r, block); err != nil {
			return meta{}, err
		}
		switch kind {
		case flacVorbisComment:
			_, comments, err := parseComments(block)
			if err != nil {
				return meta{}, err
			}
			m.comments = comments
		case flacPicture:
			if p, err := parsePicture(block); err == nil {
				m.pictures = append(m.pictures, p)
			}
		}
		if last {
			return m, nil
		}
	}
}

// writeFLAC returns a copy of a FLAC file with its comments and pictures
// changed by update. If the file has no comment block, one is added; the
// pictures follow it.
func writeFLAC(data []byte, update func(*meta)) ([]byte, error) {
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

	// Pictures that can't be parsed are kept as they are.
	var m meta
	blocks = slices.DeleteFunc(blocks, func(b block) bool {
		if b.kind != flacPicture {
			return false
		}
		p, err := parsePicture(b.data)
		if err == nil {
			m.pictures = append(m.pictures, p)
		}
		return err == nil
	})

	i := slices.IndexFunc(blocks, func(b block) bool { return b.kind == flacVorbisComment })
	vendor := "CommonJukebox"
	if i >= 0 {
		var err error
		if vendor, m.comments, err = parseComments(blocks[i].data); err != nil {
			return nil, err
		}
	} else {
		// STREAMINFO must stay first, so the new block goes right after it.
		i = 1
		blocks = slices.Insert(blocks, i, block{kind: flacVorbisComment})
	}
	update(&m)
	blocks[i].data = encodeComments(vendor, m.comments)
	if len(blocks[i].data) >= 1<<24 {
		return nil, errors.New("tags too large")
	}
	for j, p := range m.pictures {
		b := block{flacPicture, encodePicture(p)}
		if len(b.data) >= 1<<24 {
			return nil, errors.New("picture too large")
		}
		blocks = slices.Insert(blocks, i+1+j, b)
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
