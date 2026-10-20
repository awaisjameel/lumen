package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"lumen/internal/core"
	"lumen/internal/media"
	"lumen/internal/media/vips"
	"lumen/internal/media/wic"
)

func TestInventory(t *testing.T) {
	inventory()
	fmt.Printf("libvips=%s\n", vips.Version())
}

func TestRender(t *testing.T) {
	path := os.Getenv("PHASE7_FILE")
	if path == "" {
		t.Skip("PHASE7_FILE is required")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	// The test runs in spikes/phase7/probe, three folders below the repository.
	repo, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	inside := func(dir string) bool {
		prefix := strings.ToLower(filepath.ToSlash(filepath.Join(repo, dir))) + "/"
		return strings.HasPrefix(strings.ToLower(filepath.ToSlash(path)), prefix)
	}
	if !inside(filepath.Join("spikes", ".cache")) && !inside(filepath.Join("spikes", "phase7", "fixtures")) {
		t.Fatal("probe input must be in an authorised fixture folder")
	}
	side, err := strconv.Atoi(os.Getenv("PHASE7_SIDE"))
	if err != nil || side <= 0 {
		t.Fatal("positive PHASE7_SIDE is required")
	}
	mode := os.Getenv("PHASE7_MODE")
	if err := vips.Init(); err != nil {
		t.Fatal(err)
	}
	ffmpeg := filepath.Join(repo, "deps", "runtime", "ffmpeg")
	d := media.Decoders{FFmpeg: filepath.Join(ffmpeg, "ffmpeg.exe"), FFprobe: filepath.Join(ffmpeg, "ffprobe.exe")}
	m, err := d.Metadata(context.Background(), path, core.KindPhoto)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	var decodeMS, encodeMS float64
	var w, h, outputBytes int
	var thumbnail []byte
	switch mode {
	case "thumbnail":
		thumbs, e := d.Thumbnail(context.Background(), path, core.KindPhoto, m.Orientation)
		err = e
		outputBytes = len(thumbs.Large) + len(thumbs.Small)
		thumbnail = thumbs.Large
	case "wic":
		var pix []byte
		pix, w, h, err = wic.Decode(path, side)
		outputBytes = len(pix)
	default:
		im, e := d.Decode(context.Background(), path, core.KindPhoto, m.Orientation, side)
		decodeMS = float64(time.Since(start).Microseconds()) / 1000
		err = e
		w, h = im.W, im.H
		if err == nil {
			var jpeg []byte
			encodeStart := time.Now()
			jpeg, err = vips.JPEG(im.Pix, w, h, 85)
			encodeMS = float64(time.Since(encodeStart).Microseconds()) / 1000
			outputBytes = len(jpeg)
		}
	}
	elapsed := time.Since(start)
	if len(thumbnail) > 0 {
		cfg, e := jpeg.DecodeConfig(bytes.NewReader(thumbnail))
		if e != nil {
			t.Fatal(e)
		}
		w, h = cfg.Width, cfg.Height
	}
	ws, commit := peaks()
	ac, battery := power()
	row := map[string]any{"file": filepath.Base(path), "mode": mode, "side": side, "source_w": m.Width, "source_h": m.Height, "orientation": m.Orientation, "output_w": w, "output_h": h, "output_bytes": outputBytes, "ms": float64(elapsed.Microseconds()) / 1000, "peak_ws_bytes": ws, "peak_commit_bytes": commit}
	row["decode_resize_ms"], row["jpeg_encode_ms"] = decodeMS, encodeMS
	row["ac_line_status"], row["battery_percent"] = ac, battery
	row["recorded_at"] = time.Now().Format(time.RFC3339)
	if err != nil {
		row["error"] = err.Error()
	}
	b, e := json.Marshal(row)
	if e != nil {
		t.Fatal(e)
	}
	fmt.Printf("MEASURE %s\n", b)
	if err != nil {
		t.Fatal(err)
	}
}
