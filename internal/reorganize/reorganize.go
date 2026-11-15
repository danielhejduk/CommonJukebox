package reorganize

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/danielhejduk/commonjukebox/internal/layout"
	"github.com/danielhejduk/commonjukebox/internal/library"
	"github.com/danielhejduk/commonjukebox/internal/tags"
)

type Move struct {
	From, To string
}

type Plan struct {
	Moves []Move
	Warnings []string
	Skipped []error
	Collisions []error
}

func Build(root string, tracks []tags.Track, only []string) (Plan, error) {
	root = filepath.Clean(root)
	inScope, err := scope(root, only)
	if err != nil {
		return Plan{}, err
	}

	var p Plan
	var placed []layout.Track
	for _, t := range tracks {
		if missing := layout.Missing(t); len(missing) > 0 {
			if inScope(t.Path) {
				p.Skipped = append(p.Skipped, fmt.Errorf("%s: missing-tag: %s not set", t.Path, strings.Join(missing, ", ")))
			}
			continue
		}
		placed = append(placed, layout.Track{Track: t, Ext: strings.ToLower(filepath.Ext(t.Path))})
	}
	dests, warnings := layout.Place(root, placed)
	for _, w := range warnings {
		if inScope(w.Path) {
			p.Warnings = append(p.Warnings, w.String())
		}
	}

	dest := map[string]string{}
	for i, t := range placed {
		if inScope(t.Path) && dests[i] != t.Path {
			dest[t.Path] = dests[i]
			p.Moves = append(p.Moves, Move{From: t.Path, To: dests[i]})
		}
	}

	dirs := map[string]bool{}
	for from := range dest {
		dirs[filepath.Dir(from)] = true
	}
	for _, dir := range slices.Sorted(maps.Keys(dirs)) {
		moves, warning, err := leftovers(root, dir, dest)
		if err != nil {
			return Plan{}, err
		}
		p.Moves = append(p.Moves, moves...)
		if warning != "" {
			p.Warnings = append(p.Warnings, warning)
		}
	}

	slices.SortFunc(p.Moves, func(a, b Move) int { return cmp.Compare(a.From, b.From)})
	p.Collisions = collisions(p.Moves)
	return p, nil
}

func scope(root string, only []string) (func(string) bool, error) {
	var dirs []string
	for _, o := range only {
		abs, err := filepath.Abs(o)
		if err != nil {
			return nil, err
		}
		rootAbs, err := filepath.Abs(root)
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(rootAbs, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("%s: not in the library %s", o, root)
		}
		dirs = append(dirs, filepath.Join(root, rel))
	}
	return func (p string) bool {
		if len(dirs) == 0 {
			return true
		}
		for _, d := range dirs {
			if p == d || d == root || strings.HasPrefix(p, d+string(filepath.Separator)) {
				return true
			}
		}
		return false
	}, nil
}

func leftovers(root, dir string, dest map[string]string) ([]Move, string, error) {
	if dir == root {
		return nil, "", nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, "", err
	}
	targets := map[string]bool{}
	var others []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(dir, name)
		if library.IsAudio(path) {
			target := dir
			if to, ok := dest[path]; ok {
				target = filepath.Dir(to)
			}
			targets[target] = true
			continue
		}
		others = append(others, path)
	}
	if len(others) == 0 || len(targets) == 1 && targets[dir] {
		return nil, "", nil
	}
	if len(targets) > 1 {
		return nil, fmt.Sprintf("%s: its tracks go to %d folders, so %d other files stay where they are", dir, len(targets), len(others)), nil
	}
	to := slices.Collect(maps.Keys(targets))[0]
	moves := make([]Move, len(others))
	for i, o := range others {
		moves[i] = Move{From: o, To: filepath.Join(to, filepath.Base(o))}
	}
	return moves, "", nil
}

func collisions(moves []Move) []error {
	sources := map[string]string{}
	for _, m := range moves {
		sources[strings.ToLower(m.From)] = m.From
	}
	var errs []error
	aimed := map[string]string{}
	for _, m := range moves {
		key := strings.ToLower(m.To)
		if other, ok := aimed[key]; ok {
			errs = append(errs, fmt.Errorf("%s: both %s and %s would move there", m.To, other, m.From))
			continue
		}
		aimed[key] = m.From

		info, err := os.Lstat(m.To)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}

		if src, ok := sources[key]; ok && sameFile(src, info) {
			continue
		}
		errs = append(errs, fmt.Errorf("%s: already exists; %s would replace it", m.To, m.From))
	}
	return errs
}

func sameFile(path string, info fs.FileInfo) bool {
	other, err := os.Lstat(path)
	return err == nil && os.SameFile(other, info)
}

func Execute(root string, moves []Move) ([]Move, []error) {
	root = filepath.Clean(root)
	tmps := make([]string, len(moves))
	for i, m := range moves {
		tmp, err := stage(m)
		if err == nil {
			tmps[i] = tmp
			continue
		}
		errs := []error{fmt.Errorf("move %s: %w; nothing was moved", m.From, err)}
		dirs := map[string]bool{}
		for j := range i {
			if err := os.Rename(tmps[j], moves[j].From); err != nil {
				errs = append(errs, fmt.Errorf("%s: left at %s: %w", moves[j].From, tmps[j], err))
			}
			dirs[filepath.Dir(moves[j].To)] = true
		}
		dirs[filepath.Dir(m.To)] = true
		prune(root, dirs)
		return nil, errs
	}

	var done []Move
	var errs []error
	for i, m := range moves {
		if _, err := os.Lstat(m.To); err == nil {
			errs = append(errs, fmt.Errorf("%s: already exists; %s left as %s", m.To, m.From, tmps[i]))
			continue
		}
		if err := os.Rename(tmps[i], m.To); err != nil {
			errs = append(errs, fmt.Errorf("%s: left at %s: %w", m.From, tmps[i], err))
			continue
		}
		done = append(done, m)
	}
	dirs := map[string]bool{}
	for _, m := range moves {
		dirs[filepath.Dir(m.From)] = true
	}
	prune(root, dirs)
	return done, errs
}

func stage(m Move) (string, error) {
	dir := filepath.Dir(m.To)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, ".jukebox-move-*")
	if err != nil {
		return "", err
	}
	tmp := f.Name()
	if err = f.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(m.From, tmp); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return tmp, nil
}

func prune(root string, dirs map[string]bool) {
	for dir := range dirs {
		for ; under(root, dir); dir = filepath.Dir(dir) {
			if os.Remove(dir) != nil {
				break
			}
		}
	}
}

func under(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
