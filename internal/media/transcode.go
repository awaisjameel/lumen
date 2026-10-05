package media

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"lumen/internal/core"
)

// ConvertMP4 prepares converting a video to fragmented H.264/AAC MP4 while
// it is read, beginning at ms: it checks the video can be converted (errors
// come back before anything is written) and returns the conversion to run.
// Running it never writes to the user's file or keeps a second copy on disk;
// cancelling ctx stops ffmpeg.
func (d Decoders) ConvertMP4(ctx context.Context, path string, at int64) (run func(out io.Writer) error, err error) {
	return d.ConvertMP4Track(ctx, path, at, -1)
}

// ConvertMP4Track prepares playback with an explicitly chosen audio stream.
func (d Decoders) ConvertMP4Track(ctx context.Context, path string, at int64, audio int) (run func(out io.Writer) error, err error) {
	if d.FFmpeg == "" || d.FFprobe == "" {
		return nil, ErrUnsupported
	}
	v, err := d.probe(ctx, path)
	if err != nil {
		return nil, err
	}
	audioMap := "0:a:0?"
	if audio >= 0 {
		tracks, err := d.Inspect(ctx, path, core.KindVideo)
		if err != nil {
			return nil, err
		}
		found := false
		for _, track := range tracks.Tracks {
			if track.Index == audio && track.Kind == "audio" {
				found = true
			}
		}
		if !found {
			return nil, ErrUnsupported
		}
		audioMap = "0:" + strconv.Itoa(audio)
	}
	at = max(0, at)
	if v.Duration > 0 && at >= v.Duration {
		at = max(0, v.Duration-1000)
	}
	args := ffmpegArgs("-ss", ssTime(at), "-i", path,
		"-map", "0:V:0", "-map", audioMap, "-sn", "-dn",
		"-vf", fitFilter(v, 1920, true),
		"-c:v", "libopenh264", "-b:v", "4M", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "160k",
		"-movflags", "frag_keyframe+empty_moov+default_base_moof",
		"-frag_duration", "1000000", "-f", "mp4", "-")
	return func(out io.Writer) error {
		// Someone is watching: this runs at normal priority, with all the
		// threads FFmpeg wants, unlike the index's decoding.
		if err := runToolAs(ctx, foreground, d.FFmpeg, out, args...); err != nil {
			return fmt.Errorf("convert video: %w", err)
		}
		return nil
	}, nil
}
