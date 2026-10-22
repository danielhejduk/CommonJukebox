package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/danielhejduk/commonjukebox/internal/config"
	"github.com/danielhejduk/commonjukebox/internal/remote"
	jsync "github.com/danielhejduk/commonjukebox/internal/sync"
)

func runRemote(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "add":
			return runRemoteAdd(args[1:])
		case "list":
			return runRemoteList(args[1:])
		case "remove":
			return runRemoteRemove(args[1:])
		}
	}
	return errors.New("usage: jukebox remote add [--insecure] <name> <url> | jukebox remote list | jukebox remote remove <name>")
}

func runRemoteAdd(args []string) error {
	fs, lib := newFlagSet("remote add")
	insecure := fs.Bool("insecure", false, "don't verify the server's TLS certificate or SSH host key")
	cfg, err := parseWithConfig(fs, lib, args)
	if err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return errors.New("usage: jukebox remote add [--library path] [--insecure] <name> <url>")
	}
	if err := config.SetRemote(cfg.Library, fs.Arg(0), config.Remote{URL: fs.Arg(1), Insecure: *insecure}); err != nil {
		return err
	}
	fmt.Printf("remote %s set to %s in %s\n", fs.Arg(0), fs.Arg(1), config.SettingsPath(cfg.Library))
	return nil
}

func runRemoteList(args []string) error {
	fs, lib := newFlagSet("remote list")
	cfg, err := parseWithConfig(fs, lib, args)
	if err != nil {
		return err
	}
	remotes, err := config.LoadRemotes(cfg.Library)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(remotes))
	for name := range remotes {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		r := remotes[name]
		extra := ""
		if r.Insecure {
			extra = " (insecure)"
		}
		fmt.Printf("%s\t%s%s\n", name, r.URL, extra)
	}
	return nil
}

func runRemoteRemove(args []string) error {
	fs, lib := newFlagSet("remote remove")
	cfg, err := parseWithConfig(fs, lib, args)
	if err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: jukebox remote remove [--library path] <name>")
	}
	name := fs.Arg(0)
	if err := config.RemoveRemote(cfg.Library, name); err != nil {
		return err
	}
	// Without its snapshot, syncing a remote of the same name later starts afresh.
	if err := os.Remove(jsync.SnapshotPath(cfg.Library, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	fmt.Println("removed remote", name)
	return nil
}

// runSync makes the library and a remote alike: changes on either side are
// copied to the other, and files changed on both are reported and skipped.
func runSync(args []string) error {
	fs, lib := newFlagSet("sync")
	dryRun := fs.Bool("dry-run", false, "print what would be done without changing anything")
	prefer := fs.String("prefer", "", "settle conflicts in favour of local or remote")
	force := fs.Bool("force", false, "allow deleting many files at once")
	cfg, err := parseWithConfig(fs, lib, args)
	if err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: jukebox sync [--library path] [--dry-run] [--prefer local|remote] [--force] <remote>")
	}
	if *prefer != "" && *prefer != "local" && *prefer != "remote" {
		return fmt.Errorf("--prefer must be local or remote, not %q", *prefer)
	}

	name := fs.Arg(0)
	remotes, err := config.LoadRemotes(cfg.Library)
	if err != nil {
		return err
	}
	rc, ok := remotes[name]
	if !ok {
		return fmt.Errorf("no remote named %q; add one with jukebox remote add", name)
	}
	u, err := url.Parse(rc.URL)
	if err != nil {
		return err
	}
	opts := remote.Options{Insecure: rc.Insecure}
	if u.Scheme == "sftp" {
		// SSH keys usually do; only look for a password if the server asks.
		opts.AskPassword = func() (string, error) { return config.Password(u, passwordPrompter()) }
	} else if opts.Password, err = config.Password(u, passwordPrompter()); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "connecting to %s\n", u.Redacted())
	rem, err := remote.Open(rc.URL, opts)
	if err != nil {
		return err
	}
	defer rem.Close()
	local := remote.NewLocal(cfg.Library)

	localFiles, err := local.List()
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "listing %s (%d local tracks)\n", name, len(localFiles))
	remoteFiles, err := rem.List()
	if err != nil {
		return err
	}
	snap, err := jsync.LoadSnapshot(cfg.Library, name)
	if err != nil {
		return err
	}

	actions, conflicts := jsync.Plan(localFiles, remoteFiles, snap, jsync.Prefer(*prefer))
	var errs []error
	for _, c := range conflicts {
		fmt.Fprintln(os.Stderr, "conflict:", c)
	}
	if len(conflicts) > 0 {
		errs = append(errs, fmt.Errorf("%d conflicts skipped; pass --prefer local or --prefer remote to settle them", len(conflicts)))
	}

	if err := jsync.CheckDeletes(actions, max(len(localFiles), len(remoteFiles))); err != nil && !*force {
		return joinErrors(append(errs, err))
	}

	if *dryRun {
		for _, a := range actions {
			if a.Transfers() {
				fmt.Println(a.Kind, a.Path)
			}
		}
		return joinErrors(errs)
	}

	total := 0
	for _, a := range actions {
		if a.Transfers() {
			total++
		}
	}
	started := 0
	execErrs := jsync.Execute(cfg.Library, name, local, rem, actions, snap, func(a jsync.Action) {
		if a.Transfers() {
			started++
			fmt.Printf("[%d/%d] %s %s\n", started, total, a.Kind, a.Path)
		}
	})
	errs = append(errs, execErrs...)
	fmt.Printf("synced %s: %d changes, %d failed, %d conflicts\n", name, total, len(execErrs), len(conflicts))
	return joinErrors(errs)
}

// passwordPrompter asks on the terminal without echoing, or returns nil when
// stdin isn't a terminal.
func passwordPrompter() func(string) (string, error) {
	info, err := os.Stdin.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return nil
	}
	return func(question string) (string, error) {
		fmt.Fprintf(os.Stderr, "%s: ", question)
		if stty("-echo") == nil {
			defer stty("echo")
		}
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		fmt.Fprintln(os.Stderr)
		if errors.Is(err, io.EOF) && line == "" {
			return "", errors.New("no password given: set JUKEBOX_PASSWORD or add it to ~/.netrc")
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
}

func stty(arg string) error {
	cmd := exec.Command("stty", arg)
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
