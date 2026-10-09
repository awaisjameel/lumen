package media

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image/png"
	"io"
	"strconv"
	"time"

	"lumen/internal/core"
)

// jxlPNG keeps the original profile and alpha until the common libvips
// conversion. Bounded memory avoids decoded temporary files after a worker crash.
func (d Decoders) jxlPNG(ctx context.Context, path string) ([]byte, error) {
	if d.JXL == "" {
		return nil, ErrUnsupported
	}
	ctx, cancel := context.WithTimeout(ctx, ffmpegTimeout)
	defer cancel()
	var output cappedBuffer
	if err := d.runJXLTool(ctx, "", &output, path, "-", "--output_format=png", "--bits_per_sample=8", "--num_threads="+strconv.Itoa(DecodeThreads), "--quiet"); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func (d Decoders) jxl(ctx context.Context, path string) (source, error) {
	data, err := d.jxlPNG(ctx, path)
	if err != nil {
		return source{}, fmt.Errorf("decode JPEG XL: %w", err)
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return source{}, err
	}
	if config.Height <= 0 || config.Width <= 0 || config.Width > maxPixels/config.Height {
		return source{}, ErrUnsupported
	}
	image, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return source{}, err
	}
	profile, err := pngColourProfile(data)
	if err != nil {
		return source{}, err
	}
	if err := ctx.Err(); err != nil {
		return source{}, err
	}
	pixels := straightPixels(image, 1)
	pixels.ICC = profile
	return source{pixels: pixels}, nil
}

func (d Decoders) jxlEXIF(ctx context.Context, path string) ([]byte, error) {
	if d.JXL == "" {
		return nil, ErrUnsupported
	}
	ctx, cancel := context.WithTimeout(ctx, ffmpegTimeout)
	defer cancel()
	var output cappedBuffer
	if err := d.runJXLTool(ctx, "", &output, path, "-", "--output_format=exif", "--num_threads="+strconv.Itoa(DecodeThreads), "--quiet"); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func (d Decoders) runJXLTool(ctx context.Context, directory string, output io.Writer, args ...string) error {
	start := hideWindow
	if d.foreground {
		start = foreground
	}
	return runContainedToolAs(ctx, start, d.JXL, directory, output, renderMemory, args...)
}

func (d Decoders) jxlMetadata(ctx context.Context, path string) (Metadata, error) {
	m := Metadata{Orientation: 1}
	data, err := d.jxlEXIF(ctx, path)
	if err != nil {
		return m, err
	}
	fillFromImagemeta(bytes.NewReader(data), &m)
	if m.TakenAt.IsZero() && m.Camera == "" {
		fillFromGoexif(bytes.NewReader(data), &m)
	}
	v, err := d.probe(ctx, path)
	if err != nil {
		return m, err
	}
	m.Width, m.Height, m.Orientation = v.Width, v.Height, 1
	if !core.Plausible(m.TakenAt) {
		m.TakenAt = time.Time{}
	}
	if !core.Plausible(m.Modified) {
		m.Modified = time.Time{}
	}
	return m, nil
}

func (d Decoders) jxlInspection(ctx context.Context, path string) (core.MediaInspection, error) {
	info := core.MediaInspection{Tracks: []core.MediaTrack{}, Orientation: 1, Pages: 1}
	data, err := d.jxlEXIF(ctx, path)
	if err != nil {
		return info, err
	}
	fillInspectionEXIF(bytes.NewReader(data), &info)
	decoded, err := d.jxlPNG(ctx, path)
	if err != nil {
		return info, err
	}
	profile, err := pngColourProfile(decoded)
	if len(profile) > 0 {
		info.HasProfile, info.Profile = true, profileName(profile)
	}
	return info, err
}

// pngColourProfile reads the profile carried by the trusted decoder's PNG.
// Its compressed profile has a separate bound from the pixel/output budgets.
func pngColourProfile(data []byte) ([]byte, error) {
	if len(data) < 8 || !bytes.Equal(data[:8], []byte("\x89PNG\r\n\x1a\n")) {
		return nil, ErrUnsupported
	}
	for at := 8; at+12 <= len(data); {
		n := int(binary.BigEndian.Uint32(data[at : at+4]))
		if n > len(data)-at-12 {
			return nil, ErrUnsupported
		}
		if string(data[at+4:at+8]) == "iCCP" {
			payload := data[at+8 : at+8+n]
			end := bytes.IndexByte(payload, 0)
			if end < 1 || end+2 > len(payload) || payload[end+1] != 0 {
				return nil, ErrUnsupported
			}
			reader, err := zlib.NewReader(bytes.NewReader(payload[end+2:]))
			if err != nil {
				return nil, err
			}
			const maxProfile = 4 << 20
			profile, err := io.ReadAll(io.LimitReader(reader, maxProfile+1))
			err = errors.Join(err, reader.Close())
			if len(profile) > maxProfile {
				return nil, ErrUnsupported
			}
			return profile, err
		}
		at += n + 12
	}
	return nil, nil
}
