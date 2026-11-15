package transcode

import (
	"bytes"
	"fmt"
	"os/exec"

	"github.com/danielhejduk/commonjukebox/internal/tags"
)

// encoders maps each codec to its ffmpeg encoder.
var encoders = map[string]string{
	"flac":   "flac",
	"opus":   "libopus",
	"vorbis": "libvorbis",
}

// defaultArgs are the encoder options used when the caller gives none.
var defaultArgs = map[string][]string{
	"flac":   {"-compression_level", "8"},
	"opus":   {"-b:a", "160k", "-vbr", "on"},
	"vorbis": {"-q:a", "6"},
}

// Transcode encodes the audio stream of src into dst using codec,
// preserving metadata. codec must be one of flac, opus or vorbis.
// args replace the codec's default encoder options; if empty, the defaults
// are used. They come after the encoder choice, so they may also override it.
func Transcode(src, dst, codec string, args []string) error {
	encoder, ok := encoders[codec]
	if !ok {
		return fmt.Errorf("unsupported codec %q", codec)
	}
	if len(args) == 0 {
		args = defaultArgs[codec]
	}
	cmd := []string{"-nostdin", "-loglevel", "error", "-y", "-i", src, "-map", "0:a", "-map_metadata", "0", "-map_metadata", "0:s:a:0", "-c:a", encoder}
	cmd = append(append(cmd, args...), dst)
	out, err := exec.Command("ffmpeg", cmd...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: ffmpeg: %v: %s", src, err, bytes.TrimSpace(out))
	}
	if err := tags.CopyComments(src, dst); err != nil {
		return err
	}
	return tags.CopyCover(src, dst)
}

// Check reports why Transcode can't encode to codec with args: ffmpeg is
// missing, or it was built without the encoder they select. Builds differ
// in which external encoders they include; Homebrew's ffmpeg, for one, has
// no libvorbis. Converting many files, call it once up front instead of
// failing on each.
func Check(codec string, args []string) error {
	encoder, err := encoderFor(codec, args)
	if err != nil {
		return err
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("converting to %s needs ffmpeg: %w", codec, err)
	}
	out, err := exec.Command("ffmpeg", "-hide_banner", "-encoders").Output()
	if err != nil {
		return fmt.Errorf("listing ffmpeg encoders: %w", err)
	}
	if !hasEncoder(out, encoder) {
		return fmt.Errorf("converting to %s needs ffmpeg's %s encoder, which this ffmpeg was built without", codec, encoder)
	}
	return nil
}

// encoderFor returns the encoder ffmpeg will use for codec: its default,
// unless args choose another. As in ffmpeg, the last choice wins.
func encoderFor(codec string, args []string) (string, error) {
	encoder, ok := encoders[codec]
	if !ok {
		return "", fmt.Errorf("unsupported codec %q", codec)
	}
	for i := 0; i+1 < len(args); i++ {
		switch args[i] {
		case "-c", "-c:a", "-codec", "-codec:a", "-acodec":
			encoder = args[i+1]
		}
	}
	return encoder, nil
}

// hasEncoder reports whether the output of ffmpeg -encoders lists name.
// The entries follow a legend that ends in a line of dashes; each is a line
// of capability flags, the name and a description.
func hasEncoder(list []byte, name string) bool {
	_, entries, ok := bytes.Cut(list, []byte("------"))
	if !ok {
		return false
	}
	for line := range bytes.Lines(entries) {
		if f := bytes.Fields(line); len(f) >= 2 && string(f[1]) == name {
			return true
		}
	}
	return false
}
