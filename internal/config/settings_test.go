package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSettings(t *testing.T) {
	tests := []struct {
		name    string
		file    string // empty means no settings file
		want    Settings
		wantErr string
	}{
		{name: "no file", want: DefaultSettings()},
		{name: "codec", file: "codec = opus\n", want: Settings{Codec: "opus"}},
		{name: "unknown keys", file: "# x\nfoo = bar\n", want: DefaultSettings()},
		{name: "bad codec", file: "codec = mp3\n", wantErr: `unknown codec "mp3"`},
		{name: "malformed line", file: "codec opus\n", wantErr: ":1: expected key = value"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if tt.file != "" {
				writeSettings(t, root, tt.file)
			}

			got, err := LoadSettings(root)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("LoadSettings = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestInitSettings(t *testing.T) {
	tests := []struct {
		name    string
		file    string // empty means no settings file
		codec   string
		want    string // file contents afterwards
		wantErr string
	}{
		{name: "new with default", want: "codec = flac\n"},
		{name: "new with codec", codec: "opus", want: "codec = opus\n"},
		{name: "existing kept", file: "# mine\ncodec = vorbis\n", want: "# mine\ncodec = vorbis\n"},
		{name: "existing codec replaced", file: "# mine\ncodec = vorbis\n", codec: "opus", want: "# mine\ncodec = opus\n"},
		{name: "bad codec", codec: "mp3", wantErr: `unknown codec "mp3"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if tt.file != "" {
				writeSettings(t, root, tt.file)
			}

			_, err := InitSettings(root, tt.codec)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(SettingsPath(root))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("file = %q, want %q", got, tt.want)
			}
		})
	}
}

func writeSettings(t *testing.T, root, data string) {
	t.Helper()
	path := SettingsPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}
