package main

import "testing"

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
