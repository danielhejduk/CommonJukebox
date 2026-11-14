package config

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Settings are stored inside the library, so they travel with it.
type Settings struct {
	Codec string // codec new tracks are stored in: flac, opus or vorbis

	// FFmpegArgs holds encoder options set as ffmpeg.<codec> = <args>,
	// replacing the built-in ones for that codec. Codecs without an entry
	// use the built-in options.
	FFmpegArgs map[string][]string
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
			return
		}
		codec, ok := strings.CutPrefix(key, "ffmpeg.")
		if !ok {
			return
		}
		// An empty value goes back to the built-in options.
		args := strings.Fields(value)
		if len(args) == 0 {
			delete(s.FFmpegArgs, codec)
			return
		}
		if s.FFmpegArgs == nil {
			s.FFmpegArgs = map[string][]string{}
		}
		s.FFmpegArgs[codec] = args
	})
	if err != nil {
		return Settings{}, err
	}
	if err := checkCodec(s.Codec); err != nil {
		return Settings{}, fmt.Errorf("%s: %w", path, err)
	}
	// A misspelt codec would otherwise be ignored without a word.
	for _, codec := range slices.Sorted(maps.Keys(s.FFmpegArgs)) {
		if err := checkCodec(codec); err != nil {
			return Settings{}, fmt.Errorf("%s: ffmpeg.%s: %w", path, codec, err)
		}
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
