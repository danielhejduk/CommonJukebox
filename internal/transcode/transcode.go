package transcode

import (
	"bytes"
	"fmt"
	"os/exec"

	"github.com/danielhejduk/commonjukebox/internal/tags"
)

var ffmpegArgs = map[string][]string{
	"flac":   {"-c:a", "flac", "-compression_level", "8"},
	"opus":   {"-c:a", "libopus", "-b:a", "160k", "-vbr", "on"},
	"vorbis": {"-c:a", "libvorbis", "-q:a", "6"},
}

// Transcode encodes the audio stream of src into dst using codec,
// preserving metadata. codec must be one of flac, opus or vorbis.
func Transcode(src, dst, codec string) error {
	codecArgs, ok := ffmpegArgs[codec]
	if !ok {
		return fmt.Errorf("unsupported codec %q", codec)
	}
	args := []string{"-nostdin", "-loglevel", "error", "-y", "-i", src, "-map", "0:a", "-map_metadata", "0", "-map_metadata", "0:s:a:0"}
	args = append(append(args, codecArgs...), dst)
	out, err := exec.Command("ffmpeg", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: ffmpeg: %v: %s", src, err, bytes.TrimSpace(out))
	}
	return tags.CopyCover(src, dst)
}
