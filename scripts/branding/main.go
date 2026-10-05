// Generates platform and UI PNGs from build/appicon.png. Run from the repo root.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/image/draw"
)

func main() {
	iosAssets := flag.String("ios-assets", "", "also write opaque icons into the generated iOS AppIcon directory")
	flag.Parse()
	if err := generate(*iosAssets); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate(iosAssets string) error {
	data, err := os.ReadFile("build/appicon.png")
	if err != nil {
		return err
	}
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("decode logo master: %w", err)
	}
	if b := src.Bounds(); b.Dx() != b.Dy() || b.Dx() < 1024 {
		return errors.New("logo master must be square and at least 1024 pixels")
	}
	outputs := []struct {
		path   string
		size   int
		opaque bool
	}{
		{"frontend/public/lumen.png", 128, false},
		{"frontend/public/favicon.png", 32, false},
		{"build/linux/lumen.png", 128, false},
		{"build/ios/icon.png", 1024, true},
		{"build/appicon.icon/Assets/lumen.png", 1024, false},
		{"build/darwin/dmg-file-icon.png", 1024, false},
	}
	for _, output := range outputs {
		if err := writePNG(output.path, resize(src, output.size, output.opaque)); err != nil {
			return err
		}
	}
	for _, density := range []struct {
		name string
		size int
	}{{"mdpi", 48}, {"hdpi", 72}, {"xhdpi", 96}, {"xxhdpi", 144}, {"xxxhdpi", 192}} {
		icon := resize(src, density.size, false)
		for _, name := range []string{"ic_launcher.png", "ic_launcher_round.png"} {
			path := filepath.Join("build", "android", "app", "src", "main", "res", "mipmap-"+density.name, name)
			if err := writePNG(path, icon); err != nil {
				return err
			}
		}
	}
	// Keep the install-volume layout clear; the small mark sits below its icons.
	background := image.NewRGBA(image.Rect(0, 0, 540, 380))
	draw.Draw(background, background.Bounds(), image.NewUniform(color.RGBA{R: 245, G: 245, B: 246, A: 255}), image.Point{}, draw.Src)
	draw.Draw(background, image.Rect(444, 284, 516, 356), resize(src, 72, false), image.Point{}, draw.Over)
	if err := writePNG("build/darwin/dmg-background.png", background); err != nil {
		return err
	}
	if iosAssets != "" {
		return generateIOS(src, iosAssets)
	}
	return nil
}

func resize(src image.Image, size int, opaque bool) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	if opaque {
		// iOS App Store icons must have no transparency, including the corners.
		draw.Draw(dst, dst.Bounds(), image.NewUniform(color.RGBA{R: 245, G: 245, B: 246, A: 255}), image.Point{}, draw.Src)
	}
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	return dst
}

func generateIOS(src image.Image, dir string) error {
	// Wails creates Contents.json; keep its names and replace the transparent
	// PNGs it derives from the desktop master with opaque versions.
	data, err := os.ReadFile(filepath.Join(dir, "Contents.json"))
	if err != nil {
		return err
	}
	var contents struct {
		Images []struct {
			Filename string `json:"filename"`
			Size     string `json:"size"`
			Scale    string `json:"scale"`
		} `json:"images"`
	}
	if err := json.Unmarshal(data, &contents); err != nil {
		return err
	}
	for _, icon := range contents.Images {
		width, height, ok := strings.Cut(icon.Size, "x")
		points, pointsErr := strconv.ParseFloat(width, 64)
		scale, scaleErr := strconv.Atoi(strings.TrimSuffix(icon.Scale, "x"))
		pixels := points * float64(scale)
		if !ok || width != height || pointsErr != nil || scaleErr != nil || pixels < 1 || pixels > 1024 || pixels != float64(int(pixels)) || icon.Filename == "" || filepath.Base(icon.Filename) != icon.Filename {
			return fmt.Errorf("invalid iOS icon entry: %s (%s at %s)", icon.Filename, icon.Size, icon.Scale)
		}
		if err := writePNG(filepath.Join(dir, icon.Filename), resize(src, int(pixels), true)); err != nil {
			return err
		}
	}
	return nil
}

func writePNG(path string, img image.Image) error {
	var buffer bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if err := encoder.Encode(&buffer, img); err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if bytes.Equal(existing, buffer.Bytes()) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	return os.WriteFile(path, buffer.Bytes(), 0o600)
}
