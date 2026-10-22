package sync

import (
	"strings"
	"testing"
	"time"

	"github.com/danielhejduk/commonjukebox/internal/remote"
)

var t0 = time.Unix(1_700_000_000, 0)

func entry(size int64, mtime int) *remote.Entry {
	return &remote.Entry{Path: "a.flac", Size: size, ModTime: t0.Add(time.Duration(mtime) * time.Second)}
}

func TestPlan(t *testing.T) {
	same := entry(10, 0)
	tests := []struct {
		name          string
		local, remote *remote.Entry // current state; nil means missing
		lastL, lastR  *remote.Entry // snapshot; nil means not recorded
		prefer        Prefer
		want          string // action kind, "conflict" or "" for nothing
	}{
		{name: "in sync", local: same, remote: same, lastL: same, lastR: same, want: ""},
		{name: "new local", local: same, want: "push"},
		{name: "new remote", remote: same, want: "pull"},
		{name: "changed local", local: entry(11, 1), remote: same, lastL: same, lastR: same, want: "push"},
		{name: "touched local", local: entry(10, 5), remote: same, lastL: same, lastR: same, want: "push"},
		{name: "changed remote", local: same, remote: entry(11, 1), lastL: same, lastR: same, want: "pull"},
		{name: "deleted local", remote: same, lastL: same, lastR: same, want: "delete remote"},
		{name: "deleted remote", local: same, lastL: same, lastR: same, want: "delete local"},
		{name: "deleted both", lastL: same, lastR: same, want: "forget"},
		{name: "new both alike", local: same, remote: entry(10, 7), want: "adopt"},
		{name: "new both differ", local: same, remote: entry(12, 0), want: "conflict"},
		{name: "changed both", local: entry(11, 1), remote: entry(12, 1), lastL: same, lastR: same, want: "conflict"},
		{name: "changed local deleted remote", local: entry(11, 1), lastL: same, lastR: same, want: "conflict"},
		{name: "deleted local changed remote", remote: entry(11, 1), lastL: same, lastR: same, want: "conflict"},
		{name: "prefer local", local: entry(11, 1), remote: entry(12, 1), lastL: same, lastR: same, prefer: PreferLocal, want: "push"},
		{name: "prefer remote", local: entry(11, 1), remote: entry(12, 1), lastL: same, lastR: same, prefer: PreferRemote, want: "pull"},
		{name: "prefer local deletion", remote: entry(11, 1), lastL: same, lastR: same, prefer: PreferLocal, want: "delete remote"},
		{name: "prefer remote deletion", local: entry(11, 1), lastL: same, lastR: same, prefer: PreferRemote, want: "delete local"},
		{name: "remote record lost", local: same, lastL: same, want: "push"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var local, rem []remote.Entry
			if tt.local != nil {
				local = append(local, *tt.local)
			}
			if tt.remote != nil {
				rem = append(rem, *tt.remote)
			}
			snap := NewSnapshot()
			if tt.lastL != nil {
				snap.Local["a.flac"] = stateOf(*tt.lastL)
			}
			if tt.lastR != nil {
				snap.Remote["a.flac"] = stateOf(*tt.lastR)
			}

			actions, conflicts := Plan(local, rem, snap, tt.prefer)
			var got string
			switch {
			case len(conflicts) > 0:
				got = "conflict"
			case len(actions) > 0:
				got = actions[0].Kind.String()
			}
			if len(actions)+len(conflicts) > 1 {
				t.Fatalf("got %d actions and %d conflicts, want at most one", len(actions), len(conflicts))
			}
			if got != tt.want {
				t.Errorf("Plan = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPlanFirstSyncNeverDeletes(t *testing.T) {
	local := []remote.Entry{{Path: "a.flac", Size: 1}, {Path: "b.flac", Size: 2}}
	rem := []remote.Entry{{Path: "b.flac", Size: 2}, {Path: "c.flac", Size: 3}}
	actions, conflicts := Plan(local, rem, NewSnapshot(), PreferNone)
	if len(conflicts) != 0 {
		t.Fatalf("conflicts = %v", conflicts)
	}
	var got []string
	for _, a := range actions {
		got = append(got, a.Kind.String()+" "+a.Path)
	}
	want := "push a.flac,adopt b.flac,pull c.flac"
	if strings.Join(got, ",") != want {
		t.Errorf("actions = %v, want %s", got, want)
	}
}

func TestCheckDeletes(t *testing.T) {
	deletes := func(n int, k Kind) []Action {
		var as []Action
		for range n {
			as = append(as, Action{Kind: k})
		}
		return as
	}
	tests := []struct {
		name    string
		actions []Action
		total   int
		wantErr bool
	}{
		{name: "few", actions: deletes(5, DeleteRemote), total: 10},
		{name: "small share", actions: deletes(9, DeleteRemote), total: 100},
		{name: "many remote", actions: deletes(11, DeleteRemote), total: 100, wantErr: true},
		{name: "many local", actions: deletes(6, DeleteLocal), total: 6, wantErr: true},
		{name: "pushes don't count", actions: deletes(50, Push), total: 50},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := CheckDeletes(tt.actions, tt.total); (err != nil) != tt.wantErr {
				t.Errorf("CheckDeletes = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
