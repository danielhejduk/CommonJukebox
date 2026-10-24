package main

import (
	"bytes"
	"cmp"
	"errors"
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/danielhejduk/commonjukebox/internal/library"
	"github.com/danielhejduk/commonjukebox/internal/tags"
)

const artUsage = "usage: jukebox tag art show <file|dir>... | set [flags] <image> <file|dir>... | extract [--force] <file> [out] | remove [--dry-run] <file|dir>..."

const bigCover = 1 << 20

func runTagArt(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "show":
			return runArtShow(args[1:])
		case "set":
			return runArtSet(args[1:])
		case "extract":
			return runArtExtract(args[1:])
		case "remove":
			return runArtRemove(args[1:])
		}
	}
	return errors.New(artUsage)
}

func runArtShow(paths []string) error {
	if len(paths) == 0 {
		return errors.New("usage: jukebox tag art show <file|dir>...")
	}
	return forEachFile(paths, func(file string) error {
		p, err := tags.Cover(file)
		if err != nil {
			return err
		}
		fmt.Println(file)
		fmt.Printf("  %-13s %s\n", "Art:", cmp.Or(describeCover(p), "none"))
		return nil
	})
}
func runArtSet(args []string) error {
	fs := flag.NewFlagSet("tag art set", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "print what would change without writing")
	force := fs.Bool("force", false, "replace existing covers and allow a directory holding several albums")
	parseArgs(fs, args)
	if fs.NArg() < 2 {
		return errors.New("usage: jukebox tag art set [--dry-run] [--force] <image> <file|dir>...")
	}

	image := fs.Arg(0)
	data, err := os.ReadFile(image)
	if err != nil {
		return err
	}
	cover, err := tags.NewCover(data)
	if err != nil {
		return fmt.Errorf("%s: %w", image, err)
	}
	warnBigCover(image, len(data))

	t, errs := findArtTargets(fs.Args()[1:])
	if !*force {
		var wide []error
		for dir, n := range t.wide {
			wide = append(wide, fmt.Errorf("%s holds %d albums; pass --force to give them all this cover", dir, n))
		}
		if len(wide) > 0 {
			return joinErrors(wide)
		}
	}

	// Look at everything before changing anything, so a refusal leaves the
	// files as they were.
	var files []string
	var refused []error
	for _, file := range t.files {
		old, err := tags.Cover(file)
		switch {
		case err != nil:
			errs = append(errs, err)
		case old != nil && bytes.Equal(old.Data, cover.Data):
			fmt.Println("unchanged", file)
		case old != nil && !*force:
			refused = append(refused, fmt.Errorf("%s: already has a front cover (pass --force to replace it)", file))
		default:
			files = append(files, file)
		}
	}
	if !*force {
		for _, dir := range t.albums {
			conflicts, err := coverConflicts(dir, cover)
			if err != nil {
				errs = append(errs, err)
			}
			for _, c := range conflicts {
				refused = append(refused, fmt.Errorf("%s: already exists (pass --force to replace it)", c))
			}
		}
	}
	if len(refused) > 0 {
		return joinErrors(append(errs, refused...))
	}

	for _, file := range files {
		if *dryRun {
			fmt.Println("would update", file)
			continue
		}
		if err := tags.SetCover(file, &cover); err != nil {
			errs = append(errs, err)
			continue
		}
		fmt.Println("updated", file)
	}
	for _, dir := range t.albums {
		errs = append(errs, saveCover(dir, cover, *dryRun)...)
	}
	return joinErrors(errs)
}

func runArtExtract(args []string) error {
	fs := flag.NewFlagSet("tag art extract", flag.ExitOnError)
	force := fs.Bool("force", false, "replace an existing file")
	parseArgs(fs, args)
	if fs.NArg() != 1 && fs.NArg() != 2 {
		return errors.New("usage: jukebox tag art extract [--force] <file> [out]")
	}

	file := fs.Arg(0)
	p, err := tags.Cover(file)
	if err != nil {
		return err
	}
	if p == nil {
		return fmt.Errorf("%s: no front cover", file)
	}

	out := fs.Arg(1)
	if out == "" {
		out = filepath.Dir(file)
	}
	if info, err := os.Stat(out); err == nil && info.IsDir() {
		out = filepath.Join(out, library.CoverName(p.Ext()))
	}
	if old, err := os.ReadFile(out); err == nil {
		if bytes.Equal(old, p.Data) {
			fmt.Println("unchanged", out)
			return nil
		}
		if !*force {
			return fmt.Errorf("%s: already exists (pass --force to replace it)", out)
		}
	}
	if err := writeFileAtomic(out, p.Data); err != nil {
		return err
	}
	fmt.Println("wrote", out)
	return nil
}

