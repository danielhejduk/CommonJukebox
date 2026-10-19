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
	Path   string
	Genre  string
	Artist string
	Album  string
	Title  string
	Track  int    // 0 if missing or not a number
	Year   string // first 4 characters of DATE
}

var errUnsupported = errors.New("unsupported file type")

// Comments returns the raw "KEY=value" tags of a .flac, .ogg or .opus file.
func Comments(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var comments []string
	switch strings.ToLower(filepath.Ext(path)) {
	case ".flac":
		comments, err = readFLAC(f)
	case ".ogg", ".opus":
		comments, err = readOgg(f)
	default:
		err = errUnsupported
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return comments, nil
}

// Read reads the tags of a .flac, .ogg or .opus file.
func Read(path string) (Track, error) {
	comments, err := Comments(path)
	if err != nil {
		return Track{}, err
	}

	// Keys are case-insensitive; the first value of a key wins.
	tags := map[string]string{}
	for _, c := range comments {
		key, value, ok := strings.Cut(c, "=")
		key = strings.ToUpper(key)
		if _, seen := tags[key]; ok && !seen {
			tags[key] = value
		}
	}

	// TRACKNUMBER is "6" or "6/12".
	num, _, _ := strings.Cut(tags["TRACKNUMBER"], "/")
	track, _ := strconv.Atoi(num)

	year := tags["DATE"]
	if len(year) > 4 {
		year = year[:4]
	}

	return Track{
		Path:   path,
		Genre:  tags["GENRE"],
		Artist: tags["ARTIST"],
		Album:  tags["ALBUM"],
		Title:  tags["TITLE"],
		Track:  track,
		Year:   year,
	}, nil
}

// Write sets tags in a file. changes maps an uppercase key (e.g. "ARTIST") to
// its new value; an empty value removes the tag. Other tags are kept.
//
// The new file is written next to the original and renamed over it, so a
// crash never leaves a half-written track.
func Write(path string, changes map[string]string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	update := func(comments []string) []string { return applyChanges(comments, changes) }
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
		tmp.Close()
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
		binary.Write(&b, binary.LittleEndian, uint32(len(s)))
		b.WriteString(s)
	}
	put(vendor)
	binary.Write(&b, binary.LittleEndian, uint32(len(comments)))
	for _, c := range comments {
		put(c)
	}
	return b.Bytes()
}
