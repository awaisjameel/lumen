// Spike: make 256 + 1024 px thumbnails for ~1,000 local images with libvips,
// and HEIC via WIC and FFmpeg. (It also timed the Phase 1 pure-Go
// thumbnailer, since replaced by libvips; its numbers are in README.md.)
package main

import (
	"bytes"
	"flag"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/image/draw"

	"lumen/internal/core"
	"lumen/spikes/corpus"
	"lumen/spikes/vips"
	"lumen/spikes/wic"
)

func main() {
	n := flag.Int("n", 1000, "images")
	workers := flag.Int("w", max(1, runtime.NumCPU()-1), "parallel workers")
	out := flag.String("out", "out/thumbs", "where to write sample thumbnails")
	flag.Parse()
	os.MkdirAll(*out, 0o755)

	paths := corpus.Images(*n)
	fmt.Printf("corpus: %d images, %.0f MB (warm cache), %d workers, %d CPUs\n",
		len(paths), float64(corpus.Warm(paths))/1e6, *workers, runtime.NumCPU())
	byExt := map[string]int{}
	for _, p := range paths {
		byExt[corpus.Ext(p)]++
	}
	fmt.Println("formats:", byExt)

	if err := vips.Init(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	vips.SetConcurrency(1) // parallelism comes from our workers
	fmt.Println("libvips", vips.Version())

	run("libvips", paths, *workers, func(p string) error {
		big, small, _, _, err := vips.Thumbs(p, core.ThumbLarge, core.ThumbSmall)
		if err == nil && strings.HasSuffix(p, "0.JPG") { // keep a few samples to eyeball
			base := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
			os.WriteFile(filepath.Join(*out, base+"-1024.jpg"), big, 0o644)
			os.WriteFile(filepath.Join(*out, base+"-256.jpg"), small, 0o644)
		}
		return err
	})

	heic := corpus.Images(100, ".heic", ".heif")
	if len(heic) > 0 {
		fmt.Printf("\nHEIC (%d files), one at a time:\n", len(heic))
		heicRun("WIC (Windows codec)", heic, func(p string) error {
			img, _, _, err := wic.Decode(p, core.ThumbLarge)
			if err != nil {
				return err
			}
			small := image256(img)
			var b bytes.Buffer
			if err := jpeg.Encode(&b, small, &jpeg.Options{Quality: 80}); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(*out, "wic-"+filepath.Base(p)+".jpg"), b.Bytes(), 0o644)
		})
		heicRun("FFmpeg process", heic, func(p string) error {
			dst := filepath.Join(*out, "ff-"+filepath.Base(p)+".jpg")
			return ffmpegThumb(p, dst, core.ThumbLarge)
		})
		heicRun("libvips", heic, func(p string) error {
			_, _, _, _, err := vips.Thumbs(p, core.ThumbLarge, core.ThumbSmall)
			return err
		})
	}
}

func run(name string, paths []string, workers int, fn func(string) error) {
	var mu sync.Mutex
	perExt := map[string][]time.Duration{}
	errs := map[string]string{}
	var done atomic.Int64
	ch := make(chan string)
	var wg sync.WaitGroup
	var ms runtime.MemStats
	start := time.Now()
	for range workers {
		wg.Go(func() {
			for p := range ch {
				t := time.Now()
				err := fn(p)
				d := time.Since(t)
				mu.Lock()
				if err != nil {
					errs[corpus.Ext(p)] = err.Error()
				} else {
					perExt[corpus.Ext(p)] = append(perExt[corpus.Ext(p)], d)
					done.Add(1)
				}
				mu.Unlock()
			}
		})
	}
	for _, p := range paths {
		ch <- p
	}
	close(ch)
	wg.Wait()
	wall := time.Since(start)
	runtime.ReadMemStats(&ms)
	fmt.Printf("\n%s: %d/%d ok in %.1fs = %.0f images/s (Go heap sys %d MB)\n",
		name, done.Load(), len(paths), wall.Seconds(), float64(done.Load())/wall.Seconds(), ms.Sys>>20)
	exts := make([]string, 0, len(perExt))
	for e := range perExt {
		exts = append(exts, e)
	}
	sort.Strings(exts)
	for _, e := range exts {
		fmt.Printf("  %-6s n=%-4d median %4d ms  p95 %4d ms (per image, per worker)\n", e, len(perExt[e]), pct(perExt[e], 50), pct(perExt[e], 95))
	}
	for e, msg := range errs {
		fmt.Printf("  %-6s failed, e.g. %s\n", e, trim(msg))
	}
}

func heicRun(name string, paths []string, fn func(string) error) {
	var ds []time.Duration
	var fail string
	for _, p := range paths {
		t := time.Now()
		if err := fn(p); err != nil {
			fail = err.Error()
			continue
		}
		ds = append(ds, time.Since(t))
	}
	fmt.Printf("  %-20s ok %d/%d  median %4d ms  max %4d ms", name, len(ds), len(paths), pct(ds, 50), pct(ds, 100))
	if fail != "" {
		fmt.Printf("  (e.g. %s)", trim(fail))
	}
	fmt.Println()
}

// ffmpegThumb handles both plain and tiled (iPhone) HEIC: tiled files are a
// stream group that FFmpeg assembles itself.
func ffmpegThumb(src, dst string, size int) error {
	scale := fmt.Sprintf("scale=w=%d:h=%d:force_original_aspect_ratio=decrease", size, size)
	probe, _ := exec.Command("ffprobe", "-v", "error", "-show_entries", "stream_group=type", "-of", "csv=p=0", src).Output()
	args := []string{"-v", "error", "-y", "-i", src}
	if strings.Contains(string(probe), "Tile Grid") {
		args = append(args, "-filter_complex", "[0:g:0]"+scale+"[o]", "-map", "[o]")
	} else {
		args = append(args, "-vf", scale)
	}
	args = append(args, "-frames:v", "1", "-update", "1", "-q:v", "3", dst)
	if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("%v: %s", err, out)
	}
	return nil
}

func image256(src *image.RGBA) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w >= h {
		h, w = max(1, h*core.ThumbSmall/w), core.ThumbSmall
	} else {
		w, h = max(1, w*core.ThumbSmall/h), core.ThumbSmall
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	return dst
}

func pct(ds []time.Duration, p int) int64 {
	if len(ds) == 0 {
		return 0
	}
	s := append([]time.Duration(nil), ds...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	i := min(len(s)-1, (len(s)*p)/100)
	return s[i].Milliseconds()
}

func trim(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	if len(s) > 140 {
		s = s[:140] + "…"
	}
	return s
}
