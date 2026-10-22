package sync

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/danielhejduk/commonjukebox/internal/remote"
)

// Kind is what an Action does.
type Kind int

const (
	Push         Kind = iota // copy local to remote
	Pull                     // copy remote to local
	DeleteRemote             // deleted locally, so delete on the remote
	DeleteLocal              // deleted on the remote, so delete locally
	Adopt                    // new on both sides and alike; only record it
	Forget                   // gone from both sides; only drop the record
)

func (k Kind) String() string {
	return [...]string{"push", "pull", "delete remote", "delete local", "adopt", "forget"}[k]
}

// Action is one step of a sync. Local and Remote are the file's current
// state on each side, nil where it doesn't exist.
type Action struct {
	Kind          Kind
	Path          string
	Local, Remote *remote.Entry
}

// Transfers reports whether the action changes files rather than only the snapshot.
func (a Action) Transfers() bool {
	return a.Kind != Adopt && a.Kind != Forget
}

// Conflict is a file that changed on both sides since the last sync.
type Conflict struct {
	Path          string
	Local, Remote change
}

func (c Conflict) String() string {
	return fmt.Sprintf("%s: %s locally and %s on the remote", c.Path, c.Local, c.Remote)
}

// change is how one side of a file differs from the snapshot.
type change int

const (
	absent    change = iota // not there now and not in the snapshot
	unchanged               // same as in the snapshot
	added                   // there now, not in the snapshot
	modified                // size or mtime differs from the snapshot
	deleted                 // in the snapshot, not there now
)

func (c change) String() string {
	return [...]string{"absent", "unchanged", "added", "modified", "deleted"}[c]
}

func compare(cur *remote.Entry, last State, known bool) change {
	switch {
	case cur == nil && !known:
		return absent
	case cur == nil:
		return deleted
	case !known:
		return added
	case cur.Size == last.Size && cur.ModTime.Equal(last.ModTime):
		return unchanged
	}
	return modified
}

// Prefer says which side wins conflicts; the zero value skips them.
type Prefer string

const (
	PreferNone   Prefer = ""
	PreferLocal  Prefer = "local"
	PreferRemote Prefer = "remote"
)

// Plan decides what to do with every file, sorted by path. Conflicts not
// settled by prefer are returned instead of an action and left alone.
func Plan(local, rem []remote.Entry, last Snapshot, prefer Prefer) ([]Action, []Conflict) {
	byPath := func(entries []remote.Entry) map[string]*remote.Entry {
		m := make(map[string]*remote.Entry, len(entries))
		for i := range entries {
			m[entries[i].Path] = &entries[i]
		}
		return m
	}
	l, r := byPath(local), byPath(rem)

	paths := map[string]bool{}
	for _, m := range []map[string]*remote.Entry{l, r} {
		for p := range m {
			paths[p] = true
		}
	}
	for _, m := range []map[string]State{last.Local, last.Remote} {
		for p := range m {
			paths[p] = true
		}
	}

	var actions []Action
	var conflicts []Conflict
	for p := range paths {
		ls, lknown := last.Local[p]
		rs, rknown := last.Remote[p]
		lc, rc := compare(l[p], ls, lknown), compare(r[p], rs, rknown)
		a := Action{Path: p, Local: l[p], Remote: r[p]}

		quiet := func(c change) bool { return c == absent || c == unchanged }
		switch {
		case quiet(lc) && quiet(rc):
			if lc == unchanged && rc == absent {
				a.Kind = Push // the snapshot lost the remote side; restore it
			} else if lc == absent && rc == unchanged {
				a.Kind = Pull
			} else {
				continue
			}
		case (lc == added || lc == modified) && quiet(rc):
			a.Kind = Push
		case (rc == added || rc == modified) && quiet(lc):
			a.Kind = Pull
		case lc == deleted && (rc == unchanged || rc == absent || rc == deleted):
			a.Kind = DeleteRemote
			if rc != unchanged {
				a.Kind = Forget
			}
		case rc == deleted && (lc == unchanged || lc == absent):
			a.Kind = DeleteLocal
			if lc == absent {
				a.Kind = Forget
			}
		case lc == added && rc == added && l[p].Size == r[p].Size:
			a.Kind = Adopt
		default:
			switch {
			case prefer == PreferLocal && l[p] != nil:
				a.Kind = Push
			case prefer == PreferLocal:
				a.Kind = DeleteRemote
			case prefer == PreferRemote && r[p] != nil:
				a.Kind = Pull
			case prefer == PreferRemote:
				a.Kind = DeleteLocal
			default:
				conflicts = append(conflicts, Conflict{Path: p, Local: lc, Remote: rc})
				continue
			}
		}
		actions = append(actions, a)
	}

	slices.SortFunc(actions, func(a, b Action) int { return cmp.Compare(a.Path, b.Path) })
	slices.SortFunc(conflicts, func(a, b Conflict) int { return cmp.Compare(a.Path, b.Path) })
	return actions, conflicts
}

// CheckDeletes refuses plans that delete many files on one side, which
// usually means a remote that is empty or not mounted rather than a real
// clean-up. total is the size of the larger side.
func CheckDeletes(actions []Action, total int) error {
	counts := map[Kind]int{}
	for _, a := range actions {
		counts[a.Kind]++
	}
	for _, k := range []Kind{DeleteRemote, DeleteLocal} {
		if n := counts[k]; n > 5 && n*10 > total {
			side := "on the remote"
			if k == DeleteLocal {
				side = "locally"
			}
			return fmt.Errorf("would delete %d of %d files %s; pass --force if you mean it", n, total, side)
		}
	}
	return nil
}
