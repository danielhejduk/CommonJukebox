package tags

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
)

// An Ogg file is a sequence of pages. Each page has a 27-byte header, a
// segment table and a body. Packets are split across segments: a segment
// shorter than 255 bytes ends the current packet.
//
// The first packets are headers: Opus has two (OpusHead, OpusTags), Vorbis
// three (identification, comments, setup). The second packet holds the tags.
// The first page holds only the first header, and audio starts on a new page.

type oggPage []byte // the whole page, header included

func (p oggPage) segments() []byte { return p[27 : 27+int(p[26])] }
func (p oggPage) body() []byte     { return p[27+int(p[26]):] }

func readPage(r io.Reader) (oggPage, error) {
	header := make([]byte, 27)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}
	if string(header[:4]) != "OggS" {
		return nil, errors.New("not an Ogg file")
	}
	segments := make([]byte, header[26])
	if _, err := io.ReadFull(r, segments); err != nil {
		return nil, err
	}
	size := 0
	for _, s := range segments {
		size += int(s)
	}
	body := make([]byte, size)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return oggPage(append(append(header, segments...), body...)), nil
}

// readHeaders reads the pages holding the header packets.
func readHeaders(r io.Reader) (pages []oggPage, packets [][]byte, err error) {
	var packet []byte
	want := 0 // number of header packets, known after the first one
	for want == 0 || len(packets) < want {
		page, err := readPage(r)
		if err != nil {
			return nil, nil, err
		}
		pages = append(pages, page)

		body := page.body()
		for _, size := range page.segments() {
			packet = append(packet, body[:size]...)
			body = body[size:]
			if size < 255 {
				packets = append(packets, packet)
				packet = nil
			}
		}

		if want == 0 && len(packets) > 0 {
			switch {
			case bytes.HasPrefix(packets[0], []byte("OpusHead")):
				want = 2
			case bytes.HasPrefix(packets[0], []byte("\x01vorbis")):
				want = 3
			default:
				return nil, nil, errors.New("unsupported Ogg codec")
			}
		}
	}
	if len(packets) != want || packet != nil {
		return nil, nil, errors.New("unsupported Ogg layout")
	}
	return pages, packets, nil
}

// commentPrefix returns the codec's prefix of the comment packet.
func commentPrefix(packet []byte) []byte {
	if bytes.HasPrefix(packet, []byte("OpusTags")) {
		return []byte("OpusTags")
	}
	return []byte("\x03vorbis")
}

// readOgg returns the comments and pictures from an Ogg Opus or Ogg Vorbis
// file.
func readOgg(r io.Reader) (meta, error) {
	_, packets, err := readHeaders(r)
	if err != nil {
		return meta{}, err
	}
	prefix := commentPrefix(packets[1])
	if !bytes.HasPrefix(packets[1], prefix) {
		return meta{}, errors.New("missing comment header")
	}
	_, comments, err := parseComments(packets[1][len(prefix):])
	if err != nil {
		return meta{}, err
	}
	var m meta
	m.comments, m.pictures = splitPictures(comments)
	return m, nil
}

// writeOgg returns a copy of an Ogg file with its comments and pictures
// changed by update. The header pages after the first are rebuilt; if their
// count changes, the following pages are renumbered.
func writeOgg(data []byte, update func(*meta)) ([]byte, error) {
	r := bytes.NewReader(data)
	pages, packets, err := readHeaders(r)
	if err != nil {
		return nil, err
	}
	if bytes.Count(pages[0].segments(), []byte{255}) != len(pages[0].segments())-1 {
		return nil, errors.New("unsupported Ogg layout")
	}

	prefix := commentPrefix(packets[1])
	if !bytes.HasPrefix(packets[1], prefix) {
		return nil, errors.New("missing comment header")
	}
	vendor, comments, err := parseComments(packets[1][len(prefix):])
	if err != nil {
		return nil, err
	}
	var m meta
	m.comments, m.pictures = splitPictures(comments)
	update(&m)
	comment := append(prefix, encodeComments(vendor, joinPictures(m.comments, m.pictures))...)
	if string(prefix) == "\x03vorbis" {
		comment = append(comment, 1) // Vorbis framing bit
	}

	serial := binary.LittleEndian.Uint32(pages[0][14:])
	var out bytes.Buffer
	out.Write(pages[0])
	seq := uint32(1)
	for _, p := range append([][]byte{comment}, packets[2:]...) {
		for _, page := range paginate(p, serial, seq) {
			out.Write(page)
			seq++
		}
	}

	// Copy the audio pages, renumbering them if the header page count changed.
	delta := seq - uint32(len(pages))
	if delta == 0 {
		out.Write(data[len(data)-r.Len():])
		return out.Bytes(), nil
	}
	for {
		page, err := readPage(r)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		binary.LittleEndian.PutUint32(page[18:], binary.LittleEndian.Uint32(page[18:])+delta)
		setCRC(page)
		out.Write(page)
	}
	return out.Bytes(), nil
}

// paginate splits one header packet into pages, starting at sequence number seq.
func paginate(packet []byte, serial, seq uint32) []oggPage {
	var lacing []byte
	n := len(packet)
	for ; n >= 255; n -= 255 {
		lacing = append(lacing, 255)
	}
	lacing = append(lacing, byte(n))

	var pages []oggPage
	for start := 0; start < len(lacing); start += 255 {
		segments := lacing[start:min(start+255, len(lacing))]
		size := 0
		for _, s := range segments {
			size += int(s)
		}

		var flags byte
		if start > 0 {
			flags = 1 // continues the packet from the previous page
		}
		granule := uint64(0)
		if segments[len(segments)-1] == 255 {
			granule = ^uint64(0) // no packet ends on this page
		}

		page := make(oggPage, 27, 27+len(segments)+size)
		copy(page, "OggS")
		page[5] = flags
		binary.LittleEndian.PutUint64(page[6:], granule)
		binary.LittleEndian.PutUint32(page[14:], serial)
		binary.LittleEndian.PutUint32(page[18:], seq)
		page[26] = byte(len(segments))
		page = append(page, segments...)
		page = append(page, packet[:size]...)
		packet = packet[size:]
		setCRC(page)

		pages = append(pages, page)
		seq++
	}
	return pages
}

// Ogg uses CRC-32 with polynomial 0x04c11db7, no reflection, zero init.
var crcTable = func() (t [256]uint32) {
	for i := range t {
		r := uint32(i) << 24
		for range 8 {
			if r&0x80000000 != 0 {
				r = r<<1 ^ 0x04c11db7
			} else {
				r <<= 1
			}
		}
		t[i] = r
	}
	return t
}()

func setCRC(page oggPage) {
	binary.LittleEndian.PutUint32(page[22:], 0)
	var crc uint32
	for _, b := range page {
		crc = crc<<8 ^ crcTable[byte(crc>>24)^b]
	}
	binary.LittleEndian.PutUint32(page[22:], crc)
}
