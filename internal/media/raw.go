package media

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"lumen/internal/core"
	"lumen/internal/media/vips"
)

// Camera RAW files are read by lumen-raw.exe (native/lumen-raw), a worker
// that links LibRaw and runs once per file: a malformed or enormous RAW
// then costs one short-lived process with a memory budget, not the engine.
//
// The developed sensor data is the photo: thumbnails, the perceptual hash,
// the models' pixels and the viewer's render all come from the same
// development (the policy is written in the worker), so a face found in one
// lands on the same spot of the others. It is a neutral development, not the
// camera's own JPEG look. Only when LibRaw cannot read a file's sensor data
// does the largest JPEG the camera embedded stand in for it, and Image says so.

const (
	// rawTimeout bounds one run of the worker. The slowest sample measured
	// (a 26-megapixel X-Trans file at full size) took 7 s on mains power.
	rawTimeout = time.Minute
	// rawMemory is the worker's job limit. Developing the largest picture it
	// is asked for at full size (a long edge just under twice RenderSide)
	// commits about 13 bytes a pixel: 650 MB for 50 megapixels.
	rawMemory = 1 << 30
	// rawPixels is the most pixels a development may send back.
	rawPixels = 2 * RenderSide * 2 * RenderSide
	// rawPreviewBytes is the largest embedded preview read.
	rawPreviewBytes = 64 << 20
	// rawUnreadable is the worker's exit code for a file, or a part of one,
	// that LibRaw cannot read.
	rawUnreadable = 3
)

// errRawUnreadable is ErrUnsupported for a RAW file LibRaw cannot read: a
// camera or a compression it does not know.
var errRawUnreadable = fmt.Errorf("camera RAW: %w", ErrUnsupported)

// rawInfo is what the worker's info says a file holds.
type rawInfo struct {
	Make, Model   string
	Width, Height int // of the developed picture, upright
	DNG           bool
	Taken         string // the camera's wall clock, 2006-01-02T15:04:05; "" = none
	ISO           float64
	Shutter       float64 // seconds
	Aperture      float64
	Focal         float64 // mm
	Lens          string
	GPS           bool
	Lat, Lon      float64
}

// raw runs the worker and hands read the first line of its answer and the
// bytes after it. The worker is killed when ctx ends or read fails.
func (d Decoders) raw(ctx context.Context, read func(head []byte, body io.Reader) error, args ...string) error {
	if d.Raw == "" {
		return ErrUnsupported
	}
	run, cancel := context.WithTimeout(ctx, rawTimeout)
	defer cancel()
	cmd := exec.CommandContext(run, d.Raw, args...)
	if d.foreground {
		foreground(cmd)
	} else {
		hideWindow(cmd)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr tail
	cmd.Stderr = &stderr
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start the RAW worker: %w", err)
	}
	release, err := contain(cmd, rawMemory)
	if err != nil {
		cancel()
		_ = cmd.Wait() // the failure to contain it is the one to report
		return err
	}
	defer release()
	r := bufio.NewReaderSize(stdout, 64<<10)
	head, rerr := r.ReadSlice('\n')
	if rerr == nil {
		rerr = read(head, r)
	}
	if rerr != nil {
		cancel() // it may still be writing to a pipe nobody reads
	}
	werr := cmd.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	var exit *exec.ExitError
	switch {
	case errors.As(werr, &exit) && exit.ExitCode() == rawUnreadable:
		return fmt.Errorf("%w: %s", errRawUnreadable, stderr.String())
	case rerr != nil && errors.Is(run.Err(), context.DeadlineExceeded):
		return errors.New("the RAW worker took too long")
	case rerr != nil && werr != nil:
		return fmt.Errorf("RAW worker: %w: %s", werr, stderr.String())
	case rerr != nil:
		return fmt.Errorf("read the RAW worker's answer: %w", rerr)
	}
	return werr
}

// tail keeps the start of what a tool wrote to standard error: enough to
// say why it failed, however much a corrupt file makes it print.
type tail struct{ b []byte }

func (t *tail) Write(p []byte) (int, error) {
	if room := 512 - len(t.b); room > 0 {
		t.b = append(t.b, p[:min(room, len(p))]...)
	}
	return len(p), nil
}

func (t *tail) String() string { return firstLine(string(t.b)) }

