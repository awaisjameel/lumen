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
	RenderSide       = 4096 // the longest edge a render may have
	RenderSideMin    = 256  // and the shortest one worth asking for
	renderPixels     = 100_000_000
	renderBytes      = 64 << 20
	renderCacheBytes = 256 << 20
	renderVersion    = "srgb-upright-v1"
	// renderMemory is the render worker's job limit. It covers the worker, a
	// camera RAW it has developed (rawMemory) and the pixels it then holds.
	renderMemory = 1536 << 20
	// renderTimeout bounds one render: a RAW developed at full size and then
	// encoded, on battery.
	renderTimeout = 45 * time.Second
)

// CanRender reports whether known source dimensions fit the worker's budget.
func CanRender(width, height int) bool {
	return width > 0 && height > 0 && int64(width) <= renderPixels/int64(height)
}

// Renderer supplies a cancellable decode. Production runs it outside the
// engine so a native decoder cannot keep the UI or shutdown waiting.
type Renderer interface {
	Render(ctx context.Context, path string, orientation, side int) ([]byte, error)
}

// PageRenderer is a Renderer that can also render a page after a file's
// first (see Decoders.DecodePage).
type PageRenderer interface {
	RenderPage(ctx context.Context, path string, orientation, side, page int) ([]byte, error)
}

// ProcessRenderer launches the installed runtime in its private worker mode.
type ProcessRenderer struct{ Executable, FFmpeg, FFprobe, Raw string }

type renderInput struct {
	Path, FFmpeg, FFprobe, Raw string
	Orientation, Side, Page    int
	Operation                  string
}

// Render decodes one image in a cancellable, bounded worker process.
func (p ProcessRenderer) Render(ctx context.Context, path string, orientation, side int) ([]byte, error) {
	return p.RenderPage(ctx, path, orientation, side, 0)
}

// RenderPage is Render for one page of a file, counted from 0.
func (p ProcessRenderer) RenderPage(ctx context.Context, path string, orientation, side, page int) ([]byte, error) {
	return p.worker(ctx, renderInput{Path: path, FFmpeg: p.FFmpeg, FFprobe: p.FFprobe, Raw: p.Raw, Orientation: orientation, Side: side, Page: page})
}

// Inspect reads image metadata inside the same bounded native worker.
func (p ProcessRenderer) Inspect(ctx context.Context, path string) (core.MediaInspection, error) {
	data, err := p.worker(ctx, renderInput{Path: path, FFmpeg: p.FFmpeg, FFprobe: p.FFprobe, Raw: p.Raw, Operation: "inspect"})
	var info core.MediaInspection
	if err != nil {
		return info, err
	}
	err = json.Unmarshal(data, &info)
	return info, err
}

func (p ProcessRenderer) worker(ctx context.Context, in renderInput) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, renderTimeout)
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
	cleanup, err := contain(cmd, renderMemory)
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
	d := Decoders{FFmpeg: in.FFmpeg, FFprobe: in.FFprobe, Raw: in.Raw, foreground: true}
	if in.Operation == "inspect" {
		info, err := d.Inspect(ctx, in.Path, core.KindPhoto)
		if err != nil {
			return err
		}
		return json.NewEncoder(output).Encode(info)
	}
	if in.Side < 1 || in.Side > RenderSide {
		return ErrUnsupported
	}
	m, err := d.Metadata(ctx, in.Path, core.KindPhoto)
	if err != nil {
		return err
	}
	if !CanRender(m.Width, m.Height) {
		return ErrUnsupported
	}
	im, err := d.DecodePage(ctx, in.Path, core.KindPhoto, in.Orientation, in.Side, in.Page)
	if err != nil {
		return err
	}
	jpeg, err := vips.JPEG(im.Pix, im.W, im.H, 92)
	if err != nil {
		return err
	}
	if im.Preview {
		jpeg = markPreview(jpeg)
	}
	_, err = output.Write(jpeg)
	return err
}

// previewMark is the JPEG comment a render carries when it shows a RAW
// file's embedded preview. It travels with the cached file, so a render read
// back from the cache still says what it is.
const previewMark = "lumen:embedded-preview"

// markPreview adds previewMark to a JPEG, straight after its first marker.
func markPreview(jpeg []byte) []byte {
	if len(jpeg) < 2 {
		return jpeg
	}
	size := len(previewMark) + 2
	out := append(make([]byte, 0, len(jpeg)+size+2), jpeg[:2]...)
	out = append(out, 0xff, 0xfe, byte(size>>8), byte(size))
	out = append(out, previewMark...)
	return append(out, jpeg[2:]...)
}

