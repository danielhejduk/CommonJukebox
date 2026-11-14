package gonic

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/supersonic-app/go-subsonic/subsonic"
)

// Server is a logged-in connection to a Subsonic server.
type Server struct {
	c    *subsonic.Client
	poll time.Duration // how often Scan(true) asks whether the scan is done
}

// Playlist is a playlist on the server. Songs is only filled in by Get.
type Playlist struct {
	ID        string
	Name      string
	Comment   string
	Public    bool
	SongCount int
	Songs     []Song
}

// Song is one playlist entry. Path is relative to the server's music folder.
type Song struct {
	ID   string
	Path string
}

// pageSize is how many songs Songs asks for at a time.
const pageSize = 500

// Open logs in to the server at rawURL, http(s)://user@host[:port][/path].
func Open(rawURL, password string) (*Server, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	who := u.User.Username() + "@" + u.Host
	c := &subsonic.Client{
		// The library panics without an http.Client.
		Client:     &http.Client{Timeout: 30 * time.Second},
		BaseUrl:    (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path}).String(),
		User:       u.User.Username(),
		ClientName: "commonjukebox",
		UseJSON:    true,
	}
	err = c.Authenticate(password)
	switch {
	case errors.Is(err, subsonic.ErrAuthenticationFailure):
		return nil, fmt.Errorf("wrong password for %s", who)
	case err != nil:
		return nil, fmt.Errorf("subsonic %s: %w", who, err)
	}
	return &Server{c: c, poll: time.Second}, nil
}

// Songs returns the ID of every song on the server, by path.
func (s *Server) Songs() (map[string]string, error) {
	songs := map[string]string{}
	for offset := 0; ; offset += pageSize {
		// gonic lists every song for the query "", the two quote
		// characters. An empty query is a different request.
		res, err := s.c.Search3(`""`, map[string]string{
			"songCount":   strconv.Itoa(pageSize),
			"songOffset":  strconv.Itoa(offset),
			"artistCount": "0",
			"albumCount":  "0",
		})
		if err != nil {
			return nil, err
		}
		var page []*subsonic.Child
		if res != nil {
			page = res.Song
		}
		for _, c := range page {
			songs[c.Path] = c.ID
		}
		if len(page) < pageSize {
			return songs, nil
		}
	}
}

// Playlists lists the playlists the user can see, without their songs.
func (s *Server) Playlists() ([]Playlist, error) {
	lists, err := s.c.GetPlaylists(nil)
	if err != nil {
		return nil, err
	}
	out := make([]Playlist, len(lists))
	for i, p := range lists {
		out[i] = playlist(p)
	}
	return out, nil
}

// Get returns the playlist with its songs.
func (s *Server) Get(id string) (*Playlist, error) {
	p, err := s.c.GetPlaylist(id)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, fmt.Errorf("the server returned no playlist %s", id)
	}
	out := playlist(p)
	return &out, nil
}

func playlist(p *subsonic.Playlist) Playlist {
	out := Playlist{ID: p.ID, Name: p.Name, Comment: p.Comment, Public: p.Public, SongCount: p.SongCount}
	for _, e := range p.Entry {
		out.Songs = append(out.Songs, Song{ID: e.ID, Path: e.Path})
	}
	return out
}

// Delete deletes the playlist.
func (s *Server) Delete(id string) error {
	return s.c.DeletePlaylist(id)
}

// Find returns the playlist with the given name, ignoring case, or nil
// if there is none. More than one is an error, as Find can't pick.
func (s *Server) Find(name string) (*Playlist, error) {
	lists, err := s.Playlists()
	if err != nil {
		return nil, err
	}
	var found []Playlist
	for _, p := range lists {
		if strings.EqualFold(p.Name, name) {
			found = append(found, p)
		}
	}
	switch len(found) {
	case 0:
		return nil, nil
	case 1:
		return &found[0], nil
	}
	names := make([]string, len(found))
	for i, p := range found {
		names[i] = fmt.Sprintf("%q (id %s)", p.Name, p.ID)
	}
	return nil, fmt.Errorf("%d playlists are named %q: %s; rename or delete all but one",
		len(found), name, strings.Join(names, ", "))
}

// Create makes a playlist holding the songs with the given IDs.
func (s *Server) Create(name string, ids []string) error {
	return s.c.CreatePlaylistWithTracks(ids, map[string]string{"name": name})
}

// Append adds the songs with the given IDs to the end of the playlist.
func (s *Server) Append(id string, ids []string) error {
	return s.c.UpdatePlaylistTracks(id, ids, nil)
}

// Replace sets the songs of p to the given IDs, keeping its ID, name,
// comment and public flag.
func (s *Server) Replace(p Playlist, ids []string) error {
	// Given a playlistId, gonic's createPlaylist replaces the songs in
	// place and the ID stays the same. Don't remove songs with
	// updatePlaylist's songIndexToRemove instead: its indexes count
	// broken entries the apps don't show, and gonic doesn't check them.
	err := s.c.CreatePlaylistWithTracks(ids, map[string]string{"playlistId": p.ID, "name": p.Name})
	if err != nil {
		return err
	}
	// createPlaylist ignores the comment and public flag, so set them again.
	return s.c.UpdatePlaylist(p.ID, map[string]string{
		"comment": p.Comment,
		"public":  strconv.FormatBool(p.Public),
	})
}

// Scan starts a library scan and, with wait, returns once it's done.
func (s *Server) Scan(wait bool) error {
	if _, err := s.c.StartScan(); err != nil || !wait {
		return err
	}
	for {
		// Wait before asking: gonic scans in the background, so straight
		// after startScan the scan may not have begun yet.
		time.Sleep(s.poll)
		st, err := s.c.GetScanStatus()
		if err != nil {
			return err
		}
		if st == nil || !st.Scanning {
			return nil
		}
	}
}
