package transcode

import "testing"

// A trimmed ffmpeg -encoders listing from a build without libvorbis, like
// Homebrew's ffmpeg formula.
const listing = `Encoders:
 V..... = Video
 A..... = Audio
 ...X.. = Codec is experimental
 ------
 A....D flac                 FLAC (Free Lossless Audio Codec)
 A..X.D opus                 Opus
 A....D libopus              libopus Opus (codec opus)
 A..X.D vorbis               Vorbis
`

func TestHasEncoder(t *testing.T) {
	for name, want := range map[string]bool{
		"flac":      true,
		"libopus":   true,
		"vorbis":    true,
		"libvorbis": false, // only named in other entries' descriptions, if at all
		"Opus":      false, // a description, not a name
		"=":         false, // legend lines aren't encoders
	} {
		if got := hasEncoder([]byte(listing), name); got != want {
			t.Errorf("hasEncoder(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestEncoderFor(t *testing.T) {
	for _, tt := range []struct {
		codec string
		args  []string
		want  string
	}{
		{"vorbis", nil, "libvorbis"},
		{"vorbis", []string{"-q:a", "4"}, "libvorbis"},
		{"vorbis", []string{"-c:a", "vorbis", "-strict", "-2"}, "vorbis"},
		{"opus", []string{"-acodec", "opus", "-codec:a", "libopus"}, "libopus"}, // the last one wins
		{"opus", []string{"-b:a", "96k", "-c:a"}, "libopus"},                    // no value to take
	} {
		got, err := encoderFor(tt.codec, tt.args)
		if err != nil || got != tt.want {
			t.Errorf("encoderFor(%q, %q) = %q, %v, want %q", tt.codec, tt.args, got, err, tt.want)
		}
	}
	if _, err := encoderFor("mp3", nil); err == nil {
		t.Error("encoderFor accepted an unsupported codec")
	}
}
