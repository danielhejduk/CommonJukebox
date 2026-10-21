package check

import (
	"strings"

	"github.com/danielhejduk/commonjukebox/internal/tags"
)

type Problem struct {
	Path    string
	Rule    string
	Message string
}

func (p Problem) String() string { return p.Path + ": " + p.Rule + ": " + p.Message }
func Check(tracks []tags.Track) []Problem {
	var problems []Problem
	for _, t := range tracks {
		problems = append(problems, checkTrack(t)...)
	}
	return problems
}

func checkTrack(t tags.Track) []Problem {
	var problems []Problem
	require := func(key, value string) {
		if strings.TrimSpace(value) == "" {
			problems = append(problems, Problem{t.Path, "missing-tag", key + " is not set"})
		}
	}
	require("ARTIST", t.Artist)
	require("ALBUM", t.Album)
	require("TITLE", t.Title)
	if t.Track == 0 {
		problems = append(problems, Problem{t.Path, "missing-tag", "TRACKNUMBER is not set or not a number"})
	}
	return problems
}
