package media

import (
	"context"
	"fmt"
	"io"
	"strconv"
)

// StreamMP4 converts a video to fragmented H.264/AAC MP4 while it is read.
// at is the point to begin, in milliseconds. It never writes to the user's
// file or keeps a second copy on disk. Cancelling ctx stops ffmpeg.
func (d Decoders) StreamMP4(ctx context.Context, path string, at int64, out io.Writer) error {
	if d.FFmpeg == "" {
		return ErrUnsupported
	}
	v, err := d.probe(ctx, path)
	if err != nil {
		return err
	}
	if at < 0 {
		at = 0
	}
	if v.Duration > 0 && at >= v.Duration {
		at = max(0, v.Duration-1000)
	}
	args := []string{"-v", "error", "-nostdin", "-hide_banner", "-ss",
		strconv.FormatFloat(float64(at)/1000, 'f', 3, 64), "-i", path,
		"-map", "0:V:0", "-map", "0:a:0?", "-sn", "-dn",
		"-vf", fitFilter(v, 1920, true),
		"-c:v", "libopenh264", "-b:v", "4M", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "160k",
		"-movflags", "frag_keyframe+empty_moov+default_base_moof",
		"-frag_duration", "1000000", "-f", "mp4", "-"}
	if err := runTool(ctx, d.FFmpeg, out, args...); err != nil {
		return fmt.Errorf("convert video: %w", err)
	}
	return nil
}
