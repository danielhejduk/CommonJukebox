package tags

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

type Track struct {
	Path        string
	Genre       string
	Artist      string
	AlbumArtist string // empty if missing
	Album       string
	Title       string
	Disc        int    // 0 if missing or not a number
	Track       int    // 0 if missing or not a number
	Year        string // first 4 characters of DATE
	Lyrics      string
	Art         string // Sum of the front cover, empty if there is none
}

// meta is what a file holds besides its audio: Vorbis comments, without any
// pictures, and the embedded pictures.
type meta struct {
	comments []string
	pictures []Picture
}

var errUnsupported = errors.New("unsupported file type")

// Comments returns the raw "KEY=value" tags of a .flac, .ogg or .opus file.
// Embedded pictures are left out.
func Comments(path string) ([]string, error) {
	m, err := readMeta(path)
	if err != nil {
		return nil, err
	}
	return m.comments, nil
}

func readMeta(path string) (meta, error) {
	f, err := os.Open(path)
	if err != nil {
		return meta{}, err
	}
	defer f.Close()

	var m meta
	switch strings.ToLower(filepath.Ext(path)) {
	case ".flac":
		m, err = readFLAC(f)
	case ".ogg", ".opus":
		m, err = readOgg(f)
	default:
		err = errUnsupported
	}
	if err != nil {
		return meta{}, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}

// Read reads the tags of a .flac, .ogg or .opus file.
func Read(path string) (Track, error) {
	m, err := readMeta(path)
	if err != nil {
		return Track{}, err
	}
	comments := m.comments

	// Keys are case-insensitive; the first value of a key wins.
	tags := map[string]string{}
	for _, c := range comments {
		key, value, ok := strings.Cut(c, "=")
		key = strings.ToUpper(key)
		if _, seen := tags[key]; ok && !seen {
			tags[key] = value
		}
	}

	// TRACKNUMBER and DISCNUMBER are "6" or "6/12".
	number := func(key string) int {
		num, _, _ := strings.Cut(tags[key], "/")
		n, _ := strconv.Atoi(num)
		return n
	}

	art := ""
	if p := frontCover(m.pictures); p != nil {
		art = Sum(p.Data)
	}

	year := tags["DATE"]
	if len(year) > 4 {
		year = year[:4]
	}

	return Track{
		Path:        path,
		Genre:       tags["GENRE"],
		Artist:      tags["ARTIST"],
		AlbumArtist: tags["ALBUMARTIST"],
		Album:       tags["ALBUM"],
		Title:       tags["TITLE"],
		Disc:        number("DISCNUMBER"),
		Track:       number("TRACKNUMBER"),
		Year:        year,
		Lyrics:      tags["LYRICS"],
		Art:         art,
	}, nil
}

// Write sets tags in a file. changes maps an uppercase key (e.g. "ARTIST") to
// its new value; an empty value removes the tag. Other tags are kept.
//
// The new file is written next to the original and renamed over it, so a
// crash never leaves a half-written track.
func Write(path string, changes map[string]string) error {
	return modify(path, func(m *meta) { m.comments = applyChanges(m.comments, changes) })
}

// modify rewrites a file with its meta changed by update, atomically like
// Write.
func modify(path string, update func(*meta)) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var out []byte
	switch strings.ToLower(filepath.Ext(path)) {
	case ".flac":
		out, err = writeFLAC(data, update)
	case ".ogg", ".opus":
		out, err = writeOgg(data, update)
	default:
		err = errUnsupported
	}
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".jukebox-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename

	if _, err := tmp.Write(out); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), info.Mode()); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// applyChanges drops every comment whose key is in changes, then appends the
// non-empty new values in key order.
func applyChanges(comments []string, changes map[string]string) []string {
	var out []string
	for _, c := range comments {
		key, _, _ := strings.Cut(c, "=")
		if _, changed := changes[strings.ToUpper(key)]; !changed {
			out = append(out, c)
		}
	}
	for _, key := range slices.Sorted(maps.Keys(changes)) {
		if value := changes[key]; value != "" {
			out = append(out, key+"="+value)
		}
	}
	return out
}

// parseComments parses a Vorbis comment block, the tag format used by both
// FLAC and Ogg: vendor string, comment count, then "KEY=value" strings, each
// prefixed with a little-endian uint32 length. Trailing bytes are ignored.
func parseComments(b []byte) (vendor string, comments []string, err error) {
	errShort := errors.New("truncated comment block")

	next := func() (string, error) {
		if len(b) < 4 {
			return "", errShort
		}
		n := binary.LittleEndian.Uint32(b)
		b = b[4:]
		if uint64(n) > uint64(len(b)) {
			return "", errShort
		}
		s := string(b[:n])
		b = b[n:]
		return s, nil
	}

	if vendor, err = next(); err != nil {
		return "", nil, err
	}
	if len(b) < 4 {
		return "", nil, errShort
	}
	count := binary.LittleEndian.Uint32(b)
	b = b[4:]

	for range count {
		c, err := next()
		if err != nil {
			return "", nil, err
		}
		comments = append(comments, c)
	}
	return vendor, comments, nil
}

// encodeComments is the reverse of parseComments.
func encodeComments(vendor string, comments []string) []byte {
	var b bytes.Buffer
	put := func(s string) {
		_ = binary.Write(&b, binary.LittleEndian, uint32(len(s)))
		b.WriteString(s)
	}
	put(vendor)
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(comments)))
	for _, c := range comments {
		put(c)
	}
	return b.Bytes()
}