// markedPreview reports whether a render carries previewMark.
func markedPreview(jpeg []byte) bool {
	const head = 2 + 4 // the first marker, then the comment's marker and length
	return len(jpeg) >= head+len(previewMark) && jpeg[2] == 0xff && jpeg[3] == 0xfe &&
		string(jpeg[head:head+len(previewMark)]) == previewMark
}

// Inspect uses the same admission slot as image rendering. Metadata requests
// must not multiply the native worker's memory budget while a render runs.
func (c *Cache) Inspect(ctx context.Context, path string, kind core.Kind) (core.MediaInspection, error) {
	if kind == core.KindPhoto {
		if inspector, ok := c.Renderer.(interface {
			Inspect(context.Context, string) (core.MediaInspection, error)
		}); ok {
			select {
			case c.rendering <- struct{}{}:
				defer func() { <-c.rendering }()
			case <-ctx.Done():
				return core.MediaInspection{}, ctx.Err()
			}
			if err := ctx.Err(); err != nil {
				return core.MediaInspection{}, err
			}
			return inspector.Inspect(ctx, path)
		}
	}
	if d, ok := c.Decoder.(Inspector); ok {
		return d.Inspect(ctx, path, kind)
	}
	return core.MediaInspection{}, ErrUnsupported
}

// Subtitles writes a video's text subtitle stream as WebVTT, from at ms on.
func (c *Cache) Subtitles(ctx context.Context, path string, index int, at int64, out io.Writer) error {
	d, ok := c.Decoder.(Subtitler)
	if !ok {
		return ErrUnsupported
	}
	return d.Subtitles(ctx, path, index, at, out)
}

// RenderFile caches only the current source revision. It serialises workers
// and eviction, and rechecks the source before publishing a finished decode.
func (c *Cache) RenderFile(ctx context.Context, id int64, path string, orientation, side int) (string, core.RenderInfo, error) {
	return c.RenderPage(ctx, id, path, orientation, side, 0)
}

// RenderPage is RenderFile for one page of a file, counted from 0.
func (c *Cache) RenderPage(ctx context.Context, id int64, path string, orientation, side, page int) (string, core.RenderInfo, error) {
	info := core.RenderInfo{Page: page}
	if c.Renderer == nil {
		return "", info, ErrUnsupported
	}
	render := func(ctx context.Context) ([]byte, error) { return c.Renderer.Render(ctx, path, orientation, side) }
	if page != 0 {
		pages, ok := c.Renderer.(PageRenderer)
		if !ok || page < 0 {
			return "", info, ErrUnsupported
		}
		render = func(ctx context.Context) ([]byte, error) { return pages.RenderPage(ctx, path, orientation, side, page) }
	}
	side = min(max(side, RenderSideMin), RenderSide)
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
	// Display rotation and flip are applied by the viewer and by export, so
	// they are not part of the key: changing them must not decode again.
	key := sha256.Sum256(fmt.Appendf(nil, "%s:%d:%d:%d:%d:%s", path, before.Size(), before.ModTime().UnixNano(), orientation, side, renderVersion))
	if page != 0 { // the first page keeps the key it always had
		key = sha256.Sum256(fmt.Appendf(nil, "%x:page-%d", key, page))
	}
	info.Revision = hex.EncodeToString(key[:])
	dir := filepath.Join(c.Dir, "render")
	dst := filepath.Join(dir, fmt.Sprintf("%d-%s.jpg", id, info.Revision))
	jpeg, err := readRender(dst, side)
	if errors.Is(err, os.ErrNotExist) {
		jpeg, err = render(ctx)
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
	info.Preview = markedPreview(jpeg)
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

// RenderBytes is how much disk the viewing copies take now. They are
// disposable: each is made again from its original when next asked for.
func (c *Cache) RenderBytes() (int64, error) {
	entries, err := os.ReadDir(filepath.Join(c.Dir, "render"))
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var total int64
	for _, entry := range entries {
		if fi, err := entry.Info(); err == nil { // one removed meanwhile counts for nothing
			total += fi.Size()
		}
	}
	return total, nil
}

// ClearRenders deletes every viewing copy and returns the bytes freed. It
// takes the renderer's turn, so no copy is being written meanwhile. Originals,
// thumbnails and the library are outside this folder and are not touched.
func (c *Cache) ClearRenders(ctx context.Context) (int64, error) {
	select {
	case c.rendering <- struct{}{}:
		defer func() { <-c.rendering }()
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	dir := filepath.Join(c.Dir, "render")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var freed int64
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return freed, err
		}
		fi, err := entry.Info()
		if err != nil || entry.IsDir() {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
			return freed, err
		}
		freed += fi.Size()
	}
	return freed, nil
}
