package main

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/danielhejduk/commonjukebox/internal/config"
	"github.com/danielhejduk/commonjukebox/internal/gonic"
	"github.com/danielhejduk/commonjukebox/internal/library"
)

func runPlaylist(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "add":
			return runPlaylistAdd(args[1:])
		case "remove":
			return runPlaylistRemove(args[1:])
		case "list":
			return runPlaylistList(args[1:])
		case "show":
			return runPlaylistShow(args[1:])
		case "pull":
			return runPlaylistPull(args[1:])
		}
	}
	return usageError("usage: jukebox playlist add | remove | list | show ...")
}

func runPlaylistAdd(args []string) error {
	fs, lib := newFlagSet("playlist add")
	dryRun := fs.Bool("dry-run", false, "print what would change without changing the playlist")
	noScan := fs.Bool("no-scan", false, "don't ask the server to scan for the tracks it doesn't know yet")
	cfg, err := parseWithConfig(fs, lib, args)
	if err != nil {
		return err
	}
	if fs.NArg() < 3 {
		return usageError("usage: jukebox playlist add [--library path] [--dry-run] [--no-scan] <remote> <playlist> <file|dir>...")
	}
	remoteName, name := fs.Arg(0), fs.Arg(1)
	files, errs := collectFiles(library.Scan, fs.Args()[2:])
	var rels []string
	seen := map[string]bool{}
	for _, f := range files {
		rel, err := libraryPath(cfg.Library, f)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !seen[rel] {
			seen[rel] = true
			rels = append(rels, rel)
		}
	}
	if len(errs) > 0 {
		return joinErrors(errs)
	}
	srv, err := openSubsonic(cfg.Library, remoteName)
	if err != nil {
		return err
	}
	ids, missing, err := songIDs(srv, rels)
	if err != nil {
		return err
	}
	if len(missing) > 0 && !*noScan {
		fmt.Fprintf(os.Stderr, "%d tracks not on the server yet; scanning\n", len(missing))
		if err := srv.Scan(true); err != nil {
			return fmt.Errorf("scan: %w", err)
		}
		if ids, missing, err = songIDs(srv, rels); err != nil {
			return err
		}
	}
	if len(missing) > 0 {
		for _, rel := range missing {
			errs = append(errs, fmt.Errorf("%s: not on the server; run jukebox sync %s first", rel, remoteName))
		}
		return joinErrors(errs)
	}

	p, err := srv.Find(name)
	if err != nil {
		return err
	}
	if p == nil {
		if *dryRun {
			fmt.Printf("would create playlist %s with %d tracks\n", name, len(ids))
			return nil
		}
		if err := srv.Create(name, ids); err != nil {
			return err
		}
		fmt.Printf("created playlist %s with %d tracks\n", name, len(ids))
		return nil
	}
	full, err := srv.Get(p.ID)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, s := range full.Songs {
		have[s.ID] = true
	}
	var add []string
	for i, id := range ids {
		if have[id] {
			fmt.Println("already in", full.Name+":", rels[i])
			continue
		}
		add = append(add, id)
	}
	if len(add) == 0 {
		fmt.Printf("playlist %s unchanged\n", full.Name)
		return nil
	}
	if *dryRun {
		fmt.Printf("would add %d tracks to playlist %s\n", len(add), full.Name)
		return nil
	}
	if err := srv.Append(full.ID, add); err != nil {
		return err
	}
	fmt.Printf("added %d tracks to playlist %s\n", len(add), full.Name)
	return nil
}

func runPlaylistRemove(args []string) error {
	fs, lib := newFlagSet("playlist remove")
	dryRun := fs.Bool("dry-run", false, "print what would change without changing the playlist")
	cfg, err := parseWithConfig(fs, lib, args)
	if err != nil {
		return err
	}
	if fs.NArg() < 2 {
		return usageError("usage: jukebox playlist remove [--library path] [--dry-run] <remote> <playlist> [file|dir]...")
	}
	remoteName, name := fs.Arg(0), fs.Arg(1)

	var targets []string
	var errs []error
	for _, arg := range fs.Args()[2:] {
		rel, err := libraryPath(cfg.Library, arg)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		targets = append(targets, rel)
	}
	if len(errs) > 0 {
		return joinErrors(errs)
	}

	srv, err := openSubsonic(cfg.Library, remoteName)
	if err != nil {
		return err
	}
	p, err := findPlaylist(srv, remoteName, name)
	if err != nil {
		return err
	}

	if len(targets) == 0 {
		if *dryRun {
			fmt.Printf("would delete playlist %s (%d tracks)\n", p.Name, p.SongCount)
			return nil
		}
		if err := srv.Delete(p.ID); err != nil {
			return err
		}
		fmt.Printf("deleted playlist %s\n", p.Name)
		return nil
	}

	full, err := srv.Get(p.ID)
	if err != nil {
		return err
	}
	used := make([]bool, len(targets))
	var keep []string
	removed := 0
	for _, s := range full.Songs {
		i := slices.IndexFunc(targets, func(t string) bool { return underPath(songPath(s.Path), t) })
		if i < 0 {
			keep = append(keep, s.ID)
			continue
		}
		used[i] = true
		removed++
		if *dryRun {
			fmt.Println("would remove", songPath(s.Path))
		} else {
			fmt.Println("removing", songPath(s.Path))
		}
	}
	for i, t := range targets {
		if !used[i] {
			errs = append(errs, fmt.Errorf("%s: not in playlist %s", t, full.Name))
		}
	}
	if removed == 0 || *dryRun {
		return joinErrors(errs)
	}
	if err := srv.Replace(*full, keep); err != nil {
		return joinErrors(append(errs, err))
	}
	fmt.Printf("removed %d tracks from playlist %s\n", removed, full.Name)
	return joinErrors(errs)
}

