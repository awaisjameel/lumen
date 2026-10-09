package compress

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"lumen/internal/core"
	"lumen/internal/fileops"
	"lumen/internal/media"
)

const (
	jpegTool = "libjpeg-turbo 3.1.2"
	pngTool  = "OxiPNG 9.1.5"
	jxlTool  = "libjxl 0.12.0"
)

// Codec uses only pinned, bundled tools. It never reads a viewer render.
type Codec struct{ Runtime string }

func (c Codec) tool(ctx context.Context, name string) (string, error) {
	return c.file(ctx, name+".exe")
}

func (c Codec) file(ctx context.Context, name string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	path := filepath.Join(c.Runtime, "compress", name)
	info, err := os.Stat(path)
	if c.Runtime == "" || err != nil || !info.Mode().IsRegular() {
		return "", ErrUnsupported
	}
	return path, nil
}

func (c Codec) run(ctx context.Context, name, dir string, args ...string) error {
	exe, err := c.tool(ctx, name)
	if err != nil {
		return err
	}
	return media.RunTool(ctx, exe, dir, args...)
}

// Available reports whether every tool the offered modes need is bundled.
func (c Codec) Available(ctx context.Context) bool {
	for _, name := range []string{"jpegtran.exe", "djpeg.exe", "oxipng.exe", "cjxl.exe", "djxl.exe", "libjpeg-62.dll"} {
		if _, err := c.file(ctx, name); err != nil {
			return false
		}
	}
	return true
}

