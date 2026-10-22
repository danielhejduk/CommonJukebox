package config

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Remote is a sync target stored in the library's settings file as
// remote.<name>.url and remote.<name>.insecure.
type Remote struct {
	URL      string
	Insecure bool // skip TLS certificate or SSH host key verification
}

// RemoteSchemes lists the URL schemes a remote may use.
var RemoteSchemes = []string{"ftp", "ftps", "sftp", "file"}

// The name is part of a file name under .jukebox/sync, so keep it plain.
var remoteName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// LoadRemotes reads the remotes from the library's settings file.
func LoadRemotes(root string) (map[string]Remote, error) {
	remotes := map[string]Remote{}
	path := SettingsPath(root)
	f, err := os.Open(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return remotes, nil
	case err != nil:
		return nil, err
	}
	defer f.Close()

	err = parse(f, path, func(key, value string) {
		name, field, ok := remoteKey(key)
		if !ok {
			return
		}
		r := remotes[name]
		switch field {
		case "url":
			r.URL = value
		case "insecure":
			r.Insecure = value == "true"
		default:
			return
		}
		remotes[name] = r
	})
	if err != nil {
		return nil, err
	}
	for name, r := range remotes {
		if err := checkRemoteURL(r.URL); err != nil {
			return nil, fmt.Errorf("%s: remote %s: %w", path, name, err)
		}
	}
	return remotes, nil
}

// remoteKey splits remote.<name>.<field>.
func remoteKey(key string) (name, field string, ok bool) {
	rest, ok := strings.CutPrefix(key, "remote.")
	if !ok {
		return "", "", false
	}
	i := strings.LastIndex(rest, ".")
	if i < 0 {
		return "", "", false
	}
	return rest[:i], rest[i+1:], true
}

// SetRemote adds or replaces the named remote.
func SetRemote(root, name string, r Remote) error {
	if !remoteName.MatchString(name) {
		return fmt.Errorf("invalid remote name %q (use letters, digits, - and _)", name)
	}
	if err := checkRemoteURL(r.URL); err != nil {
		return err
	}
	u, _ := url.Parse(r.URL)
	if _, ok := u.User.Password(); ok {
		return errors.New("the settings file travels with the library, so don't put the password in the URL; use JUKEBOX_PASSWORD or ~/.netrc")
	}

	path := SettingsPath(root)
	if err := setKey(path, "remote."+name+".url", r.URL, true); err != nil {
		return err
	}
	if r.Insecure {
		return setKey(path, "remote."+name+".insecure", "true", true)
	}
	return removeKeys(path, func(key string) bool { return key == "remote."+name+".insecure" })
}

// RemoveRemote deletes the named remote from the settings file.
func RemoveRemote(root, name string) error {
	remotes, err := LoadRemotes(root)
	if err != nil {
		return err
	}
	if _, ok := remotes[name]; !ok {
		return fmt.Errorf("no remote named %q", name)
	}
	return removeKeys(SettingsPath(root), func(key string) bool {
		n, _, ok := remoteKey(key)
		return ok && n == name
	})
}

func checkRemoteURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if !slices.Contains(RemoteSchemes, u.Scheme) {
		return fmt.Errorf("%q: unsupported scheme (want %s)", raw, strings.Join(RemoteSchemes, ", "))
	}
	if u.Scheme != "file" && u.Hostname() == "" {
		return fmt.Errorf("%q: missing host", raw)
	}
	return nil
}

// removeKeys deletes the lines of the file at path whose key matches.
func removeKeys(path string, match func(key string) bool) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var kept []string
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		k, _, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && !strings.HasPrefix(k, "#") && match(strings.TrimSpace(k)) {
			continue
		}
		kept = append(kept, line)
	}
	out := strings.Join(kept, "\n")
	if out != "" {
		out += "\n"
	}
	return writeFileAtomic(path, []byte(out))
}

// Password finds the password for u: the JUKEBOX_PASSWORD variable, then a
// matching ~/.netrc entry, then prompt (if non-nil). A URL without a user
// logs in anonymously and needs none.
func Password(u *url.URL, prompt func(question string) (string, error)) (string, error) {
	if u.User == nil {
		return "", nil
	}
	if p, ok := os.LookupEnv("JUKEBOX_PASSWORD"); ok {
		return p, nil
	}
	if home, err := os.UserHomeDir(); err == nil {
		p, ok, err := netrcPassword(filepath.Join(home, ".netrc"), u.Hostname(), u.User.Username())
		if err != nil {
			return "", err
		}
		if ok {
			return p, nil
		}
	}
	if prompt == nil {
		return "", fmt.Errorf("no password for %s@%s: set JUKEBOX_PASSWORD or add it to ~/.netrc", u.User.Username(), u.Hostname())
	}
	return prompt(fmt.Sprintf("Password for %s@%s", u.User.Username(), u.Hostname()))
}

// netrcPassword returns the password of the first entry in the netrc file at
// path for host and login, falling back to a "default" entry.
func netrcPassword(path, host, login string) (string, bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	defer f.Close()

	type entry struct{ machine, login, password string }
	var entries []entry
	var cur *entry
	sc := bufio.NewScanner(f)
	sc.Split(bufio.ScanWords)
	for sc.Scan() {
		switch tok := sc.Text(); tok {
		case "machine", "default":
			entries = append(entries, entry{})
			cur = &entries[len(entries)-1]
			if tok == "machine" && sc.Scan() {
				cur.machine = sc.Text()
			}
		case "login", "password":
			if cur == nil || !sc.Scan() {
				continue
			}
			if tok == "login" {
				cur.login = sc.Text()
			} else {
				cur.password = sc.Text()
			}
		}
	}
	if err := sc.Err(); err != nil {
		return "", false, err
	}
	for _, machine := range []string{host, ""} {
		for _, e := range entries {
			if e.machine == machine && (e.login == "" || e.login == login) && e.password != "" {
				return e.password, true, nil
			}
		}
	}
	return "", false, nil
}
