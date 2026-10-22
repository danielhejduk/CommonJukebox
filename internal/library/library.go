package library

import (
	"io/fs"
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