// rawInfo asks the worker what a RAW file says about itself.
func (d Decoders) rawInfo(ctx context.Context, path string) (rawInfo, error) {
	var info rawInfo
	err := d.raw(ctx, func(head []byte, _ io.Reader) error { return json.Unmarshal(head, &info) }, "info", path)
	return info, err
}

// rawMetadata is Metadata for a RAW file. The developed picture is upright
// already, so its orientation is 1 and its size is as displayed.
func (d Decoders) rawMetadata(ctx context.Context, path string) (Metadata, error) {
	m := Metadata{Orientation: 1}
	info, err := d.rawInfo(ctx, path)
	if err != nil {
		return m, err
	}
	m.Width, m.Height = info.Width, info.Height
	m.Camera = cameraName(strings.TrimSpace(info.Make), strings.TrimSpace(info.Model))
	if t, err := time.Parse("2006-01-02T15:04:05", info.Taken); err == nil && core.Plausible(t) {
		m.TakenAt = t // wall-clock time, as ReadMetadata returns it
	}
	if info.GPS && validGPS(info.Lat, info.Lon) {
		m.Lat, m.Lon = &info.Lat, &info.Lon
	}
	return m, nil
}

// rawInspection is Inspect for a RAW file.
func (d Decoders) rawInspection(ctx context.Context, path string) (core.MediaInspection, error) {
	out := core.MediaInspection{Tracks: []core.MediaTrack{}, Orientation: 1}
	info, err := d.rawInfo(ctx, path)
	if err != nil {
		return out, err
	}
	out.Lens = strings.TrimSpace(info.Lens)
	if info.Shutter > 0 {
		out.Exposure = exposureTime(info.Shutter)
	}
	if info.Aperture > 0 {
		out.Aperture = strconv.FormatFloat(info.Aperture, 'f', -1, 32)
	}
	if info.ISO > 0 {
		out.ISO = strconv.FormatFloat(info.ISO, 'f', -1, 32)
	}
	return out, nil
}

// exposureTime writes seconds the way EXIF does, as a fraction: "1/250",
// "2/1" for two seconds, "13/10" for 1.3.
func exposureTime(seconds float64) string {
	if seconds < 0.3 {
		return "1/" + strconv.Itoa(int(1/seconds+0.5))
	}
	if tenths := int(seconds*10 + 0.5); tenths%10 != 0 {
		return strconv.Itoa(tenths) + "/10"
	}
	return strconv.Itoa(int(seconds+0.5)) + "/1"
}

// rawSource develops a RAW file for libvips, halved by the worker while its
// long edge stays at least maxSide. A file whose sensor data LibRaw cannot
// read gives its embedded preview instead.
func (d Decoders) rawSource(ctx context.Context, path string, maxSide int) (source, error) {
	var src source
	err := d.raw(ctx, func(head []byte, body io.Reader) error {
		var h struct{ Width, Height int }
		if err := json.Unmarshal(head, &h); err != nil {
			return err
		}
		if h.Width < 1 || h.Height < 1 || h.Width > rawPixels/h.Height {
			return fmt.Errorf("developed size %dx%d", h.Width, h.Height)
		}
		pix := make([]byte, 3*h.Width*h.Height)
		if _, err := io.ReadFull(body, pix); err != nil {
			return err
		}
		src.pixels = vips.RGBPixels(pix, h.Width, h.Height)
		return nil
	}, "develop", path, strconv.Itoa(maxSide))
	if !errors.Is(err, errRawUnreadable) {
		return src, err
	}
	preview, perr := d.rawPreview(ctx, path)
	if perr != nil {
		return source{}, err // the development's failure says more
	}
	return preview, nil
}

// rawPreview is the largest JPEG the camera embedded in a RAW file.
func (d Decoders) rawPreview(ctx context.Context, path string) (source, error) {
	src := source{preview: true}
	err := d.raw(ctx, func(head []byte, body io.Reader) error {
		var h struct{ Orientation int }
		if err := json.Unmarshal(head, &h); err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(body, rawPreviewBytes))
		if err != nil {
			return err
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return err
		}
		if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > maxPixels/cfg.Height {
			return fmt.Errorf("embedded preview size %dx%d", cfg.Width, cfg.Height)
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return err
		}
		src.pixels = straightPixels(img, h.Orientation)
		return nil
	}, "preview", path)
	return src, err
}
