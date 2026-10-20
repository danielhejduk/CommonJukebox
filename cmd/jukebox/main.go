package main

import (
	"cmp"
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/danielhejduk/commonjukebox/internal/config"
	"github.com/danielhejduk/commonjukebox/internal/library"
	"github.com/danielhejduk/commonjukebox/internal/tags"
)

const usage = `Usage: jukebox <command> [flags]

Commands:
  init       Set the library path in the config file
  list       List tracks in the library
  search     Search tracks by artist, album or title
  tag        Show or set tags (tag show | tag set)

Run "jukebox <command> -h" for command flags.
`

type command func(args []string) error

var commands = map[string]command{
	"init":   runInit,
	"list":   runList,
	"search": runSearch,
	"tag":    runTag,
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	name := os.Args[1]
	if name == "-h" || name == "--help" || name == "help" {
		fmt.Print(usage)
		return
	}

	cmd, ok := commands[name]
	if !ok {
		fmt.Fprintf(os.Stderr, "jukebox: unknown command %q\n\n%s", name, usage)
		os.Exit(2)
	}

	if err := cmd(os.Args[2:]); err != nil {
		fmt.Fprintf(os.Stderr, "jukebox %s: %v\n", name, err)
		os.Exit(1)
	}
}

// newFlagSet returns a FlagSet for a subcommand with the shared --library flag.
func newFlagSet(name string) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	lib := fs.String("library", "", "path to music library (overrides config file)")
	return fs, lib
}

// parseWithConfig parses the subcommand's flags and loads the config.
func parseWithConfig(fs *flag.FlagSet, lib *string, args []string) (config.Config, error) {
	fs.Parse(args)
	return config.Load(*lib)
}

// runInit writes the library path to the config file, so other commands
// don't need --library.
func runInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	force := fs.Bool("force", false, "replace a library that is already set")
	codec := fs.String("codec", "", "codec for the library: "+strings.Join(config.Codecs, ", ")+" (default "+config.DefaultSettings().Codec+")")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return errors.New("usage: jukebox init [--force] [--codec name] <dir>")
	}

	lib, err := config.AbsPath(fs.Arg(0))
	if err != nil {
		return err
	}
	info, err := os.Stat(lib)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s: not a directory", lib)
	}

	files, err := library.Scan(lib)
	if err != nil {
		return err
	}
	// Settings first, so a library we can't write to doesn't become the default.
	settings, err := config.InitSettings(lib, *codec)
	if err != nil {
		return err
	}
	path, err := config.SetLibrary(lib, *force)
	if err != nil {
		return err
	}

	fmt.Printf("library set to %s in %s (%d audio files)\n", lib, path, len(files))
	fmt.Printf("codec %s in %s\n", settings.Codec, config.SettingsPath(lib))
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "warning: no .flac, .ogg or .opus files found")
	}
	return nil
}

func runList(args []string) error {
	fs, lib := newFlagSet("list")
	artist := fs.String("artist", "", "only tracks by this artist")
	album := fs.String("album", "", "only tracks from this album")
	genre := fs.String("genre", "", "only tracks in this genre")
	cfg, err := parseWithConfig(fs, lib, args)
	if err != nil {
		return err
	}

	tracks, err := loadTracks(cfg.Library)
	if err != nil {
		return err
	}
	for _, t := range tracks {
		if matches(*artist, t.Artist) && matches(*album, t.Album) && matches(*genre, t.Genre) {
			printTrack(t)
		}
	}
	return nil
}

func runSearch(args []string) error {
	fs, lib := newFlagSet("search")
	cfg, err := parseWithConfig(fs, lib, args)
	if err != nil {
		return err
	}
	query := strings.ToLower(strings.Join(fs.Args(), " "))
	if query == "" {
		return errors.New("usage: jukebox search [--library path] <text>")
	}

	tracks, err := loadTracks(cfg.Library)
	if err != nil {
		return err
	}
	for _, t := range tracks {
		if containsText(t, query) {
			printTrack(t)
		}
	}
	return nil
}

// loadTracks reads the tags of every audio file in the library, sorted by
// artist, album and track number. Unreadable files are reported and skipped.
func loadTracks(root string) ([]tags.Track, error) {
	files, err := library.Scan(root)
	if err != nil {
		return nil, err
	}

	var tracks []tags.Track
	for _, f := range files {
		t, err := tags.Read(f)
		if err != nil {
			fmt.Fprintln(os.Stderr, "warning:", err)
			continue
		}
		tracks = append(tracks, t)
	}

	slices.SortFunc(tracks, func(a, b tags.Track) int {
		return cmp.Or(
			cmp.Compare(a.Artist, b.Artist),
			cmp.Compare(a.Album, b.Album),
			cmp.Compare(a.Track, b.Track),
		)
	})
	return tracks, nil
}

