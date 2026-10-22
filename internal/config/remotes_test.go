package config

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemotes(t *testing.T) {
	root := t.TempDir()
	writeSettings(t, root, "codec = opus\n")

	if err := SetRemote(root, "phone", Remote{URL: "ftps://me@10.0.0.2:2121/Music", Insecure: true}); err != nil {
		t.Fatal(err)
	}
	if err := SetRemote(root, "nas", Remote{URL: "ftp://nas/music"}); err != nil {
		t.Fatal(err)
	}
	got, err := LoadRemotes(root)
	if err != nil {
		t.Fatal(err)
	}
	if got["phone"] != (Remote{URL: "ftps://me@10.0.0.2:2121/Music", Insecure: true}) || got["nas"] != (Remote{URL: "ftp://nas/music"}) || len(got) != 2 {
		t.Fatalf("LoadRemotes = %+v", got)
	}

	// Replacing a remote drops the insecure flag it no longer has.
	if err := SetRemote(root, "phone", Remote{URL: "ftps://me@10.0.0.2:2121/Music"}); err != nil {
		t.Fatal(err)
	}
	if err := RemoveRemote(root, "nas"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(SettingsPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if want := "codec = opus\nremote.phone.url = ftps://me@10.0.0.2:2121/Music\n"; string(data) != want {
		t.Errorf("file = %q, want %q", data, want)
	}
	if s, err := LoadSettings(root); err != nil || s.Codec != "opus" {
		t.Errorf("LoadSettings = %+v, %v", s, err)
	}
	if err := RemoveRemote(root, "nas"); err == nil {
		t.Error("removing a missing remote succeeded")
	}
}

func TestSetRemoteErrors(t *testing.T) {
	tests := []struct {
		name, remote, url, wantErr string
	}{
		{name: "bad name", remote: "a/b", url: "ftp://h/", wantErr: "invalid remote name"},
		{name: "bad scheme", remote: "x", url: "smb://h/", wantErr: "unsupported scheme"},
		{name: "no host", remote: "x", url: "ftp:///music", wantErr: "missing host"},
		{name: "password", remote: "x", url: "ftp://me:pw@h/", wantErr: "don't put the password"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := SetRemote(t.TempDir(), tt.remote, Remote{URL: tt.url})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestPassword(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	netrc := "default login me password fallback\nmachine nas login other password no\nmachine nas\n  login me\n  password fromnetrc\n"
	if err := os.WriteFile(filepath.Join(home, ".netrc"), []byte(netrc), 0o600); err != nil {
		t.Fatal(err)
	}
	ask := func(string) (string, error) { return "typed", nil }
	tests := []struct {
		name, url, env string
		prompt         func(string) (string, error)
		want           string
		wantErr        bool
	}{
		{name: "anonymous", url: "ftp://nas/", want: ""},
		{name: "netrc", url: "ftp://me@nas/", want: "fromnetrc"},
		{name: "netrc default", url: "ftp://me@phone/", want: "fallback"},
		{name: "env first", url: "ftp://me@nas/", env: "fromenv", want: "fromenv"},
		{name: "prompt", url: "ftp://you@phone/", prompt: ask, want: "typed"},
		{name: "nothing", url: "ftp://you@phone/", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env != "" {
				t.Setenv("JUKEBOX_PASSWORD", tt.env)
			} else {
				os.Unsetenv("JUKEBOX_PASSWORD")
			}
			u, _ := url.Parse(tt.url)
			got, err := Password(u, tt.prompt)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Errorf("Password = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}
