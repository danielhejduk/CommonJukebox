package check

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/danielhejduk/commonjukebox/internal/tags"
)

type Problem struct {
	Path    string
	Rule    string
	Message string
}

type Album struct {
	Dir    string
	Tracks []tags.Track
}

func (p Problem) String() string { return p.Path + ": " + p.Rule + ": " + p.Message }
func Check(tracks []tags.Track) []Problem {
	var problems []Problem
	for _, t := range tracks {
		problems = append(problems, checkTrack(t)...)
	}
	for _, a := range groupAlbums(tracks) {
		problems = append(problems, checkAlbum(a)...)
	}
	return problems
}

func groupAlbums(tracks []tags.Track) []Album {
	byDir := map[string][]tags.Track{}
	for _, t := range tracks {
		if strings.TrimSpace(t.Album) == "" {
			continue
		}
		dir := filepath.Dir(t.Path)
		byDir[dir] = append(byDir[dir], t)
	}

	var albums []Album
	for _, dir := range slices.Sorted(maps.Keys(byDir)) {
		albums = append(albums, Album{Dir: dir, Tracks: byDir[dir]})
	}
	return albums
}

// require appends a missing-tag problem with message to problems if value is blank.
func require(problems []Problem, path, value, message string) []Problem {
	if strings.TrimSpace(value) == "" {
		problems = append(problems, Problem{path, "missing-tag", message})
	}
	return problems
}

func checkTrack(t tags.Track) []Problem {
	var problems []Problem
	problems = require(problems, t.Path, t.Artist, "ARTIST is not set")
	problems = require(problems, t.Path, t.Album, "ALBUM is not set")
	problems = require(problems, t.Path, t.Title, "TITLE is not set")
	// t.Track is 0 when TRACKNUMBER is missing or not a number.
	trackNumber := ""
	if t.Track != 0 {
		trackNumber = strconv.Itoa(t.Track)
	}
	problems = require(problems, t.Path, trackNumber, "TRACKNUMBER is not set or not a number")
	return problems
}

func checkAlbum(a Album) []Problem {
	// TODO: inconsistent-tag, missing-albumartist
	var problems []Problem
	seen := map[[2]int]string{} // {disc, track} -> path of the first track with it
	for _, t := range a.Tracks {
		problems = require(problems, t.Path, t.AlbumArtist, "ALBUMARTIST is not set")

		if t.Track == 0 {
			continue // checkTrack reports it
		}
		key := [2]int{t.Disc, t.Track}
		if first, ok := seen[key]; ok {
			msg := fmt.Sprintf("track %d is also used by %s", t.Track, first)
			if t.Disc != 0 {
				msg = fmt.Sprintf("disc %d %s", t.Disc, msg)
			}
			problems = append(problems, Problem{t.Path, "duplicate-track", msg})
			continue
		}
		seen[key] = t.Path
	}

	// Every disc should have tracks 1 to its highest track number.
	last := map[int]int{} // disc -> highest track number
	for key := range seen {
		last[key[0]] = max(last[key[0]], key[1])
	}
	for _, disc := range slices.Sorted(maps.Keys(last)) {
		var missing []string
		for n := 1; n <= last[disc]; n++ {
			if _, ok := seen[[2]int{disc, n}]; !ok {
				missing = append(missing, strconv.Itoa(n))
			}
		}
		if len(missing) == 0 {
			continue
		}
		msg := "track " + missing[0] + " is missing"
		if len(missing) > 1 {
			msg = "tracks " + strings.Join(missing, ", ") + " are missing"
		}
		if disc != 0 {
			msg = fmt.Sprintf("disc %d %s", disc, msg)
		}
		problems = append(problems, Problem{a.Dir, "missing-track", msg})
	}
	return problems
}
