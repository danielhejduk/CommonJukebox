package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/danielhejduk/commonjukebox/internal/library"
	"github.com/danielhejduk/commonjukebox/internal/reorganize"
	"github.com/danielhejduk/commonjukebox/internal/tags"
)

func runReorganize(args []string) error {
	fs, lib := newFlagSet("reorganize")
	dryRun := fs.Bool("dry-run", false, "print what would be moved without moving anything")
	cfg, err := parseWithConfig(fs, lib, args)
	if err != nil {
		return err
	}

	files, err := library.Scan(cfg.Library)
	if err != nil {
		return err
	}
	read, readErrs := readTracks(files)
	var tracks []tags.Track
	for i := range files {
		if readErrs[i] != nil {
			fmt.Fprintln(os.Stderr, "warning:", readErrs[i], "(left where it is)")
			continue
		}
		tracks = append(tracks, read[i])
	}

	plan, err := reorganize.Build(cfg.Library, tracks, fs.Args())
	if err != nil {
		return err
	}
	for _, w := range plan.Warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	errs := plan.Skipped
	if len(plan.Collisions) > 0 {
		errs = append(errs, plan.Collisions...)
		errs = append(errs, fmt.Errorf("%d collisions; nothing was moved", len(plan.Collisions)))
		return joinErrors(errs)
	}
	if len(plan.Moves) == 0 {
		fmt.Println("nothing to reorganize")
		return joinErrors(errs)
	}

	rel := func(p string) string {
		if r, err := filepath.Rel(cfg.Library, p); err == nil {
			return filepath.ToSlash(r)
		}
		return p
	}
	if *dryRun {
		for _, m := range plan.Moves {
			fmt.Printf("move %s -> %s\n", rel(m.From), rel(m.To))
		}
		changed, err := rewritePlaylists(cfg.Library, plan.Moves, true)
		for _, name := range changed {
			fmt.Println("would update", name)
		}
		if err != nil {
			errs = append(errs, err)
		}
		return joinErrors(errs)
	}

	done, execErrs := reorganize.Execute(cfg.Library, plan.Moves)
	for _, m := range done {
		fmt.Printf("moved %s -> %s\n", rel(m.From), rel(m.To))
	}
	errs = append(errs, execErrs...)
	changed, err := rewritePlaylists(cfg.Library, done, false)
	for _, name := range changed {
		fmt.Println("updated", name)
	}
	if err != nil {
		errs = append(errs, err)
	}
	fmt.Printf("moved %d of %d files\n", len(done), len(plan.Moves))
	return joinErrors(errs)
}

func rewritePlaylists(lib string, moves []reorganize.Move, dryRun bool) ([]string, error) {
	renamed := map[string]string{} // old -> new, slash-separated and relative to lib
	for _, m := range moves {
		from, err1 := filepath.Rel(lib, m.From)
		to, err2 := filepath.Rel(lib, m.To)
		if err1 == nil && err2 == nil {
			renamed[filepath.ToSlash(from)] = filepath.ToSlash(to)
		}
	}
	if len(renamed) == 0 {
		return nil, nil
	}

	playlists, err := filepath.Glob(filepath.Join(lib, "*.m3u8"))
	if err != nil {
		return nil, err
	}
	var changed []string
	var errs []error
	for _, file := range playlists {
		data, err := os.ReadFile(file)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		lines := strings.Split(string(data), "\n")
		n := 0
		for i, line := range lines {
			text := strings.TrimSuffix(line, "\r")
			if text == "" || strings.HasPrefix(text, "#") {
				continue
			}
			if to, ok := renamed[songPath(text)]; ok {
				lines[i] = to + line[len(text):] // keep a \r line ending
				n++
			}
		}
		if n == 0 {
			continue
		}
		changed = append(changed, filepath.Base(file))
		if dryRun {
			continue
		}
		if err := writeFileAtomic(file, []byte(strings.Join(lines, "\n"))); err != nil {
			errs = append(errs, err)
		}
	}
	return changed, joinErrors(errs)
}

