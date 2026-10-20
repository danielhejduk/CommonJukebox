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

func TestSetKey(t *testing.T) {
	tests := []struct {
		name    string
		file    string // empty means no config file
		force   bool
		want    string
		wantErr string
	}{
		{name: "no file", want: "library = /music\n"},
		{name: "keeps other lines", file: "# music\nfoo = bar\n", want: "# music\nfoo = bar\nlibrary = /music\n"},
		{name: "no trailing newline", file: "foo = bar", want: "foo = bar\nlibrary = /music\n"},
		{name: "already set", file: "library = /old\n", wantErr: "library already set to /old"},
		{name: "same value is not a conflict", file: "library = /music\n", want: "library = /music\n"},
		{name: "force replaces in place", file: "# music\nlibrary = /old\nfoo = bar\n", force: true, want: "# music\nlibrary = /music\nfoo = bar\n"},
		{name: "commented library ignored", file: "# library = /old\n", want: "# library = /old\nlibrary = /music\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "commonjukebox", "config")
			if tt.file != "" {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(tt.file), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			err := setKey(path, "library", "/music", tt.force)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("file = %q, want %q", got, tt.want)
			}
			if cfg, err := loadFrom(path, ""); err != nil || cfg.Library != "/music" {
				t.Errorf("loadFrom = %q, %v; want /music", cfg.Library, err)
			}
		})
	}
}
