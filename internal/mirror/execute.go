package sync

import (
	"fmt"
	"io"

	"github.com/danielhejduk/commonjukebox/internal/remote"
)

// saveEvery is how many actions run between snapshot saves, so an
// interrupted sync redoes little when run again.
const saveEvery = 20

// Execute runs the actions, updating snap after each one that succeeds and
// saving it under root for the named remote. start, if non-nil, is called
// before each action, so a long transfer can be shown as it happens. It keeps
// going after errors and returns all of them.
func Execute(root, name string, local, rem remote.Remote, actions []Action, snap Snapshot, start func(Action)) []error {
	var errs []error
	for i, a := range actions {
		if start != nil {
			start(a)
		}
		if err := run(local, rem, a, snap); err != nil {
			errs = append(errs, fmt.Errorf("%s %s: %w", a.Kind, a.Path, err))
		}
		if (i+1)%saveEvery == 0 {
			if err := snap.Save(root, name); err != nil {
				return append(errs, err)
			}
		}
	}
	if err := snap.Save(root, name); err != nil {
		errs = append(errs, err)
	}
	return errs
}

func run(local, rem remote.Remote, a Action, snap Snapshot) error {
	switch a.Kind {
	case Push:
		e, err := copyFile(local, rem, *a.Local)
		if err != nil {
			return err
		}
		snap.Local[a.Path], snap.Remote[a.Path] = stateOf(*a.Local), stateOf(e)
	case Pull:
		e, err := copyFile(rem, local, *a.Remote)
		if err != nil {
			return err
		}
		snap.Local[a.Path], snap.Remote[a.Path] = stateOf(e), stateOf(*a.Remote)
	case DeleteRemote, DeleteLocal:
		side := rem
		if a.Kind == DeleteLocal {
			side = local
		}
		if err := side.Remove(a.Path); err != nil {
			return err
		}
		forget(snap, a.Path)
	case Adopt:
		snap.Local[a.Path], snap.Remote[a.Path] = stateOf(*a.Local), stateOf(*a.Remote)
	case Forget:
		forget(snap, a.Path)
	}
	return nil
}

func forget(snap Snapshot, p string) {
	delete(snap.Local, p)
	delete(snap.Remote, p)
}

// copyFile streams src's file to dst and returns it as dst now reports it.
func copyFile(src, dst remote.Remote, e remote.Entry) (remote.Entry, error) {
	pr, pw := io.Pipe()
	go func() {
		pw.CloseWithError(src.Get(e.Path, pw))
	}()
	out, err := dst.Put(e.Path, pr, e.ModTime)
	pr.CloseWithError(err) // unblocks the reader if Put gave up early
	return out, err
}