func runArtRemove(args []string) error {
	fs := flag.NewFlagSet("tag art remove", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "print what would change without writing")
	parseArgs(fs, args)
	if fs.NArg() == 0 {
		return errors.New("usage: jukebox tag art remove [--dry-run] <file|dir>...")
	}

	t, errs := findArtTargets(fs.Args())
	errs = append(errs, eachFile(t.files, func(file string) error {
		p, err := tags.Cover(file)
		if err != nil || p == nil {
			return err
		}
		if *dryRun {
			fmt.Println("would update", file)
			return nil
		}
		if err := tags.SetCover(file, nil); err != nil {
			return err
		}
		fmt.Println("updated", file)
		return nil
	})...)

	for _, dir := range t.albums {
		covers, err := library.Covers(dir)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		errs = append(errs, eachFile(covers, func(c string) error {
			if *dryRun {
				fmt.Println("would remove", c)
				return nil
			}
			if err := os.Remove(c); err != nil {
				return err
			}
			fmt.Println("removed", c)
			return nil
		})...)
	}
	return joinErrors(errs)
}

type artTargets struct {
	files  []string       // tracks
	albums []string       // directories whose cover file goes with the tracks
	wide   map[string]int // directory arguments holding more than one album, with the count
}

// findArtTargets collects the tracks in paths. The directories holding the
// tracks of a directory argument are its albums; a file argument has none, so
// changing one track never touches the cover file the rest of its album shares.
func findArtTargets(paths []string) (artTargets, []error) {
	t := artTargets{wide: map[string]int{}}
	var errs []error
	albums := map[string]bool{}
	for _, p := range paths {
		found, scanErrs := collectFiles([]string{p})
		errs = append(errs, scanErrs...)
		t.files = append(t.files, found...)

		if info, err := os.Stat(p); err != nil || !info.IsDir() {
			continue
		}
		dirs := map[string]bool{}
		for _, f := range found {
			dirs[filepath.Dir(f)] = true
		}
		if len(dirs) > 1 {
			t.wide[p] = len(dirs)
		}
		maps.Copy(albums, dirs)
	}
	t.albums = slices.Sorted(maps.Keys(albums))
	return t, errs
}

func coverConflicts(dir string, p tags.Picture) ([]string, error) {
	covers, err := library.Covers(dir)
	if err != nil {
		return nil, err
	}
	var conflicts []string
	for _, c := range covers {
		data, err := os.ReadFile(c)
		if err != nil {
			return conflicts, err
		}
		if !bytes.Equal(data, p.Data) {
			conflicts = append(conflicts, c)
		}
	}
	return conflicts, nil
}

// saveCover makes p the cover file of dir, named after its image type, and
// removes any other cover file there.
func saveCover(dir string, p tags.Picture, dryRun bool) []error {
	covers, err := library.Covers(dir)
	if err != nil {
		return []error{err}
	}
	target := filepath.Join(dir, library.CoverName(p.Ext()))
	for _, c := range covers {
		// Keep the existing spelling: on a case-insensitive file system
		// "Cover.JPG" and "cover.jpg" are one file.
		if strings.EqualFold(filepath.Base(c), filepath.Base(target)) {
			target = c
		}
	}

	var errs []error
	if old, err := os.ReadFile(target); err != nil || !bytes.Equal(old, p.Data) {
		switch {
		case dryRun:
			fmt.Println("would write", target)
		case writeFileAtomic(target, p.Data) != nil:
			errs = append(errs, writeFileAtomic(target, p.Data))
		default:
			fmt.Println("wrote", target)
		}
	}
	for _, c := range covers {
		if c == target {
			continue
		}
		if dryRun {
			fmt.Println("would remove", c)
		} else if err := os.Remove(c); err != nil {
			errs = append(errs, err)
		} else {
			fmt.Println("removed", c)
		}
	}
	return errs
}

// writeFileAtomic writes data to a new file next to path and renames it over
// path, so a crash never leaves a half-written file.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".jukebox-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// describeCover formats a front cover for display, or gives "" for nil.
func describeCover(p *tags.Picture) string {
	if p == nil {
		return ""
	}
	format := strings.ToUpper(strings.TrimPrefix(p.MIME, "image/"))
	return fmt.Sprintf("front cover, %s %d×%d, %s", format, p.Width, p.Height, formatSize(len(p.Data)))
}

func formatSize(n int) string {
	switch {
	case n < 1<<10:
		return fmt.Sprintf("%d B", n)
	case n < 1<<20:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
}

// warnBigCover warns about a cover image over bigCover bytes.
func warnBigCover(name string, size int) {
	if size > bigCover {
		fmt.Fprintf(os.Stderr, "warning: %s is %s; every track it is embedded in grows by that much\n", name, formatSize(size))
	}
}