// matches reports whether got equals want, ignoring case. An empty want matches anything.
func matches(want, got string) bool {
	return want == "" || strings.EqualFold(want, got)
}

// containsText reports whether the artist, album or title contains query (already lowercase).
func containsText(t tags.Track, query string) bool {
	for _, s := range []string{t.Artist, t.Album, t.Title} {
		if strings.Contains(strings.ToLower(s), query) {
			return true
		}
	}
	return false
}

func printTrack(t tags.Track) {
	fmt.Printf("%s — %s — %d. %s\n", t.Artist, t.Album, t.Track, t.Title)
}

// runTag works on the paths it is given, so it doesn't need the library.
func runTag(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "show":
			return runTagShow(args[1:])
		case "set":
			return runTagSet(args[1:])
		}
	}
	return errors.New("usage: jukebox tag show <file|dir>... | jukebox tag set [flags] <file|dir>...")
}

func runTagShow(paths []string) error {
	if len(paths) == 0 {
		return errors.New("usage: jukebox tag show <file|dir>...")
	}
	return forEachFile(paths, func(file string) error {
		comments, err := tags.Comments(file)
		if err != nil {
			return err
		}
		fmt.Println(file)
		for _, c := range comments {
			fmt.Println("  " + c)
		}
		return nil
	})
}

// tagFlags maps each "tag set" flag to the tag it writes.
var tagFlags = map[string]string{
	"artist": "ARTIST",
	"album":  "ALBUM",
	"title":  "TITLE",
	"genre":  "GENRE",
	"track":  "TRACKNUMBER",
	"year":   "DATE",
}

// perTrackFlags are tags that differ between tracks, so setting one value on
// many files at once is almost always a mistake.
var perTrackFlags = []string{"title", "track"}

func runTagSet(args []string) error {
	fs := flag.NewFlagSet("tag set", flag.ExitOnError)
	for name, key := range tagFlags {
		fs.String(name, "", "set "+key+" (empty removes it)")
	}
	dryRun := fs.Bool("dry-run", false, "print what would change without writing")
	force := fs.Bool("force", false, "allow --title or --track on more than one file")
	fs.Parse(args)

	// Only tag flags that were actually passed are changed.
	changes := map[string]string{}
	var perTrack []string
	fs.Visit(func(f *flag.Flag) {
		if key, ok := tagFlags[f.Name]; ok {
			changes[key] = f.Value.String()
		}
		if slices.Contains(perTrackFlags, f.Name) {
			perTrack = append(perTrack, "--"+f.Name)
		}
	})
	if len(changes) == 0 || fs.NArg() == 0 {
		return errors.New("usage: jukebox tag set [--dry-run] [--force] [--artist X] [--album X] [--title X] [--genre X] [--track N] [--year N] <file|dir>...")
	}

	files, errs := collectFiles(fs.Args())
	if len(perTrack) > 0 && len(files) > 1 && !*force {
		return fmt.Errorf("%s would give %d files the same value; pass --force if you mean it", strings.Join(perTrack, " and "), len(files))
	}

	errs = append(errs, eachFile(files, func(file string) error {
		if *dryRun {
			fmt.Println("would update", file)
			return nil
		}
		if err := tags.Write(file, changes); err != nil {
			return err
		}
		fmt.Println("updated", file)
		return nil
	})...)
	return joinErrors(errs)
}

// forEachFile calls fn for every audio file in paths; directories are searched.
// It keeps going after errors and returns all of them at the end.
func forEachFile(paths []string, fn func(file string) error) error {
	files, errs := collectFiles(paths)
	errs = append(errs, eachFile(files, fn)...)
	return joinErrors(errs)
}

// collectFiles returns every audio file in paths, plus an error for each path
// that can't be scanned or holds no audio files.
func collectFiles(paths []string) ([]string, []error) {
	var files []string
	var errs []error
	for _, p := range paths {
		found, err := library.Scan(p)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if len(found) == 0 {
			errs = append(errs, fmt.Errorf("%s: no audio files", p))
			continue
		}
		files = append(files, found...)
	}
	return files, errs
}

// eachFile calls fn for every file and returns the errors it reported.
func eachFile(files []string, fn func(file string) error) []error {
	var errs []error
	for _, f := range files {
		if err := fn(f); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// joinErrors combines errs into one error that reports how many there were.
func joinErrors(errs []error) error {
	switch len(errs) {
	case 0:
		return nil
	case 1:
		return errs[0]
	}
	return fmt.Errorf("%d errors:\n%w", len(errs), errors.Join(errs...))
}
