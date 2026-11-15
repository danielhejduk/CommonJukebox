package main

import (
	"testing"

	"github.com/danielhejduk/commonjukebox/internal/gonic"
)

func TestPlaylistFile(t *testing.T) {
	for in, want := range map[string]string{
		"Party": "Party.m3u8",
		" Road trip ":  "Road trip.m3u8",
		"AC/DC: Best?": "AC_DC_ Best_.m3u8",
		"..":           "___.m3u8",
		"":             "_.m3u8",
	} {
		if got := playlistFile(in); got != want {
			t.Errorf("playlistFile(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestM3U(t *testing.T) {
	p := gonic.Playlist{Name: "Mix", Songs: []gonic.Song{
		{ID: "1", Path: "Abba/Gold/01.flac"},
		{ID: "2", Path: "/Queen//Hits/02.opus"},
	}}
	want := "#EXTM#U\n#PLAYLIST: Mix\nAbba/Gold/01.flac\nQueen/Hits/02.opus\n"
	if got := string(m3u(p)); got != want {
		t.Errorf("m3u = %q, want %q", got, want)
	}
}

func TestShowNote(t *testing.T) {
	tests := []struct {
		listed, got int
		want        string
	}{
		{2, 2, ""},
		{0, 0, "playlist Mix is empty"},
		{8, 0, "Mix: the server can't find the files of 8 of its 8 tracks; they were probably moved or deleted. Add them again with jukebox playlist add"},
		{3, 2, "Mix: the server can't find the files of 1 of its 3 tracks; they were probably moved or deleted. Add them again with jukebox playlist add"},
		{1, 0, "Mix: the server can't find the files of 1 of its 1 track; they were probably moved or deleted. Add them again with jukebox playlist add"},
	}
	for _, tt := range tests {
		if got := showNote("Mix", tt.listed, tt.got); got != tt.want {
			t.Errorf("showNote(Mix, %d, %d) = %q, want %q", tt.listed, tt.got, got, tt.want)
		}
	}
}
