package main

import (
	"bufio"
	"cmp"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/danielhejduk/commonjukebox/internal/add"
	"github.com/danielhejduk/commonjukebox/internal/check"
	"github.com/danielhejduk/commonjukebox/internal/config"
	"github.com/danielhejduk/commonjukebox/internal/library"
	"github.com/danielhejduk/commonjukebox/internal/tags"
	"github.com/danielhejduk/commonjukebox/internal/transcode"
)

const usage = `Usage: jukebox <command> [flags]

Commands:
  init       Set the library path in the config file
  add        Copy new music into the library
  list       List tracks in the library
  search     Search tracks by artist, album or title
  tag        Show or set tags (tag show | tag set)
  check      Check tags for errors and duplicates
  transcode  Encode a file with the configured codec
  remote     Manage sync remotes (remote add | list | remove)
  sync       Sync the library with a remote in both directions

Run "jukebox <command> -h" for command flags.
`

type command func(args []string) error

var version = ""

func versionString() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "devel"
}

var commands = map[string]command{
	"init":      runInit,
	"add":       runAdd,
	"list":      runList,
	"search":    runSearch,
	"tag":       runTag,
	"check":     runCheck,
	"transcode": runTranscode,
	"remote":    runRemote,
	"sync":      runSync,
	"version":   runVersion,
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

func parseArgs(fs *flag.FlagSet, args []string) {
	var pos []string
	for {
		fs.Parse(args)
		rest := fs.Args()
		if n := len(args) - len(rest); n > 0 && args[n-1] == "--" {
			pos = append(pos, rest...)
			break
		}
		if len(rest) == 0 {
			break
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
	fs.Parse(append([]string{"--"}, pos...))
}

// parseWithConfig parses the subcommand's flags and loads the config.
func parseWithConfig(fs *flag.FlagSet, lib *string, args []string) (config.Config, error) {
	parseArgs(fs, args)
	return config.Load(*lib)
}

// runInit writes the library path to the config file, so other commands
// don't need --library.
func runInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	force := fs.Bool("force", false, "replace a library that is already set")
	codec := fs.String("codec", "", "codec for the library: "+strings.Join(config.Codecs, ", ")+" (default "+config.DefaultSettings().Codec+")")
	parseArgs(fs, args)
	if fs.NArg() != 1 {
		return usageError("usage: jukebox init [--force] [--codec name] <dir>")
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

// runAdd copies files into the library, converting lossless ones to the
// library's codec and asking for tags the destination path needs.
func runAdd(args []string) error {
	fs, lib := newFlagSet("add")
	dryRun := fs.Bool("dry-run", false, "print where files would go without copying or asking")
	force := fs.Bool("force", false, "add despite check problems and replace existing files")
	cfg, err := parseWithConfig(fs, lib, args)
	if err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return usageError("usage: jukebox add [--library path] [--dry-run] [--force] <file|dir>...")
	}

	files, errs := collectFiles(fs.Args())
	var ask add.Prompter
	if !*dryRun {
		ask = stdinPrompter()
	}
	items, planErrs := add.Plan(cfg.Library, cfg.Settings, files, ask)
	errs = append(errs, planErrs...)

	for _, it := range items {
		if !it.Transcode && it.Codec != cfg.Settings.Codec {
			fmt.Fprintf(os.Stderr, "warning: %s: %s is lossy, adding it without converting to %s\n", it.Src, it.Codec, cfg.Settings.Codec)
		}
	}
	problems := add.Validate(items)
	for _, p := range problems {
		fmt.Println(p)
	}
	if len(problems) > 0 && !*force {
		errs = append(errs, fmt.Errorf("%d problems found; fix the tags or pass --force", len(problems)))
	}

	if *dryRun {
		for _, it := range items {
			action := "copy"
			if it.Transcode {
				action = "transcode " + it.Codec
			}
			fmt.Printf("%s -> %s [%s]\n", it.Src, it.Dest, action)
		}
		return joinErrors(errs)
	}
	if len(problems) > 0 && !*force {
		return joinErrors(errs)
	}

	if slices.ContainsFunc(items, func(it add.Item) bool { return it.Transcode }) {
		if _, err := exec.LookPath("ffmpeg"); err != nil {
			return joinErrors(append(errs, fmt.Errorf("converting to %s needs ffmpeg: %w", cfg.Settings.Codec, err)))
		}
	}
	for _, it := range items {
		if err := add.Execute(it, *force); err != nil {
			errs = append(errs, err)
			continue
		}
		fmt.Println("added", it.Dest)
	}
	return joinErrors(errs)
}

// stdinPrompter asks on the terminal, or returns nil when stdin isn't one so
// scripts never wait for input. Character devices like /dev/null pass the
// check, so end of input counts as no answer from then on.
func stdinPrompter() add.Prompter {
	info, err := os.Stdin.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return nil
	}
	r := bufio.NewReader(os.Stdin)
	eof := false
	return func(question, def string) (string, error) {
		if eof {
			return "", nil
		}
		if def != "" {
			fmt.Fprintf(os.Stderr, "%s [%s]: ", question, def)
		} else {
			fmt.Fprintf(os.Stderr, "%s: ", question)
		}
		line, err := r.ReadString('\n')
		if errors.Is(err, io.EOF) {
			fmt.Fprintln(os.Stderr)
			eof = true
			return strings.TrimSpace(line), nil
		}
		if err != nil {
			return "", err
		}
		return cmp.Or(strings.TrimSpace(line), def), nil
	}
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
		return usageError("usage: jukebox search [--library path] <text>")
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
// compareTracks. Unreadable files are reported and skipped.
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

	slices.SortFunc(tracks, compareTracks)
	return tracks, nil
}

func readTracks(files []string) (tracks []tags.Track, errs []error) {
	tracks = make([]tags.Track, len(files))
	errs = make([]error, len(files))
	sem := make(chan struct{}, 2*runtime.GOMAXPROCS(0))
	var wg sync.WaitGroup
	for i, f := range files {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			tracks[i], errs[i] = tags.Read(f)
		})
	}
	wg.Wait()
	return tracks, errs
}

