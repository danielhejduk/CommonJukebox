package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFrom(t *testing.T) {
	t.Setenv("HOME", "/home/test")

	tests := []struct {
		name    string
		file    string // empty means no config file
		flag    string
		want    string
		wantErr string
	}{
		{name: "flag only", flag: "/flag", want: "/flag"},
		{name: "file only", file: "library = /file\n", want: "/file"},
		{name: "flag wins", file: "library = /file\n", flag: "/flag", want: "/flag"},
		{name: "neither", wantErr: "no library set"},
		{name: "empty file", file: "\n", wantErr: "no library set"},
		{name: "comments and unknown keys", file: "# music\n\nfoo = bar\nlibrary=/file\n", want: "/file"},
		{name: "malformed line", file: "library /file\n", wantErr: ":1: expected key = value"},
		{name: "tilde", file: "library = ~/Music\n", want: "/home/test/Music"},
		{name: "tilde in flag", flag: "~/Music", want: "/home/test/Music"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config")
			if tt.file != "" {
				if err := os.WriteFile(path, []byte(tt.file), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			cfg, err := loadFrom(path, tt.flag)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Library != tt.want {
				t.Errorf("Library = %q, want %q", cfg.Library, tt.want)
			}
		})
	}
}
