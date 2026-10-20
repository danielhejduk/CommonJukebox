package config

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Library  string
	Settings Settings // from the library's own settings file
}

// Path returns the config file location, e.g. ~/.config/commonjukebox/config.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "commonjukebox", "config"), nil
}

// Load reads the config file (if present) and applies the override (if non-empty).
func Load(libraryFlag string) (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
	return loadFrom(path, libraryFlag)
}

func loadFrom(path, libraryFlag string) (Config, error) {
	var cfg Config

	f, err := os.Open(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// No config file is fine; the flag may be enough.
	case err != nil:
		return Config{}, err
	default:
		defer f.Close()
		err := parse(f, path, func(key, value string) {
			if key == "library" {
				cfg.Library = value
			}
		})
		if err != nil {
			return Config{}, err
		}
	}

	if libraryFlag != "" {
		cfg.Library = libraryFlag
	}
	if cfg.Library == "" {
		return Config{}, fmt.Errorf("no library set: pass --library or set \"library\" in %s", path)
	}

	lib, err := expandHome(cfg.Library)
	if err != nil {
		return Config{}, err
	}
	cfg.Library = lib

	cfg.Settings, err = LoadSettings(lib)
	if err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// parse calls set for every key = value line in r. Unknown keys are left to
// set to ignore, so older binaries accept newer configs.
func parse(r io.Reader, path string, set func(key, value string)) error {
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("%s:%d: expected key = value", path, n)
		}
		set(strings.TrimSpace(key), strings.TrimSpace(value))
	}
	return sc.Err()
}

func expandHome(p string) (string, error) {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, p[1:]), nil
}

// AbsPath expands a leading ~ and makes p absolute, so the stored library
// doesn't depend on the directory jukebox is later run from.
func AbsPath(p string) (string, error) {
	p, err := expandHome(p)
	if err != nil {
		return "", err
	}
	return filepath.Abs(p)
}

// SetLibrary writes lib to the config file and returns the file's path.
// Other lines are kept. An existing library is only replaced if force is set.
func SetLibrary(lib string, force bool) (string, error) {
	path, err := Path()
	if err != nil {
		return "", err
	}
	return path, setKey(path, "library", lib, force)
}

// setKey sets key to value in the file at path, creating it if needed and
// keeping other lines. A different existing value is only replaced if force is set.
func setKey(path, key, value string, force bool) error {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	var lines []string
	if len(data) > 0 {
		lines = strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	}

	// Replace every line for key; parse would use the last one.
	newLine := key + " = " + value
	replaced := false
	for i, line := range lines {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || strings.HasPrefix(k, "#") || strings.TrimSpace(k) != key {
			continue
		}
		if v = strings.TrimSpace(v); v != value && !force {
			return fmt.Errorf("%s already set to %s in %s (use --force to replace it)", key, v, path)
		}
		lines[i] = newLine
		replaced = true
	}
	if !replaced {
		lines = append(lines, newLine)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(path, []byte(strings.Join(lines, "\n")+"\n"))
}

// writeFileAtomic writes data to a temporary file and renames it over path,
// so a failed write never leaves a truncated config behind.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op once renamed

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