// Encode keeps the smallest candidate that passes independent checks, within
// the preset's bounded search. notify checkpoints the verification phase.
func (c Codec) Encode(ctx context.Context, source, dir string, options core.CompressionOptions, notify func() error) (path, verification, tool string, err error) {
	limit := 90 * time.Second
	if options.Effort == core.CompressMaximum {
		limit = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	family := core.FamilyOf(source)
	threads := "--num_threads=" + strconv.Itoa(options.Threads)
	switch options.Mode {
	case core.CompressReversible:
		if family != core.FamilyJPEG {
			return "", "", "", ErrUnsupported
		}
		if _, err := jpegMetadata(ctx, source); err != nil {
			return "", "", "", err
		}
		path = filepath.Join(dir, "candidate.jxl")
		err = c.run(ctx, "cjxl", dir, source, path, "--distance=0", "--lossless_jpeg=1", "--allow_jpeg_reconstruction=1", "--container=1", "--effort="+jxlEffort(options.Effort), threads)
		if err != nil {
			return "", "", "", err
		}
		if err := notify(); err != nil {
			return "", "", "", err
		}
		restored := filepath.Join(dir, "roundtrip.jpg")
		if err := c.run(ctx, "djxl", dir, path, restored, "--reconstruct_jpeg", threads); err != nil {
			return "", "", "", err
		}
		a, size, err := fileops.FileDigest(ctx, source)
		if err != nil {
			return "", "", "", err
		}
		b, outputSize, err := fileops.FileDigest(ctx, restored)
		if err != nil {
			return "", "", "", err
		}
		if a != b || size != outputSize {
			return "", "", "", ErrPreservation
		}
		return path, "jpeg-full-sha256", jxlTool, nil
	case core.CompressConvert:
		if family != core.FamilyPNG {
			return "", "", "", ErrUnsupported
		}
		if _, err := readPNG(ctx, source, true); err != nil {
			return "", "", "", err
		}
		path = filepath.Join(dir, "candidate.jxl")
		err = c.run(ctx, "cjxl", dir, source, path, "--distance=0", "--alpha_distance=0", "--keep_invisible=1", "--premultiply=0", "--container=1", "--effort="+jxlEffort(options.Effort), threads)
		if err != nil {
			return "", "", "", err
		}
		if err := notify(); err != nil {
			return "", "", "", err
		}
		raster := filepath.Join(dir, "roundtrip.png")
		if err := c.run(ctx, "djxl", dir, path, raster, "--bits_per_sample=0", threads); err != nil {
			return "", "", "", err
		}
		if err := verifyPNG(ctx, source, raster, true); err != nil {
			return "", "", "", err
		}
		return path, "native-samples-alpha-precision-srgb", jxlTool + "; Go PNG verifier", nil
	case core.CompressCompatible:
		switch family {
		case core.FamilyPNG:
			if _, err := readPNG(ctx, source, false); err != nil {
				return "", "", "", err
			}
			levels := []string{"1"}
			if options.Effort != core.CompressFast {
				levels = []string{"1", "2"}
			}
			if options.Effort == core.CompressMaximum {
				levels = append(levels, "6")
			}
			for i, level := range levels {
				candidate := filepath.Join(dir, fmt.Sprintf("candidate-%d.png", i))
				args := []string{"--nx", "--interlace", "keep", "--threads", strconv.Itoa(options.Threads), "--opt", level, "--out", candidate, source}
				if err := c.run(ctx, "oxipng", dir, args...); err != nil {
					return "", "", "", err
				}
				if err := notify(); err != nil {
					return "", "", "", err
				}
				if err := verifyPNG(ctx, source, candidate, false); err != nil {
					if errors.Is(err, ErrPreservation) {
						continue
					}
					return "", "", "", err
				}
				path, err = smaller(ctx, path, candidate)
				if err != nil {
					return "", "", "", err
				}
			}
			if path == "" {
				return "", "", "", ErrPreservation
			}
			return path, "native-samples-alpha-precision-metadata", pngTool + "; Go PNG verifier", nil
		case core.FamilyJPEG:
			metadata, err := jpegMetadata(ctx, source)
			if err != nil {
				return "", "", "", err
			}
			canonical := filepath.Join(dir, "canonical-source.jpg")
			if err := c.run(ctx, "jpegtran", dir, "-copy", "none", "-optimize", "-outfile", canonical, source); err != nil {
				return "", "", "", err
			}
			expected, _, err := fileops.FileDigest(ctx, canonical)
			if err != nil {
				return "", "", "", err
			}
			count := 1
			if options.Effort != core.CompressFast {
				count = 2
			}
			for i := range count {
				candidate := filepath.Join(dir, fmt.Sprintf("candidate-%d.jpg", i))
				args := []string{"-copy", "none", "-optimize", "-outfile", candidate}
				if i == 1 {
					args = append(args, "-progressive")
				}
				if err := c.run(ctx, "jpegtran", dir, append(args, source)...); err != nil {
					return "", "", "", err
				}
				if err := restoreJPEGMarkers(ctx, candidate, metadata); err != nil {
					return "", "", "", err
				}
				if err := notify(); err != nil {
					return "", "", "", err
				}
				got, err := jpegMetadata(ctx, candidate)
				if err != nil {
					return "", "", "", err
				}
				if !equalChunks(metadata, got) {
					continue
				}
				normal := filepath.Join(dir, "canonical-output.jpg")
				if err := c.run(ctx, "jpegtran", dir, "-copy", "none", "-optimize", "-outfile", normal, candidate); err != nil {
					return "", "", "", err
				}
				digest, _, err := fileops.FileDigest(ctx, normal)
				if err != nil {
					return "", "", "", err
				}
				if digest != expected {
					continue
				}
				// Decode both with the same pinned decoder as a separate check.
				for n, input := range []string{source, candidate} {
					if err := c.run(ctx, "djpeg", dir, "-outfile", filepath.Join(dir, fmt.Sprintf("decoded-%d.ppm", n)), input); err != nil {
						return "", "", "", err
					}
				}
				a, _, err := fileops.FileDigest(ctx, filepath.Join(dir, "decoded-0.ppm"))
				if err != nil {
					return "", "", "", err
				}
				b, _, err := fileops.FileDigest(ctx, filepath.Join(dir, "decoded-1.ppm"))
				if err != nil {
					return "", "", "", err
				}
				if a != b {
					continue
				}
				path, err = smaller(ctx, path, candidate)
				if err != nil {
					return "", "", "", err
				}
			}
			if path == "" {
				return "", "", "", ErrPreservation
			}
			return path, "jpeg-coefficients-parameters-metadata-decode", jpegTool, nil
		default:
			return "", "", "", ErrUnsupported
		}
	default:
		return "", "", "", ErrUnsupported
	}
}

func smaller(ctx context.Context, best, candidate string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	info, err := os.Stat(candidate)
	if err != nil {
		return "", err
	}
	if best == "" {
		return candidate, nil
	}
	old, err := os.Stat(best)
	if err != nil {
		return "", err
	}
	if info.Size() < old.Size() {
		return candidate, nil
	}
	return best, nil
}

func jxlEffort(e core.CompressionEffort) string {
	switch e {
	case core.CompressFast:
		return "3"
	case core.CompressMaximum:
		return "9"
	default:
		return "7"
	}
}
