package gonic

import (
	"crypto/md5"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fake is a Subsonic server that answers with canned JSON. Each handler
// gets the request's parameters and returns the subsonic-response object.
type fake struct {
	t   *testing.T
	url string // http://alice@127.0.0.1:port

	mu       sync.Mutex
	handlers map[string]func(q url.Values) string // by request path
	calls    []string                             // endpoint names, in order
}

func newFake(t *testing.T) *fake {
	f := &fake{t: t, handlers: map[string]func(url.Values) string{}}
	f.handle("/rest/ping", func(url.Values) string { return ok("") })
	// The library asks for the extensions before it changes a playlist.
	// No formPost here, so it sends everything in the query string.
	f.handle("/rest/getOpenSubsonicExtensions", func(url.Values) string {
		return ok(`"openSubsonicExtensions":[]`)
	})
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.User("alice")
	f.url = u.String()
	return f
}

func (f *fake) handle(path string, h func(q url.Values) string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers[path] = h
}

// serve runs one handler at a time, so handlers can keep state.
func (f *fake) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	h := f.handlers[r.URL.Path]
	if name := path.Base(r.URL.Path); name != "getOpenSubsonicExtensions" {
		f.calls = append(f.calls, name)
	}

	body := `{"status":"failed","version":"1.16.1","error":{"code":70,"message":"not found"}}`
	if h == nil {
		f.t.Errorf("unexpected request %s", r.URL.Path)
	} else {
		body = h(r.URL.Query())
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"subsonic-response":%s}`, body)
}

// called returns the endpoints called so far and forgets them.
func (f *fake) called() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	calls := f.calls
	f.calls = nil
	return calls
}

// ok wraps fields, the JSON members that go next to "status", in a
// successful response.
func ok(fields string) string {
	if fields != "" {
		fields = "," + fields
	}
	return `{"status":"ok","version":"1.16.1"` + fields + `}`
}

// open logs in to f and forgets the login requests.
func open(t *testing.T, f *fake) *Server {
	t.Helper()
	s, err := Open(f.url, "secret")
	if err != nil {
		t.Fatal(err)
	}
	s.poll = time.Millisecond
	f.called()
	return s
}

func TestOpen(t *testing.T) {
	f := newFake(t)
	f.handle("/rest/ping", func(q url.Values) string {
		want := fmt.Sprintf("%x", md5.Sum([]byte("secret"+q.Get("s"))))
		if q.Get("u") != "alice" || q.Get("c") != "commonjukebox" || q.Get("f") != "json" || q.Get("t") != want {
			t.Errorf("ping got u=%q c=%q f=%q t=%q, want alice, commonjukebox, json and the salted password",
				q.Get("u"), q.Get("c"), q.Get("f"), q.Get("t"))
		}
		return ok("")
	})
	if _, err := Open(f.url, "secret"); err != nil {
		t.Fatal(err)
	}
	if got := f.called(); !slices.Equal(got, []string{"ping"}) {
		t.Errorf("calls = %v, want [ping]", got)
	}
}

func TestOpenWrongPassword(t *testing.T) {
	f := newFake(t)
	f.handle("/rest/ping", func(url.Values) string {
		return `{"status":"failed","version":"1.16.1","error":{"code":40,"message":"wrong username or password"}}`
	})
	_, err := Open(f.url, "nope")
	if err == nil {
		t.Fatal("Open with a wrong password succeeded")
	}
	host := strings.TrimPrefix(f.url, "http://")
	if !strings.Contains(err.Error(), "wrong password for "+host) {
		t.Errorf("error %q doesn't say the password for %s is wrong", err, host)
	}
}

func TestOpenKeepsPath(t *testing.T) {
	f := newFake(t)
	f.handle("/rest/ping", func(url.Values) string {
		t.Error("the API was called without the /music prefix")
		return ok("")
	})
	f.handle("/music/rest/ping", func(url.Values) string { return ok("") })
	if _, err := Open(f.url+"/music", "secret"); err != nil {
		t.Fatal(err)
	}
}

// song is a search3 or playlist entry.
func song(id, path string) string {
	return fmt.Sprintf(`{"id":%q,"isDir":false,"title":"t","path":%q}`, id, path)
}

func TestSongsPages(t *testing.T) {
	f := newFake(t)
	f.handle("/rest/search3", func(q url.Values) string {
		if q.Get("query") != `""` || q.Get("songCount") != "500" || q.Get("artistCount") != "0" || q.Get("albumCount") != "0" {
			t.Errorf("search3 got %v", q)
		}
		offset, _ := strconv.Atoi(q.Get("songOffset"))
		n := 0
		switch offset {
		case 0:
			n = 500
		case 500:
			n = 2
		default:
			t.Errorf("search3 got songOffset %q", q.Get("songOffset"))
		}
		var songs []string
		for i := offset; i < offset+n; i++ {
			songs = append(songs, song(strconv.Itoa(i), fmt.Sprintf("Artist/%d.flac", i)))
		}
		return ok(`"searchResult3":{"song":[` + strings.Join(songs, ",") + `]}`)
	})
	songs, err := open(t, f).Songs()
	if err != nil {
		t.Fatal(err)
	}
	if len(songs) != 502 || songs["Artist/0.flac"] != "0" || songs["Artist/501.flac"] != "501" {
		t.Errorf("got %d songs, Artist/0.flac=%q, Artist/501.flac=%q; want 502, 0, 501",
			len(songs), songs["Artist/0.flac"], songs["Artist/501.flac"])
	}
	if got := f.called(); !slices.Equal(got, []string{"search3", "search3"}) {
		t.Errorf("calls = %v, want two search3 pages", got)
	}
}

func TestSongsEmptyLibrary(t *testing.T) {
	f := newFake(t)
	f.handle("/rest/search3", func(url.Values) string { return ok(`"searchResult3":{}`) })
	songs, err := open(t, f).Songs()
	if err != nil {
		t.Fatal(err)
	}
	if len(songs) != 0 {
		t.Errorf("got %v, want no songs", songs)
	}
}

const created = `"created":"2026-01-02T03:04:05Z"`

func playlists(lists ...string) func(url.Values) string {
	return func(url.Values) string {
		return ok(`"playlists":{"playlist":[` + strings.Join(lists, ",") + `]}`)
	}
}

var (
	roadTrip = `{"id":"3","name":"Road Trip","comment":"summer","owner":"alice","public":true,"songCount":2,"duration":400,` + created + `}`
	chill    = `{"id":"7","name":"Chill","owner":"alice","songCount":0,"duration":0,` + created + `}`
)

func TestPlaylists(t *testing.T) {
	f := newFake(t)
	f.handle("/rest/getPlaylists", playlists(roadTrip, chill))
	got, err := open(t, f).Playlists()
	if err != nil {
		t.Fatal(err)
	}
	want := []Playlist{
		{ID: "3", Name: "Road Trip", Comment: "summer", Public: true, SongCount: 2},
		{ID: "7", Name: "Chill"},
	}
	if !slices.EqualFunc(got, want, equal) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func equal(a, b Playlist) bool {
	return a.ID == b.ID && a.Name == b.Name && a.Comment == b.Comment &&
		a.Public == b.Public && a.SongCount == b.SongCount && slices.Equal(a.Songs, b.Songs)
}

func TestGet(t *testing.T) {
	f := newFake(t)
	f.handle("/rest/getPlaylist", func(q url.Values) string {
		if q.Get("id") != "3" {
			t.Errorf("getPlaylist got id %q, want 3", q.Get("id"))
		}
		return ok(`"playlist":{"id":"3","name":"Road Trip","comment":"summer","public":true,"songCount":2,"duration":400,` +
			created + `,"entry":[` + song("t1", "A/01.flac") + "," + song("t2", "B/02.flac") + `]}`)
	})
	got, err := open(t, f).Get("3")
	if err != nil {
		t.Fatal(err)
	}
	want := Playlist{ID: "3", Name: "Road Trip", Comment: "summer", Public: true, SongCount: 2,
		Songs: []Song{{"t1", "A/01.flac"}, {"t2", "B/02.flac"}}}
	if !equal(*got, want) {
		t.Errorf("got %+v, want %+v", *got, want)
	}
}

func TestFind(t *testing.T) {
	f := newFake(t)
	f.handle("/rest/getPlaylists", playlists(roadTrip, chill))
	s := open(t, f)
	p, err := s.Find("road TRIP")
	if err != nil {
		t.Fatal(err)
	}
	if p == nil || p.ID != "3" {
		t.Errorf("Find(road TRIP) = %+v, want playlist 3", p)
	}
	p, err = s.Find("missing")
	if err != nil || p != nil {
		t.Errorf("Find(missing) = %+v, %v; want nil, nil", p, err)
	}
}

func TestFindDuplicate(t *testing.T) {
	f := newFake(t)
	f.handle("/rest/getPlaylists", playlists(
		`{"id":"pl-a","name":"Mix","songCount":0,"duration":0,`+created+`}`,
		chill,
		`{"id":"pl-b","name":"MIX","songCount":0,"duration":0,`+created+`}`,
	))
	_, err := open(t, f).Find("mix")
	if err == nil {
		t.Fatal("Find returned no error for two playlists with the same name")
	}
	if !strings.Contains(err.Error(), "pl-a") || !strings.Contains(err.Error(), "pl-b") {
		t.Errorf("error %q doesn't list both playlists", err)
	}
}

func TestCreate(t *testing.T) {
	f := newFake(t)
	f.handle("/rest/createPlaylist", func(q url.Values) string {
		if q.Get("name") != "New" || q.Has("playlistId") || !slices.Equal(q["songId"], []string{"t1", "t2"}) {
			t.Errorf("createPlaylist got %v", q)
		}
		return ok("")
	})
	if err := open(t, f).Create("New", []string{"t1", "t2"}); err != nil {
		t.Fatal(err)
	}
	if got := f.called(); !slices.Equal(got, []string{"createPlaylist"}) {
		t.Errorf("calls = %v, want [createPlaylist]", got)
	}
}

func TestAppend(t *testing.T) {
	f := newFake(t)
	f.handle("/rest/updatePlaylist", func(q url.Values) string {
		if q.Get("playlistId") != "3" || q.Has("songIndexToRemove") || !slices.Equal(q["songIdToAdd"], []string{"t1", "t2"}) {
			t.Errorf("updatePlaylist got %v", q)
		}
		return ok("")
	})
	if err := open(t, f).Append("3", []string{"t1", "t2"}); err != nil {
		t.Fatal(err)
	}
	if got := f.called(); !slices.Equal(got, []string{"updatePlaylist"}) {
		t.Errorf("calls = %v, want [updatePlaylist]", got)
	}
}

func TestReplace(t *testing.T) {
	f := newFake(t)
	f.handle("/rest/createPlaylist", func(q url.Values) string {
		if q.Get("playlistId") != "3" || q.Get("name") != "Road Trip" || !slices.Equal(q["songId"], []string{"t2", "t1", "t2"}) {
			t.Errorf("createPlaylist got %v", q)
		}
		return ok("")
	})
	f.handle("/rest/updatePlaylist", func(q url.Values) string {
		if q.Get("playlistId") != "3" || q.Get("comment") != "summer" || q.Get("public") != "true" ||
			q.Has("songIdToAdd") || q.Has("songIndexToRemove") {
			t.Errorf("updatePlaylist got %v", q)
		}
		return ok("")
	})
	p := Playlist{ID: "3", Name: "Road Trip", Comment: "summer", Public: true}
	if err := open(t, f).Replace(p, []string{"t2", "t1", "t2"}); err != nil {
		t.Fatal(err)
	}
	if got := f.called(); !slices.Equal(got, []string{"createPlaylist", "updatePlaylist"}) {
		t.Errorf("calls = %v, want [createPlaylist updatePlaylist]", got)
	}
}

func TestReplaceKeepsPrivate(t *testing.T) {
	f := newFake(t)
	f.handle("/rest/createPlaylist", func(url.Values) string { return ok("") })
	f.handle("/rest/updatePlaylist", func(q url.Values) string {
		if q.Get("public") != "false" {
			t.Errorf("updatePlaylist got public=%q, want false", q.Get("public"))
		}
		return ok("")
	})
	if err := open(t, f).Replace(Playlist{ID: "7", Name: "Chill"}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestDelete(t *testing.T) {
	f := newFake(t)
	f.handle("/rest/deletePlaylist", func(q url.Values) string {
		if q.Get("id") != "3" {
			t.Errorf("deletePlaylist got id %q, want 3", q.Get("id"))
		}
		return ok("")
	})
	if err := open(t, f).Delete("3"); err != nil {
		t.Fatal(err)
	}
}

func TestPlaylistError(t *testing.T) {
	f := newFake(t)
	f.handle("/rest/deletePlaylist", func(url.Values) string {
		return `{"status":"failed","version":"1.16.1","error":{"code":50,"message":"not your playlist"}}`
	})
	err := open(t, f).Delete("3")
	if err == nil || !strings.Contains(err.Error(), "not your playlist") {
		t.Errorf("Delete error = %v, want the server's message", err)
	}
}

func scanning(on bool) string {
	return ok(fmt.Sprintf(`"scanStatus":{"scanning":%t,"count":10}`, on))
}

func TestScan(t *testing.T) {
	f := newFake(t)
	f.handle("/rest/startScan", func(url.Values) string { return scanning(true) })
	polls := 0
	f.handle("/rest/getScanStatus", func(url.Values) string {
		polls++
		return scanning(polls < 3)
	})
	s := open(t, f)

	if err := s.Scan(false); err != nil {
		t.Fatal(err)
	}
	if got := f.called(); !slices.Equal(got, []string{"startScan"}) {
		t.Errorf("Scan(false) calls = %v, want [startScan]", got)
	}

	if err := s.Scan(true); err != nil {
		t.Fatal(err)
	}
	want := []string{"startScan", "getScanStatus", "getScanStatus", "getScanStatus"}
	if got := f.called(); !slices.Equal(got, want) {
		t.Errorf("Scan(true) calls = %v, want %v", got, want)
	}
}
