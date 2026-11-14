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
	if codec == "opus" || codec == "vorbis" {}
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