func runPlaylistList(args []string) error {
	fs, lib := newFlagSet("playlist list")
	cfg, err := parseWithConfig(fs, lib, args)
	if err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return usageError("usage: jukebox playlist list [--library path] <remote>")
	}
	srv, err := openSubsonic(cfg.Library, fs.Arg(0))
	if err != nil {
		return err
	}
	lists, err := srv.Playlists()
	if err != nil {
		return err
	}
	slices.SortFunc(lists, func(a, b gonic.Playlist) int { return compareText(a.Name, b.Name) })
	for _, p := range lists {
		fmt.Println(playlistLine(p))
	}
	return nil
}

func playlistLine(p gonic.Playlist) string {
	line := fmt.Sprintf("%s\t%d tracks", p.Name, p.SongCount)
	if p.SongCount == 1 {
		line = p.Name + "\t1 track"
	}
	var notes []string
	if p.Public {
		notes = append(notes, "public")
	}
	if p.Comment != "" {
		notes = append(notes, p.Comment)
	}
	if len(notes) > 0 {
		line += " (" + strings.Join(notes, ", ") + ")"
	}
	return line
}

func runPlaylistShow(args []string) error {
	fs, lib := newFlagSet("playlist show")
	cfg, err := parseWithConfig(fs, lib, args)
	if err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return usageError("usage: jukebox playlist show [--library path] <remote> <playlist>")
	}
	remoteName := fs.Arg(0)
	srv, err := openSubsonic(cfg.Library, remoteName)
	if err != nil {
		return err
	}
	p, err := findPlaylist(srv, remoteName, fs.Arg(1))
	if err != nil {
		return err
	}
	full, err := srv.Get(p.ID)
	if err != nil {
		return err
	}
	for _, s := range full.Songs {
		rel := songPath(s.Path)
		if _, err := os.Stat(filepath.Join(cfg.Library, filepath.FromSlash(rel))); err != nil {
			fmt.Println(rel + "\t(not in library)")
			continue
		}
		fmt.Println(rel)
	}
	if note := showNote(p.Name, p.SongCount, len(full.Songs)); note != "" {
		fmt.Fprintln(os.Stderr, note)
	}
	return nil
}

func runPlaylistPull(args []string) error {
	fs, lib := newFlagSet("playlist pull")
	cfg, err := parseWithConfig(fs, lib, args)
	if err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return usageError("usage: jukebox playlist pull [--library path] <remote>")
	}
	remoteName := fs.Arg(0)
	srv, err := openSubsonic(cfg.Library, remoteName)
	lists, err := srv.Playlists()
	return nil
}

// showNote explains what show leaves out. gonic's getPlaylists counts every
// path in the playlist file, but getPlaylist skips the paths it can't find,
// so listed, from the first, above got, from the second, means broken entries.
func showNote(name string, listed, got int) string {
	if listed > got {
		tracks := "tracks"
		if listed == 1 {
			tracks = "track"
		}
		return fmt.Sprintf("%s: the server can't find the files of %d of its %d %s; they were probably moved or deleted. Add them again with jukebox playlist add",
			name, listed-got, listed, tracks)
	}
	if got == 0 {
		return fmt.Sprintf("playlist %s is empty", name)
	}
	return ""
}

func openSubsonic(lib, name string) (*gonic.Server, error) {
	remotes, err := config.LoadRemotes(lib)
	if err != nil {
		return nil, err
	}
	rc, ok := remotes[name]
	if !ok {
		return nil, fmt.Errorf("no remote named %q; add one with jukebox remote add", name)
	}
	if rc.Subsonic == "" {
		return nil, fmt.Errorf("remote %s has no Subsonic server; set on with jukebox remote add --subsonic url %s %s", name, name, rc.URL)
	}
	u, err := url.Parse(rc.Subsonic)
	if err != nil {
		return nil, err
	}
	password, err := config.Password(u, passwordPrompter())
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(os.Stderr, "connecting to %s\n", u.Redacted())
	return gonic.Open(rc.Subsonic, password)
}

// findPlaylist is srv.Find, but a missing playlist is an error.
func findPlaylist(srv *gonic.Server, remoteName, name string) (*gonic.Playlist, error) {
	p, err := srv.Find(name)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, fmt.Errorf("no playlist named %q on %s; see jukebox playlist list %s", name, remoteName, remoteName)
	}
	return p, nil
}

func songIDs(srv *gonic.Server, rels []string) (ids, missing []string, err error) {
	songs, err := srv.Songs()
	if err != nil {
		return nil, nil, err
	}
	byPath := make(map[string]string, len(songs))
	for p, id := range songs {
		byPath[songPath(p)] = id
	}
	for _, rel := range rels {
		if id, ok := byPath[rel]; ok {
			ids = append(ids, id)
		} else {
			missing = append(missing, rel)
		}
	}
	return ids, missing, nil
}

func libraryPath(lib, p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(lib, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s: not in the library %s", p, lib)
	}
	return filepath.ToSlash(rel), nil
}

func songPath(p string) string {
	return strings.TrimPrefix(path.Clean("/"+p), "/")
}

func underPath(rel, target string) bool {
	return target == "." || rel == target || strings.HasPrefix(rel, target+"/")
}
