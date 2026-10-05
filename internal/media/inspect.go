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
			kind := core.TrackKind(s.CodecType)
			if kind != core.TrackAudio && kind != core.TrackSubtitle {
				continue
			}
			supported := kind == core.TrackAudio || textSubtitles[s.CodecName]
			info.Tracks = append(info.Tracks, core.MediaTrack{Index: s.Index, Kind: kind, Codec: s.CodecName, Language: s.Tags["language"], Title: s.Tags["title"], Supported: supported})
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
		info.HasProfile, info.Profile = true, profileName(profile)
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

// track is the stream of that kind at index, as the file itself declares
// it: a requested index is never passed to ffmpeg unchecked.
func (d Decoders) track(ctx context.Context, path string, index int, kind core.TrackKind) (core.MediaTrack, error) {
	info, err := d.Inspect(ctx, path, core.KindVideo)
	if err != nil {
		return core.MediaTrack{}, err
	}
	for _, t := range info.Tracks {
		if t.Index == index && t.Kind == kind {
			return t, nil
		}
	}
	return core.MediaTrack{}, ErrUnsupported
}

// Subtitles converts an admitted text stream to local WebVTT.
func (d Decoders) Subtitles(ctx context.Context, path string, index int, at int64, out io.Writer) error {
	t, err := d.track(ctx, path, index, core.TrackSubtitle)
	if err != nil {
		return err
	}
	if !t.Supported || d.FFmpeg == "" {
		return ErrUnsupported
	}
	return runTool(ctx, d.FFmpeg, out, ffmpegArgs("-ss", ssTime(at), "-i", path, "-map", "0:"+strconv.Itoa(index), "-f", "webvtt", "-")...)
}
