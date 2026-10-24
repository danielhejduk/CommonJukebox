package main

import (
	"bufio"
	"cmp"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"

	"github.com/danielhejduk/commonjukebox/internal/add"
	"github.com/danielhejduk/commonjukebox/internal/config"
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