// compareTracks orders tracks by album artist (falling back to artist, so a
// compilation stays together), album, disc and track number. Text is compared
// ignoring case.
func compareTracks(a, b tags.Track) int {
	return cmp.Or(
		compareText(sortArtist(a), sortArtist(b)),
		compareText(a.Album, b.Album),
		cmp.Compare(a.Disc, b.Disc),
		cmp.Compare(a.Track, b.Track),
		cmp.Compare(a.Path, b.Path),
	)
}

func sortArtist(t tags.Track) string {
	return cmp.Or(t.AlbumArtist, t.Artist)
}

// compareText compares ignoring case, falling back to the exact text so the
// order is stable when two strings differ only in case.
func compareText(a, b string) int {
	return cmp.Or(cmp.Compare(strings.ToLower(a), strings.ToLower(b)), cmp.Compare(a, b))
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
		case "art":
			return runTagArt(args[1:])
		}
	}
	return usageError("usage: jukebox tag show <file|dir>... | jukebox tag set [flags] <file|dir>... | jukebox tag art <show|set|extract|remove> ...")
}

func runCheck(args []string) error {
	fs, lib := newFlagSet("check")
	cfg, err := parseWithConfig(fs, lib, args)
	if err != nil {
		return err
	}
	files, err := library.Scan(cfg.Library)
	if err != nil {
		return err
	}
	read, errs := readTracks(files)
	var tracks []tags.Track
	var problems []check.Problem
	for i, f := range files {
		if errs[i] != nil {
			// tags.Read already prefixes the path; don't print it twice.
			msg := strings.TrimPrefix(errs[i].Error(), f+": ")
			problems = append(problems, check.Problem{Path: f, Rule: "unreadable", Message: msg})
			continue
		}
		tracks = append(tracks, read[i])
	}
	problems = append(problems, check.Check(tracks)...)

	for _, p := range problems {
		fmt.Println(p)
	}
	fmt.Printf("scanned %d tracks, %d errors\n", len(files), len(problems))
	if len(problems) > 0 {
		return fmt.Errorf("%d problems found", len(problems))
	}
	return nil
}

func runTranscode(args []string) error {
	fs, lib := newFlagSet("transcode")
	cfg, err := parseWithConfig(fs, lib, args)
	if err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return usageError("usage: jukebox transcode [--library path] <src> <dst>")
	}
	return transcode.Transcode(fs.Arg(0), fs.Arg(1), cfg.Settings.Codec)
}

func runVersion(args []string) error {
	fmt.Println("CommonJukebox", versionString())
	return nil
}

func runTagShow(paths []string) error {
	if len(paths) == 0 {
		return usageError("usage: jukebox tag show <file|dir>...")
	}
	return forEachFile(paths, func(file string) error {
		t, err := tags.Read(file)
		if err != nil {
			return err
		}
		fmt.Println(file)
		for _, f := range []struct {
			name, value string
		}{
			{"Artist", t.Artist},
			{"Album artist", t.AlbumArtist},
			{"Album", t.Album},
			{"Title", t.Title},
			{"Disc", number(t.Disc)},
			{"Track", number(t.Track)},
			{"Year", t.Year},
			{"Genre", t.Genre},
		} {
			if f.value != "" {
				fmt.Printf("  %-13s %s\n", f.name+":", f.value)
			}
		}
		// Lyrics span many lines, so they go last, each line indented.
		if lyrics := strings.TrimSpace(strings.ReplaceAll(t.Lyrics, "\r\n", "\n")); lyrics != "" {
			fmt.Println("  Lyrics:")
			for _, line := range strings.Split(lyrics, "\n") {
				if line == "" {
					fmt.Println()
				} else {
					fmt.Println("    " + line)
				}
			}
		}
		return nil
	})
}

// number formats n for display; 0 means missing and gives "".
func number(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
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
	parseArgs(fs, args)

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
		return usageError("usage: jukebox tag set [--dry-run] [--force] [--artist X] [--album X] [--title X] [--genre X] [--track N] [--year N] <file|dir>...")
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

// usageError wraps a usage message; these intentionally end in "..." so ST1005 does not apply.
func usageError(msg string) error {
	return errors.New(msg)
}
