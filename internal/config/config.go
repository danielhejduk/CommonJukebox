package config

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Library string
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
		if err := parse(f, path, &cfg); err != nil {
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
	return cfg, nil
}

func parse(f *os.File, path string, cfg *Config) error {
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("%s:%d: expected key = value", path, n)
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		switch key {
		case "library":
			cfg.Library = value
		default:
			// Unknown keys are ignored so older binaries accept newer configs.
		}
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
