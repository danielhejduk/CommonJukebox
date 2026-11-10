package library

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Scan returns the paths of all .flac, .ogg and .opus files under root.
func Scan(root string) ([]string, error) {
	return scan(root, IsAudio)
}

// ScanSources returns the paths of all files under root that can be added to
// a library: those Scan returns, plus .wav files, which are converted on the
// way in and so never found in a library.
func ScanSources(root string) ([]string, error) {
	return scan(root, func(path string) bool {
		return IsAudio(path) || !hidden(path) && strings.EqualFold(filepath.Ext(path), ".wav")
	})
}

func scan(root string, match func(path string) bool) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && match(path) {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

// IsAudio reports whether path has a supported audio extension, in any case.
func IsAudio(path string) bool {
	if hidden(path) {
		return false
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".flac", ".ogg", ".opus":
		return true
	}
	return false
}

// hidden reports whether path is a hidden file, such as the "._name" metadata
// files MacOS leaves next to the real ones.
func hidden(path string) bool {
	return strings.HasPrefix(filepath.Base(path), ".")
}

// CoverName returns the name of an album's cover file holding an image with
// file extension ext, such as ".jpg".
func CoverName(ext string) string {
	return "cover" + ext
}

// Covers returns the cover files in dir: cover.jpg, cover.jpeg or cover.png,
// in any case.
func Covers(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var covers []string
	for _, e := range entries {
		name := strings.ToLower(e.Name())
		switch name {
		case "cover.jpg", "cover.jpeg", "cover.png":
			if e.Type().IsRegular() {
				covers = append(covers, filepath.Join(dir, e.Name()))
			}
		}
	}
	return covers, nil
}
