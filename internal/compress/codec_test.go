package compress

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"lumen/internal/core"
	"lumen/internal/fileops"
	"lumen/internal/testenv"
)

func TestCompressionHistoryNamesThePinnedCodecVersions(t *testing.T) {
	for _, tool := range []struct{ file, name, path string }{
		{"compression-deps.sh", jpegTool, "libjpeg-turbo/releases/download/"},
		{"compression-deps.sh", jxlTool, "libjxl/releases/download/v"},
		{"oxipng-deps.sh", pngTool, "crates/oxipng/oxipng-"},
	} {
		data, err := os.ReadFile(filepath.Join("..", "..", "scripts", tool.file))
		if err != nil {
			t.Fatal(err)
		}
		_, version, okay := strings.Cut(tool.name, " ")
		if !okay || !strings.Contains(string(data), tool.path+version) {
			t.Errorf("history tool %q disagrees with dependency pin", tool.name)
		}
	}
}

func codecFixture(t *testing.T, ext string, highPrecision bool) (string, Codec) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "source"+ext)
	var im image.Image
	if highPrecision {
		pixels := image.NewNRGBA64(image.Rect(0, 0, 256, 128))
		for y := range 128 {
			for x := range 256 {
				pixels.SetNRGBA64(x, y, color.NRGBA64{R: uint16(x * 201), G: uint16(y * 311), B: 29127, A: uint16((x % 3) * 32767)})
			}
		}
		im = pixels
	} else {
		pixels := image.NewNRGBA(image.Rect(0, 0, 256, 128))
		for y := range 128 {
			for x := range 256 {
				pixels.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 117, A: uint8((x % 2) * 255)})
			}
		}
		im = pixels
	}
	var data bytes.Buffer
	if ext == ".jpg" {
		if err := jpeg.Encode(&data, im, &jpeg.Options{Quality: 92}); err != nil {
			t.Fatal(err)
		}
	} else {
		encoder := png.Encoder{CompressionLevel: png.NoCompression}
		if err := encoder.Encode(&data, im); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(path, data.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, Codec{Runtime: testenv.Runtime(t)}
}

func TestNativeCodecsPreservePrecisionInvisibleRGBAndOriginalJPEGBytes(t *testing.T) {
	for _, tc := range []struct {
		name, ext string
		precision bool
		mode      core.CompressionMode
	}{
		{"JPEG coefficients", ".jpg", false, core.CompressCompatible},
		{"PNG invisible RGB", ".png", false, core.CompressCompatible},
		{"PNG sixteen bit", ".png", true, core.CompressCompatible},
		{"JPEG reconstruction", ".jpg", false, core.CompressReversible},
		{"PNG exact conversion", ".png", false, core.CompressConvert},
		{"PNG sixteen bit conversion", ".png", true, core.CompressConvert},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, codec := codecFixture(t, tc.ext, tc.precision)
			if !codec.Available(t.Context()) {
				t.Skip("compression tools missing")
			}
			before, size, err := fileops.FileDigest(t.Context(), source)
			if err != nil {
				t.Fatal(err)
			}
			o := core.DefaultCompressionOptions()
			o.Mode = tc.mode
			start := time.Now()
			path, verification, tool, err := codec.Encode(t.Context(), source, filepath.Dir(source), o, func() error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			_, out, err := fileops.FileDigest(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			after, _, err := fileops.FileDigest(t.Context(), source)
			if err != nil || after != before {
				t.Fatal("source was modified")
			}
			t.Logf("%s: %d -> %d bytes, %s, %s, %s", tool, size, out, time.Since(start), verification, tc.name)
		})
	}
}

func TestJPEGOptimisationRetainsEveryOrientationAndOpaqueMetadataPayload(t *testing.T) {
	for orientation := byte(1); orientation <= 8; orientation++ {
		t.Run(string(rune('0'+orientation)), func(t *testing.T) {
			source, codec := codecFixture(t, ".jpg", false)
			data, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			// A valid little-endian EXIF orientation IFD, plus opaque ICC, XMP
			// and comment payloads: the optimiser must retain exact bytes.
			exif := []byte{'E', 'x', 'i', 'f', 0, 0, 'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 1, 3, 0, 1, 0, 0, 0, orientation, 0, 0, 0, 0, 0, 0, 0}
			markers := [][]byte{append([]byte{225}, exif...), append([]byte{226}, []byte("ICC_PROFILE\x00\x01\x01opaque profile bytes")...), append([]byte{225}, []byte("http://ns.adobe.com/xap/1.0/\x00embedded copyright XMP")...), append([]byte{254}, []byte("copyright and comment")...)}
			var out bytes.Buffer
			out.Write(data[:2])
			for _, marker := range markers {
				out.Write([]byte{255, marker[0]})
				var n [2]byte
				binary.BigEndian.PutUint16(n[:], uint16(len(marker)+1))
				out.Write(n[:])
				out.Write(marker[1:])
			}
			out.Write(data[2:])
			if err := os.WriteFile(source, out.Bytes(), 0o600); err != nil {
				t.Fatal(err)
			}
			path, _, _, err := codec.Encode(t.Context(), source, filepath.Dir(source), core.DefaultCompressionOptions(), func() error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			got, err := jpegMetadata(t.Context(), path)
			if err != nil || !equalChunks(markers, got) {
				t.Fatalf("metadata changed: %v %v", got, err)
			}
		})
	}
}

func TestPNGOptimisationRetainsGreyscaleAndPaletteSamples(t *testing.T) {
	for _, im := range []image.Image{image.NewGray16(image.Rect(0, 0, 40, 30)), image.NewPaletted(image.Rect(0, 0, 40, 30), color.Palette{color.NRGBA{R: 127, G: 43, B: 181, A: 0}, color.White})} {
		source, codec := codecFixture(t, ".png", false)
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, im); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(source, encoded.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := codec.Encode(t.Context(), source, filepath.Dir(source), core.DefaultCompressionOptions(), func() error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMalformedPNGWithoutAHeaderIsRefusedBeforeSampleConversion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.png")
	data := append([]byte{137, 80, 78, 71, 13, 10, 26, 10}, pngChunk("IDAT", make([]byte, 30))...)
	data = append(data, pngChunk("IEND", nil)...)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPNG(t.Context(), path, true); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("malformed PNG accepted: %v", err)
	}
}

func pngChunk(name string, data []byte) []byte {
	result := make([]byte, len(data)+12)
	binary.BigEndian.PutUint32(result[:4], uint32(len(data)))
	copy(result[4:8], name)
	copy(result[8:], data)
	binary.BigEndian.PutUint32(result[len(result)-4:], crc32.ChecksumIEEE(result[4:len(result)-4]))
	return result
}

func TestPNGOptimisationPreservesPrivateMetadataAndConversionRefusesIt(t *testing.T) {
	source, codec := codecFixture(t, ".png", true)
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	chunk := pngChunk("vpAg", []byte("private metadata and copyright"))
	data = append(append(slices.Clone(data[:33]), chunk...), data[33:]...)
	if err := os.WriteFile(source, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPNG(t.Context(), source, true); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("private metadata conversion: %v", err)
	}
	o := core.DefaultCompressionOptions()
	path, _, _, err := codec.Encode(t.Context(), source, filepath.Dir(source), o, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	out, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(out, chunk) {
		t.Fatal("private metadata was not retained")
	}
}

func TestPNGVerificationRejectsChangedInvisibleRGBAndAnimation(t *testing.T) {
	source, _ := codecFixture(t, ".png", false)
	representation, err := readPNG(t.Context(), source, false)
	if err != nil {
		t.Fatal(err)
	}
	im, ok := representation.image.(*image.NRGBA)
	if !ok {
		t.Fatal("fixture is not unassociated RGBA")
	}
	im.SetNRGBA(0, 0, color.NRGBA{R: 99, G: 12, B: 42, A: 0})
	var output bytes.Buffer
	if err := png.Encode(&output, im); err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(filepath.Dir(source), "changed.png")
	if err := os.WriteFile(candidate, output.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyPNG(t.Context(), source, candidate, false); !errors.Is(err, ErrPreservation) {
		t.Fatalf("invisible RGB change: %v", err)
	}
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	data = append(append(slices.Clone(data[:33]), pngChunk("acTL", make([]byte, 8))...), data[33:]...)
	if err := os.WriteFile(source, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPNG(t.Context(), source, false); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("animation: %v", err)
	}
}
