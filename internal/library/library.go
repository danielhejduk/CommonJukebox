package library

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Scan returns the paths of all .flac, .ogg and .opus files under root.
func Scan(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && IsAudio(path) {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

// IsAudio reports whether path has a supported audio extension, in any case.
func IsAudio(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".flac", ".ogg", ".opus":
		return true
	}
	return false
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
