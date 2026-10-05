package media

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	goexif "github.com/rwcarlsen/goexif/exif"

	"lumen/internal/core"
	"lumen/internal/media/vips"
)

// Inspect reads optional metadata and the streams present in a media file.
func (d Decoders) Inspect(ctx context.Context, path string, kind core.Kind) (core.MediaInspection, error) {
	info := core.MediaInspection{Tracks: []core.MediaTrack{}}
	if kind == core.KindVideo {
		p, err := d.probeTracks(ctx, path)
		if err != nil {
			return info, err
		}
		for _, s := range p.Streams {
			if s.CodecType == "video" && info.FrameRate == "" {
				info.FrameRate = s.FrameRate
			}
			if s.CodecType != "audio" && s.CodecType != "subtitle" {
				continue
			}
			supported := s.CodecType == "audio" || textSubtitles[s.CodecName]
			info.Tracks = append(info.Tracks, core.MediaTrack{Index: s.Index, Kind: s.CodecType, Codec: s.CodecName, Language: s.Tags["language"], Title: s.Tags["title"], Supported: supported})
		}
		return info, nil
	}
	m, err := ReadMetadata(path, kind)
	if err != nil {
		return info, err
	}
	info.Orientation = m.Orientation
	if pages, err := vips.Pages(path); err == nil {
		info.Pages = pages
	}
	if profile, err := vips.Profile(path); err == nil && len(profile) > 0 {
		info.Profile = "Embedded ICC"
	}
	f, err := os.Open(path)
	if err != nil {
		return info, err
	}
	defer f.Close()
	x, err := goexif.Decode(f)
	if err == nil {
		for name, to := range map[goexif.FieldName]*string{goexif.LensModel: &info.Lens, goexif.ExposureTime: &info.Exposure, goexif.FNumber: &info.Aperture, goexif.ISOSpeedRatings: &info.ISO} {
			if tag, err := x.Get(name); err == nil {
				*to = strings.Trim(tag.String(), "\"")
			}
		}
	}
	return info, nil
}

var textSubtitles = map[string]bool{"subrip": true, "ass": true, "ssa": true, "webvtt": true, "mov_text": true, "text": true}

func (d Decoders) probeTracks(ctx context.Context, path string) (ffprobeOutput, error) {
	var p ffprobeOutput
	if d.FFprobe == "" {
		return p, ErrUnsupported
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var out cappedBuffer
	if err := runTool(ctx, d.FFprobe, &out, "-v", "error", "-print_format", "json", "-show_streams", path); err != nil {
		return p, err
	}
	err := json.Unmarshal(out.Bytes(), &p)
	return p, err
}

// Subtitles converts an admitted text stream to local WebVTT.
func (d Decoders) Subtitles(ctx context.Context, path string, index int, at int64, out io.Writer) error {
	info, err := d.Inspect(ctx, path, core.KindVideo)
	if err != nil {
		return err
	}
	allowed := false
	for _, track := range info.Tracks {
		if track.Index == index && track.Kind == "subtitle" && track.Supported {
			allowed = true
		}
	}
	if !allowed || d.FFmpeg == "" {
		return ErrUnsupported
	}
	return runTool(ctx, d.FFmpeg, out, ffmpegArgs("-ss", ssTime(at), "-i", path, "-map", "0:"+strconv.Itoa(index), "-f", "webvtt", "-")...)
}
