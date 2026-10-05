package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"time"

	"lumen/internal/core"
	"lumen/internal/media/vips"
)

// Render limits bound one output, source admission, process lifetime and
// retained disk space. Very large sources keep their indexed preview until
// a tiled decoder exists; no full-resolution capability is inferred here.
const (
	RenderSide       = 4096
	renderPixels     = 100_000_000
	renderBytes      = 64 << 20
	renderCacheBytes = 256 << 20
	renderVersion    = "srgb-upright-v1"
)

// Renderer supplies a cancellable decode. Production runs it outside the
// engine so a native decoder cannot keep the UI or shutdown waiting.
type Renderer interface {
	Render(ctx context.Context, path string, orientation, side int) ([]byte, error)
}

// ProcessRenderer launches the installed runtime in its private worker mode.
type ProcessRenderer struct{ Executable, FFmpeg string }

type renderInput struct {
	Path, FFmpeg      string
	Orientation, Side int
	Operation         string
}

// Render decodes one image in a cancellable, bounded worker process.
func (p ProcessRenderer) Render(ctx context.Context, path string, orientation, side int) ([]byte, error) {
	return p.worker(ctx, renderInput{Path: path, FFmpeg: p.FFmpeg, Orientation: orientation, Side: side})
}

// Inspect reads image metadata inside the same bounded native worker.
func (p ProcessRenderer) Inspect(ctx context.Context, path string) (core.MediaInspection, error) {
	data, err := p.worker(ctx, renderInput{Path: path, FFmpeg: p.FFmpeg, Operation: "inspect"})
	var info core.MediaInspection
	if err != nil {
		return info, err
	}
	err = json.Unmarshal(data, &info)
	return info, err
}

func (p ProcessRenderer) worker(ctx context.Context, in renderInput) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	input, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, p.Executable, "--render-worker")
	foreground(cmd)
	cmd.Stdin = bytes.NewReader(input)
	var output cappedBuffer
	cmd.Stdout = &output
	// Decoder diagnostics can contain arbitrarily large corrupt metadata.
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	cleanup, err := containRender(cmd)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, err
	}
	defer cleanup()
	err = cmd.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, fmt.Errorf("render image: %w", err)
	}
	return output.Bytes(), nil
}

type cappedBuffer struct{ bytes.Buffer }

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if len(p) > renderBytes-b.Len() {
		return 0, errors.New("render output exceeds budget")
	}
	return b.Buffer.Write(p)
}

// RenderWorker reads only its explicit input. It never opens a library,
// starts Wails, or alters originals. Native pixels use the indexer's existing
// upright sRGB path, so EXIF and colour conversion happen exactly once.
func RenderWorker(ctx context.Context, input io.Reader, output io.Writer) error {
	var in renderInput
	if err := json.NewDecoder(io.LimitReader(input, 64<<10)).Decode(&in); err != nil {
		return err
	}
	if in.Operation == "inspect" {
		info, err := (Decoders{FFmpeg: in.FFmpeg}).Inspect(ctx, in.Path, core.KindPhoto)
		if err != nil {
			return err
		}
		return json.NewEncoder(output).Encode(info)
	}
	if in.Side < 1 || in.Side > RenderSide {
		return ErrUnsupported
	}
	m, err := ReadMetadata(in.Path, core.KindPhoto)
	if err != nil {
		return err
	}
	if m.Width <= 0 || m.Height <= 0 || int64(m.Width) > renderPixels/int64(m.Height) {
		return ErrUnsupported
	}
	d := Decoders{FFmpeg: in.FFmpeg}
	im, err := d.Decode(ctx, in.Path, core.KindPhoto, in.Orientation, in.Side)
	if err != nil {
		return err
	}
	jpeg, err := vips.JPEG(im.Pix, im.W, im.H, 92)
	if err != nil {
		return err
	}
	_, err = output.Write(jpeg)
	return err
}

// RenderFile caches only the current source revision. It serialises workers
// and eviction, and rechecks the source before publishing a finished decode.
func (c *Cache) RenderFile(ctx context.Context, id int64, path string, orientation, side, adjustmentVersion int) (string, core.RenderInfo, error) {
	var info core.RenderInfo
	if c.Renderer == nil {
		return "", info, ErrUnsupported
	}
	side = min(max(side, 256), RenderSide)
	select {
	case c.rendering <- struct{}{}:
		defer func() { <-c.rendering }()
	case <-ctx.Done():
		return "", info, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return "", info, err
	}
	before, err := os.Stat(path)
	if err != nil {
		return "", info, err
	}
	key := sha256.Sum256(fmt.Appendf(nil, "%s:%d:%d:%d:%d:%d:%s", path, before.Size(), before.ModTime().UnixNano(), orientation, side, adjustmentVersion, renderVersion))
	info.Revision = hex.EncodeToString(key[:])
	info.Provenance = "source"
	dir := filepath.Join(c.Dir, "render")
	dst := filepath.Join(dir, fmt.Sprintf("%d-%s.jpg", id, info.Revision))
	jpeg, err := readRender(dst, side)
	if errors.Is(err, os.ErrNotExist) {
		jpeg, err = c.Renderer.Render(ctx, path, orientation, side)
		if err != nil {
			return "", info, err
		}
		if err := validRender(jpeg, side); err != nil {
			return "", info, err
		}
		after, err := os.Stat(path)
		if err != nil {
			return "", info, err
		}
		if after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
			return "", info, errors.New("source changed during render")
		}
		if err := ctx.Err(); err != nil {
			return "", info, err
		}
		if err := c.trimRenders(ctx, dir, int64(len(jpeg))); err != nil {
			return "", info, err
		}
		if err := c.writeAtomic(dst, jpeg); err != nil {
			return "", info, err
		}
	} else if err != nil {
		return "", info, err
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(jpeg))
	if err != nil {
		return "", info, err
	}
	info.Width, info.Height = cfg.Width, cfg.Height
	// Source dimensions come from metadata in the service, outside this cache.
	return dst, info, nil
}

func readRender(path string, side int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, renderBytes+1))
	if err != nil {
		return nil, err
	}
	return data, validRender(data, side)
}

func validRender(data []byte, side int) error {
	if len(data) > renderBytes {
		return ErrUnsupported
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return err
	}
	if format != "jpeg" || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > side || cfg.Height > side {
		return ErrUnsupported
	}
	return nil
}

func (c *Cache) trimRenders(ctx context.Context, dir string, incoming int64) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var total int64
	for _, entry := range entries {
		fi, err := entry.Info()
		if err != nil {
			return err
		}
		total += fi.Size()
	}
	slices.SortFunc(entries, func(a, b os.DirEntry) int {
		ai, ae := a.Info()
		bi, be := b.Info()
		if ae != nil || be != nil {
			return 0
		}
		return ai.ModTime().Compare(bi.ModTime())
	})
	// A small bounded cache evicts the oldest published copy. Originals and the
	// thumbnail cache are outside this directory and cannot be removed here.
	for _, entry := range entries {
		if total+incoming <= renderCacheBytes {
			break
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		fi, err := entry.Info()
		if err != nil {
			return err
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
		total -= fi.Size()
	}
	return nil
}
