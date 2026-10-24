package tags

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	_ "image/jpeg" // registers JPEG for image.DecodeConfig
	_ "image/png"  // registers PNG for image.DecodeConfig
	"slices"
	"strings"
)

// FrontCover is the picture type of a front cover.
const FrontCover = 3

// otherPicture is the picture type of an image with no stated purpose.
const otherPicture = 0

// Picture is an embedded image. FLAC stores it in a PICTURE block, Ogg in a
// base64 METADATA_BLOCK_PICTURE comment; the layout is the same.
type Picture struct {
	Type        uint32 // FrontCover, or one of the other types FLAC defines
	MIME        string
	Description string
	Width       uint32
	Height      uint32
	Depth       uint32 // bits per pixel
	Colors      uint32 // palette size, 0 if the image has no palette
	Data        []byte
}

// pictureKey is the Ogg comment that holds a picture.
const pictureKey = "METADATA_BLOCK_PICTURE"

// NewCover returns a front cover holding the JPEG or PNG image in data.
func NewCover(data []byte) (Picture, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Picture{}, errors.New("not a JPEG or PNG image")
	}
	p := Picture{
		Type:   FrontCover,
		MIME:   "image/" + format,
		Width:  uint32(cfg.Width),
		Height: uint32(cfg.Height),
		Depth:  24,
		Data:   data,
	}
	if palette, ok := cfg.ColorModel.(color.Palette); ok {
		p.Depth, p.Colors = 8, uint32(len(palette))
	} else {
		switch cfg.ColorModel {
		case color.GrayModel:
			p.Depth = 8
		case color.Gray16Model:
			p.Depth = 16
		case color.RGBAModel, color.NRGBAModel, color.CMYKModel:
			p.Depth = 32
		case color.RGBA64Model, color.NRGBA64Model:
			p.Depth = 64
		}
	}
	return p, nil
}

// Ext returns the file extension for the picture's image type.
func (p Picture) Ext() string {
	if p.MIME == "image/png" {
		return ".png"
	}
	return ".jpg"
}

// Sum returns the value Track.Art has for a front cover holding data.
func Sum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Cover returns the front cover of a .flac, .ogg or .opus file, or nil if it
// has none.
func Cover(path string) (*Picture, error) {
	m, err := readMeta(path)
	if err != nil {
		return nil, err
	}
	return frontCover(m.pictures), nil
}

func frontCover(pictures []Picture) *Picture {
	i := frontCoverIndex(pictures)
	if i < 0 {
		return nil
	}
	return &pictures[i]
}

// frontCoverIndex returns the index of the front cover, or -1. Without one,
// a picture of type "other" stands in for it: ffmpeg embeds covers that way.
func frontCoverIndex(pictures []Picture) int {
	if i := slices.IndexFunc(pictures, func(p Picture) bool { return p.Type == FrontCover }); i >= 0 {
		return i
	}
	return slices.IndexFunc(pictures, func(p Picture) bool { return p.Type == otherPicture })
}

// SetCover replaces the front cover of a file with p; nil removes it. Other
// pictures are kept.
func SetCover(path string, p *Picture) error {
	return modify(path, func(m *meta) {
		for i := frontCoverIndex(m.pictures); i >= 0; i = frontCoverIndex(m.pictures) {
			m.pictures = slices.Delete(m.pictures, i, i+1)
		}
		if p != nil {
			m.pictures = append(m.pictures, *p)
		}
	})
}

// CopyCover gives dst the front cover of src, if src has one.
func CopyCover(src, dst string) error {
	p, err := Cover(src)
	if err != nil || p == nil {
		return err
	}
	return SetCover(dst, p)
}

// parsePicture parses a FLAC picture: big-endian uint32 fields, with the
// MIME type, description and data each prefixed with their length.
func parsePicture(b []byte) (Picture, error) {
	r := pictureReader{b: b}
	p := Picture{Type: r.u32()}
	p.MIME = string(r.bytes())
	p.Description = string(r.bytes())
	p.Width, p.Height, p.Depth, p.Colors = r.u32(), r.u32(), r.u32(), r.u32()
	p.Data = r.bytes()
	if r.short {
		return Picture{}, errors.New("truncated picture")
	}
	return p, nil
}

// pictureReader reads the fields of a picture; once one is cut short, the
// rest read as zero.
type pictureReader struct {
	b     []byte
	short bool
}

func (r *pictureReader) u32() uint32 {
	if len(r.b) < 4 {
		r.short, r.b = true, nil
		return 0
	}
	n := binary.BigEndian.Uint32(r.b)
	r.b = r.b[4:]
	return n
}

func (r *pictureReader) bytes() []byte {
	n := r.u32()
	if uint64(n) > uint64(len(r.b)) {
		r.short, r.b = true, nil
		return nil
	}
	s := r.b[:n:n]
	r.b = r.b[n:]
	return s
}

// encodePicture is the reverse of parsePicture.
func encodePicture(p Picture) []byte {
	var b bytes.Buffer
	u32 := func(n uint32) { _ = binary.Write(&b, binary.BigEndian, n) }
	put := func(s []byte) {
		u32(uint32(len(s)))
		b.Write(s)
	}
	u32(p.Type)
	put([]byte(p.MIME))
	put([]byte(p.Description))
	u32(p.Width)
	u32(p.Height)
	u32(p.Depth)
	u32(p.Colors)
	put(p.Data)
	return b.Bytes()
}

// splitPictures moves the pictures out of Ogg comments. Comments that can't
// be decoded are left among the others.
func splitPictures(comments []string) (rest []string, pictures []Picture) {
	for _, c := range comments {
		key, value, _ := strings.Cut(c, "=")
		if strings.EqualFold(key, pictureKey) {
			if data, err := base64.StdEncoding.DecodeString(value); err == nil {
				if p, err := parsePicture(data); err == nil {
					pictures = append(pictures, p)
					continue
				}
			}
		}
		rest = append(rest, c)
	}
	return rest, pictures
}

// joinPictures is the reverse of splitPictures.
func joinPictures(comments []string, pictures []Picture) []string {
	for _, p := range pictures {
		comments = append(comments, pictureKey+"="+base64.StdEncoding.EncodeToString(encodePicture(p)))
	}
	return comments
}
