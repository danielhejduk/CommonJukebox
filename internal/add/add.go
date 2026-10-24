// Package add copies new tracks into the library, under
// <genre>/<album artist>/<album>/<NN>. <title>.<ext>.
package add

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/danielhejduk/commonjukebox/internal/check"
	"github.com/danielhejduk/commonjukebox/internal/config"
	"github.com/danielhejduk/commonjukebox/internal/library"
	"github.com/danielhejduk/commonjukebox/internal/tags"
	"github.com/danielhejduk/commonjukebox/internal/transcode"
)

type Item struct {
	Src       string
	Dest      string
	Track     tags.Track
	Changes   map[string]string
	Codec     string
	Transcode bool
}

type Prompter func(question, def string) (string, error)

var albumFields = []struct {
	key, name string
	value     func(*tags.Track) *string
}{
	{"GENRE", "Genre", func(t *tags.Track) *string { return &t.Genre }},
	{"ALBUMARTIST", "Album artist", func(t *tags.Track) *string { return &t.AlbumArtist }},
	{"ALBUM", "Album", func(t *tags.Track) *string { return &t.Album }},
}

var trackFields = []struct {
	key, name string
	value     func(*tags.Track) *string
}{
	{"ARTIST", "Artist", func(t *tags.Track) *string { return &t.Artist }},
	{"TITLE", "Title", func(t *tags.Track) *string { return &t.Title }},
}

func Plan(root string, s config.Settings, files []string, ask Prompter) ([]Item, []error) {
	var errs []error
	var dirs []string
	byDir := map[string][]tags.Track{}
	for _, f := range files {
		t, err := tags.Read(f)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		dir := filepath.Dir(f)
		if _, ok := byDir[dir]; !ok {
			dirs = append(dirs, dir)
		}
		byDir[dir] = append(byDir[dir], t)
	}

	var items []Item
	for _, dir := range dirs {
		planned, planErrs, err := planAlbum(root, s.Codec, dir, byDir[dir], ask)
		if err != nil {
			return nil, append(errs, err) // the prompt failed, so stop asking
		}
		items = append(items, planned...)
		errs = append(errs, planErrs...)
	}
	return items, errs
}

func planAlbum(root, codec, dir string, tracks []tags.Track, ask Prompter) (items []Item, errs []error, err error) {
	items = make([]Item, len(tracks))
	for i, t := range tracks {
		items[i] = Item{Src: t.Path, Track: t, Changes: map[string]string{}}
	}

	if ask != nil {
		for _, f := range albumFields {
			var missing []*Item
			for i := range items {
				if sanitize(*f.value(&items[i].Track)) == "" {
					missing = append(missing, &items[i])
				}
			}
			if len(missing) == 0 {
				continue
			}
			def := ""
			if f.key == "ALBUMARTIST" {
				def = missing[0].Track.Artist
			}
			answer, err := ask(f.name+" for "+dir, def)
			if err != nil {
				return nil, nil, err
			}
			for _, it := range missing {
				it.set(f.key, f.value(&it.Track), answer)
			}
		}

		for i := range items {
			it := &items[i]
			for _, f := range trackFields {
				if sanitize(*f.value(&it.Track)) != "" {
					continue
				}
				def := ""
				if f.key == "ARTIST" {
					def = it.Track.AlbumArtist
				}
				answer, err := ask(f.name+" for "+it.Src, def)
				if err != nil {
					return nil, nil, err
				}
				it.set(f.key, f.value(&it.Track), answer)
			}
			for it.Track.Track == 0 {
				answer, err := ask("Track number for "+it.Src, "")
				if err != nil {
					return nil, nil, err
				}
				if answer == "" {
					break
				}
				if n, err := strconv.Atoi(answer); err == nil && n > 0 {
					it.Track.Track = n
					it.Changes["TRACKNUMBER"] = answer
				}
			}
		}
	}

	multiDisc := slices.ContainsFunc(items, func(it Item) bool { return it.Track.Disc > 1 })
	var planned []Item
	for _, it := range items {
		if missing := missingTags(it.Track); len(missing) > 0 {
			errs = append(errs, fmt.Errorf("%s: missing-tag: %s not set", it.Src, strings.Join(missing, ", ")))
			continue
		}
		src := codecOf(it.Src)
		it.Codec = src
		// Only lossless sources are converted; re-encoding lossy audio loses quality.
		if src == "flac" && codec != "flac" {
			it.Codec = codec
			it.Transcode = true
		}
		it.Dest = destPath(root, it.Track, multiDisc, extFor(it.Codec))
		it.Track.Path = it.Dest
		planned = append(planned, it)
	}
	return planned, errs, nil
}

