package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Settings are stored inside the library, so they travel with it.
type Settings struct {
	Codec string // codec new tracks are stored in: flac, opus or vorbis
}

// Codecs lists the valid values of Settings.Codec.
var Codecs = []string{"flac", "opus", "vorbis"}

// DefaultSettings are used for anything a library's settings file doesn't set.
func DefaultSettings() Settings {
	return Settings{Codec: "flac"}
}

// SettingsPath returns the settings file of the library at root.
func SettingsPath(root string) string {
	return filepath.Join(root, ".jukebox", "config")
}

// LoadSettings reads the library's settings file. A missing file gives the defaults.
func LoadSettings(root string) (Settings, error) {
	s := DefaultSettings()
	path := SettingsPath(root)

	f, err := os.Open(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return s, nil
	case err != nil:
		return Settings{}, err
	}
	defer f.Close()

	err = parse(f, path, func(key, value string) {
		if key == "codec" {
			s.Codec = value
		}
	})
	if err != nil {
		return Settings{}, err
	}
	if err := checkCodec(s.Codec); err != nil {
		return Settings{}, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

// InitSettings creates the library's settings file if it is missing and sets
// the codec if it is non-empty. It returns the resulting settings.
func InitSettings(root, codec string) (Settings, error) {
	if codec != "" {
		if err := checkCodec(codec); err != nil {
			return Settings{}, err
		}
	}

	path := SettingsPath(root)
	_, err := os.Stat(path)
	missing := errors.Is(err, fs.ErrNotExist)
	if err != nil && !missing {
		return Settings{}, err
	}

	if missing && codec == "" {
		codec = DefaultSettings().Codec
	}
	if codec != "" {
		if err := setKey(path, "codec", codec, true); err != nil {
			return Settings{}, err
		}
	}
	return LoadSettings(root)
}

func checkCodec(codec string) error {
	if !slices.Contains(Codecs, codec) {
		return fmt.Errorf("unknown codec %q (want %s)", codec, strings.Join(Codecs, ", "))
	}
	return nil
}
