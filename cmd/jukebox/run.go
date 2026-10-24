package main

import (
	"cmp"
	"errors"
	"flag"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/danielhejduk/commonjukebox/internal/add"
	"github.com/danielhejduk/commonjukebox/internal/check"
	"github.com/danielhejduk/commonjukebox/internal/config"
	"github.com/danielhejduk/commonjukebox/internal/library"
	"github.com/danielhejduk/commonjukebox/internal/tags"
	"github.com/danielhejduk/commonjukebox/internal/transcode"
)

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

func runList(args []string) error {
	fs, lib := newFlagSet("list")
	artist := fs.String("artist", "", "only tracks by this artist")
	album := fs.String("album", "", "only tracks from this album")
	genre := fs.String("genre", "", "only tracks in this genre")
	stats := fs.Bool("stats", false, "print a summary of the matching tracks instead of the tracks")
	cfg, err := parseWithConfig(fs, lib, args)
	if err != nil {
		return err
	}
	tracks, err := loadTracks(cfg.Library)
	if err != nil {
		return err
	}
	var matched []tags.Track
	for _, t := range tracks {
		if matches(*artist, t.Artist) && matches(*album, t.Album) && matches(*genre, t.Genre) {
			matched = append(matched, t)
		}
	}
	if *stats {
		printStats(matched)
		return nil
	}
	for _, t := range matched {
		printTrack(t)
	}
	return nil
}

// printStats summarizes tracks: totals, tracks per decade, the biggest
// artists and genres, and tags worth fixing. An album is told apart by its
// album artist (or artist) and title, like the sort order in list.
func printStats(ts []tags.Track) {
	albums := map[[2]string]bool{}
	noArt := map[[2]string]bool{}
	artists := map[string]int{}
	genres := map[string]int{}
	decades := map[string]int{}
	noYear := 0
	for _, t := range ts {
		key := [2]string{cmp.Or(t.AlbumArtist, t.Artist), t.Album}
		albums[key] = true
		if t.Art == "" {
			noArt[key] = true
		}
		artists[t.Artist]++
		if t.Genre != "" {
			genres[t.Genre]++
		}
		if y, err := strconv.Atoi(t.Year); err == nil && len(t.Year) == 4 {
			decades[fmt.Sprintf("%ds", y/10*10)]++
		} else {
			noYear++
		}
	}

	fmt.Printf("%d tracks, %d albums, %d artists, %d genres\n",
		len(ts), len(albums), len(artists), len(genres))

	if len(decades) > 0 {
		fmt.Println("\nBy decade")
		names := slices.Sorted(maps.Keys(decades))
		printBars(names, decades)
	}
	if len(artists) > 0 {
		fmt.Println("\nTop artists")
		printBars(topKeys(artists, 10), artists)
	}
	if len(genres) > 0 {
		fmt.Println("\nTop genres")
		printBars(topKeys(genres, 10), genres)
	}

	if len(noArt) > 0 || noYear > 0 {
		fmt.Println()
		if len(noArt) > 0 {
			fmt.Printf("%d albums without cover art\n", len(noArt))
		}
		if noYear > 0 {
			fmt.Printf("%d tracks without a year\n", noYear)
		}
	}
}

// topKeys returns the n keys with the highest counts, ties broken by name.
func topKeys(counts map[string]int, n int) []string {
	keys := slices.Collect(maps.Keys(counts))
	slices.SortFunc(keys, func(a, b string) int {
		return cmp.Or(cmp.Compare(counts[b], counts[a]), cmp.Compare(a, b))
	})
	return keys[:min(n, len(keys))]
}

// printBars prints one line per key: the key, a bar scaled to the largest
// count and the count.
func printBars(keys []string, counts map[string]int) {
	const width = 30
	label, most := 0, 0
	for _, k := range keys {
		label = max(label, utf8.RuneCountInString(k))
		most = max(most, counts[k])
	}
	for _, k := range keys {
		n := counts[k]
		bar := strings.Repeat("█", max(1, n*width/most))
		pad := strings.Repeat(" ", label-utf8.RuneCountInString(k))
		fmt.Printf("  %s%s  %-*s %d\n", k, pad, width, bar, n)
	}
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
