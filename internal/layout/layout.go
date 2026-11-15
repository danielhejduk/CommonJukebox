// Package layout decides where a track belongs in the library
package layout

import (
	"cmp"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/danielhejduk/commonjukebox/internal/tags"
)

type Track struct {
	tags.Track
	Ext string
}

type Warning struct {
	Path    string
	Message string
}

func (w Warning) String() string { return w.Path + ": " + w.Message }

func Place(root string, tracks []Track) (dests []string, warnings []Warning) {
	type key struct{ dir, album string }
	var order []key
	albums := map[key][]int{}
	for i, t := range tracks {
		k := key{filepath.Dir(t.Path), Sanitize(t.Album)}
		if _, ok := albums[k]; !ok {
			order = append(order, k)
		}
		albums[k] = append(albums[k], i)
	}
	folders := make([]string, len(tracks))
	for _, k := range order {
		idx := albums[k]
		genre := majorityOf(tracks, idx, &warnings, func(t Track) (string, string) {
			return "GENRE", Sanitize(t.Genre)
		})
		artist := majorityOf(tracks, idx, &warnings, func(t Track) (string, string) {
			if a := Sanitize(t.AlbumArtist); a != "" {
				return "ALBUMARTIST", a
			}
			return "ARTIST", Sanitize(t.Artist)
		})
		folder := filepath.Join(root, genre, artist, k.album)
		for _, i := range idx {
			folders[i] = folder
		}
	}

	multiDisc := map[string]bool{}
	for i, t := range tracks {
		if t.Disc > 1 {
			multiDisc[folders[i]] = true
		}
	}
	dests = make([]string, len(tracks))
	for i, t := range tracks {
		dests[i] = filepath.Join(folders[i], fileName(t.Track, multiDisc[folders[i]])+t.Ext)
	}
	return dests, warnings
}

func majorityOf(tracks []Track, idx []int, warnings *[]Warning, value func(Track) (tag, v string)) string {
	values := make([]string, len(idx))
	for j, i := range idx {
		_, values[j] = value(tracks[i])
	}
	common := Majority(values)
	for j, i := range idx {
		if v := values[j]; v != common {
			tag, _ := value(tracks[i])
			msg := fmt.Sprintf("%s is %q, but the album goes under %q", tag, v, common)
			*warnings = append(*warnings, Warning{tracks[i].Path, msg})
		}
	}
	return common
}

func Majority(values []string) string {
	count := map[string]int{}
	common := ""
	for _, v := range values {
		if strings.TrimSpace(v) == "" {
			continue
		}
		count[v]++
		if count[v] > count[common] {
			common = v
		}
	}
	return common
}

func fileName(t tags.Track, multiDisc bool) string {
	name := fmt.Sprintf("%02d. %s", t.Track, Sanitize(t.Title))
	if multiDisc {
		name = fmt.Sprintf("%d-%s", max(t.Disc, 1), name)
	}
	return name
}

func Missing(t tags.Track) []string {
	var missing []string
	if Sanitize(t.Genre) == "" {
		missing = append(missing, "GENRE")
	}
	if cmp.Or(Sanitize(t.AlbumArtist), Sanitize(t.Artist)) == "" {
		missing = append(missing, "ALBUMARTIST or ARTIST")
	}
	if Sanitize(t.Album) == "" {
		missing = append(missing, "ALBUM")
	}
	if Sanitize(t.Title) == "" {
		missing = append(missing, "TITLE")
	}
	if t.Track == 0 {
		missing = append(missing, "TRACKNUMBER")
	}
	return missing
}

func Sanitize(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '/' || r == 0 {
			return '_'
		}
		return r
	}, s)
	return strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(s), "."))
}

func Ext(codec string) string {
	switch codec {
	case "flac":
		return ".flac"
	case "opus":
		return ".opus"
	}
	return ".ogg"
}