// set stores a non-empty answer in the track and in the changes to write.
func (it *Item) set(key string, field *string, answer string) {
	if answer = strings.TrimSpace(answer); answer != "" {
		*field = answer
		it.Changes[key] = answer
	}
}

// missingTags lists the tags the destination path can't be built without.
func missingTags(t tags.Track) []string {
	var missing []string
	if sanitize(t.Genre) == "" {
		missing = append(missing, "GENRE")
	}
	if sanitize(cmp.Or(t.AlbumArtist, t.Artist)) == "" {
		missing = append(missing, "ALBUMARTIST or ARTIST")
	}
	if sanitize(t.Album) == "" {
		missing = append(missing, "ALBUM")
	}
	if sanitize(t.Title) == "" {
		missing = append(missing, "TITLE")
	}
	if t.Track == 0 {
		missing = append(missing, "TRACKNUMBER")
	}
	return missing
}

func destPath(root string, t tags.Track, multiDisc bool, ext string) string {
	name := fmt.Sprintf("%02d. %s", t.Track, sanitize(t.Title))
	if multiDisc {
		name = fmt.Sprintf("%d-%s", max(t.Disc, 1), name)
	}
	artist := cmp.Or(sanitize(t.AlbumArtist), sanitize(t.Artist))
	return filepath.Join(root, sanitize(t.Genre), artist, sanitize(t.Album), name+ext)
}

// sanitize makes a tag value usable as one path element.
func sanitize(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '/' || r == 0 {
			return '_'
		}
		return r
	}, s)
	return strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(s), "."))
}

func codecOf(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".flac":
		return "flac"
	case ".opus":
		return "opus"
	}
	return "vorbis"
}

func extFor(codec string) string {
	switch codec {
	case "flac":
		return ".flac"
	case "opus":
		return ".opus"
	}
	return ".ogg"
}

func Validate(items []Item) []check.Problem {
	incoming := map[string]bool{}
	dirs := map[string]bool{}
	for _, it := range items {
		incoming[it.Dest] = true
		dirs[filepath.Dir(it.Dest)] = true
	}

	// Existing tracks go first: check reports a duplicate on the later
	// track, and that should be the one being added.
	var tracks []tags.Track

	for dir := range dirs {
		if _, err := os.Stat(dir); err != nil {
			continue // a new album
		}
		files, err := library.Scan(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			// A file about to be replaced is reported by Execute instead.
			if incoming[f] {
				continue
			}
			if t, err := tags.Read(f); err == nil {
				tracks = append(tracks, t)
			}
		}
	}

	for _, it := range items {
		tracks = append(tracks, it.Track)
	}

	var problems []check.Problem
	for _, p := range check.Check(tracks) {
		if incoming[p.Path] || dirs[p.Path] {
			problems = append(problems, p)
		}
	}
	return problems
}

// Execute copies or converts it.Src to it.Dest and writes the planned tags.
// An existing destination is only replaced when force is set.
func Execute(it Item, force bool) error {
	if !force {
		if err := notExist(it.Dest); err != nil {
			return err
		}
	}
	info, err := os.Stat(it.Src)
	if err != nil {
		return err
	}
	dir := filepath.Dir(it.Dest)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	// ffmpeg picks the output format from the extension, so keep it.
	tmp, err := os.CreateTemp(dir, ".jukebox-add-*"+filepath.Ext(it.Dest))
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename

	if it.Transcode {
		err = tmp.Close()
		if err == nil {
			err = transcode.Transcode(it.Src, tmp.Name(), it.Codec)
		}
	} else {
		err = copyFile(tmp, it.Src)
	}
	if err != nil {
		return err
	}
	if len(it.Changes) > 0 {
		if err := tags.Write(tmp.Name(), it.Changes); err != nil {
			return err
		}
	}
	if err := os.Chmod(tmp.Name(), info.Mode().Perm()); err != nil {
		return err
	}
	if !force {
		if err := notExist(it.Dest); err != nil {
			return err
		}
	}
	return os.Rename(tmp.Name(), it.Dest)
}

func notExist(path string) error {
	_, err := os.Lstat(path)
	switch {
	case err == nil:
		return fmt.Errorf("%s: already exists (pass --force to replace it)", path)
	case errors.Is(err, fs.ErrNotExist):
		return nil
	}
	return err
}

// copyFile copies src into dst and closes dst.
func copyFile(dst *os.File, src string) error {
	in, err := os.Open(src)
	if err != nil {
		_ = dst.Close()
		return err
	}
	defer in.Close()
	if _, err := io.Copy(dst, in); err != nil {
		_ = dst.Close()
		return err
	}
	return dst.Close()
}
