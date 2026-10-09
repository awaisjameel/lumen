// Package compress prepares and verifies local candidates. Only fileops publishes them.
package compress

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"os"
	"slices"
)

// ErrPreservation refuses any candidate whose full preservation contract is unproved.
var ErrPreservation = errors.New("the image preservation contract could not be verified")

// ErrUnsupported refuses representations for which no preservation path is implemented.
var ErrUnsupported = errors.New("this representation cannot be compressed losslessly")

const (
	maxInputBytes = 256 << 20
	maxPixels     = 16_000_000
)

type pngRepresentation struct {
	header    []byte
	metadata  [][]byte
	positions []bool
	image     image.Image
}

func readPNG(ctx context.Context, path string, conversion bool) (pngRepresentation, error) {
	var result pngRepresentation
	data, err := readBounded(ctx, path)
	if err != nil {
		return result, err
	}
	if len(data) < 33 || !bytes.Equal(data[:8], []byte{137, 80, 78, 71, 13, 10, 26, 10}) {
		return result, ErrUnsupported
	}
	end := false
	afterData := false
	for at := 8; at < len(data); {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if len(data)-at < 12 {
			return result, ErrUnsupported
		}
		n := int(binary.BigEndian.Uint32(data[at : at+4]))
		if n > len(data)-at-12 {
			return result, ErrUnsupported
		}
		typeAndPayload := data[at+4 : at+8+n]
		if crc32.ChecksumIEEE(typeAndPayload) != binary.BigEndian.Uint32(data[at+8+n:at+12+n]) {
			return result, ErrUnsupported
		}
		name := string(data[at+4 : at+8])
		switch name {
		case "IHDR":
			if at != 8 || n != 13 {
				return result, ErrUnsupported
			}
			result.header = slices.Clone(data[at+8 : at+8+n])
			width, height := binary.BigEndian.Uint32(result.header[:4]), binary.BigEndian.Uint32(result.header[4:8])
			if width == 0 || height == 0 || uint64(width)*uint64(height) > maxPixels {
				return result, ErrUnsupported
			}
		case "IDAT":
			afterData = true
		case "IEND":
			if n != 0 || at+12 != len(data) {
				return result, ErrUnsupported
			}
			end = true
		case "acTL", "fcTL", "fdAT":
			return result, ErrUnsupported
		default:
			// Conversion initially accepts only plain sRGB RGB/RGBA PNGs. A
			// profile, private chunk or precision hint needs its own mapping.
			if conversion {
				return result, ErrUnsupported
			}
			result.metadata = append(result.metadata, slices.Clone(typeAndPayload))
			result.positions = append(result.positions, afterData)
		}
		at += 12 + n
	}
	if !end || len(result.header) != 13 {
		return result, ErrUnsupported
	}
	if conversion && (result.header[8] != 8 && result.header[8] != 16 || result.header[9] != 2 && result.header[9] != 6) {
		return result, ErrUnsupported
	}
	result.image, err = png.Decode(bytes.NewReader(data))
	return result, err
}

func verifyPNG(ctx context.Context, source, candidate string, conversion bool) error {
	a, err := readPNG(ctx, source, conversion)
	if err != nil {
		return err
	}
	b, err := readPNG(ctx, candidate, false)
	if err != nil {
		return err
	}
	if a.image.Bounds() != b.image.Bounds() {
		return ErrPreservation
	}
	if !conversion && (!bytes.Equal(a.header, b.header) || !equalChunks(a.metadata, b.metadata) || !slices.Equal(a.positions, b.positions)) {
		return ErrPreservation
	}
	if conversion {
		// djxl can express the source precision with another PNG colour type,
		// but may not reduce it or introduce a non-sRGB interpretation.
		if a.header[8] != b.header[8] {
			return ErrPreservation
		}
		for _, chunk := range b.metadata {
			if !standardSRGBChunk(chunk) {
				return ErrPreservation
			}
		}
	}
	for y := a.image.Bounds().Min.Y; y < a.image.Bounds().Max.Y; y++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		for x := a.image.Bounds().Min.X; x < a.image.Bounds().Max.X; x++ {
			// NRGBA64 keeps RGB under zero alpha, at original precision.
			if nativeSample(a.image.At(x, y)) != nativeSample(b.image.At(x, y)) {
				return ErrPreservation
			}
		}
	}
	return nil
}

func standardSRGBChunk(chunk []byte) bool {
	switch string(chunk[:4]) {
	case "sRGB":
		return len(chunk) == 5 && chunk[4] <= 3
	case "gAMA":
		return len(chunk) == 8 && binary.BigEndian.Uint32(chunk[4:]) == 45455
	case "cHRM":
		if len(chunk) != 36 {
			return false
		}
		for i, value := range []uint32{31270, 32900, 64000, 33000, 30000, 60000, 15000, 6000} {
			if binary.BigEndian.Uint32(chunk[4+i*4:8+i*4]) != value {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func nativeSample(sample color.Color) color.Color {
	switch c := sample.(type) {
	case color.NRGBA:
		return color.NRGBA64{R: uint16(c.R) * 257, G: uint16(c.G) * 257, B: uint16(c.B) * 257, A: uint16(c.A) * 257}
	case color.NRGBA64:
		return c
	default:
		return color.NRGBA64Model.Convert(c)
	}
}

func equalChunks(a, b [][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !bytes.Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}

func readBounded(ctx context.Context, path string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxInputBytes {
		return nil, ErrUnsupported
	}
	return os.ReadFile(path)
}

// jpegMetadata walks every scan rather than treating entropy bytes as markers.
// Auxiliary-image containers and trailing payloads are refused conservatively.
func jpegMetadata(ctx context.Context, path string) ([][]byte, error) {
	data, err := readBounded(ctx, path)
	if err != nil {
		return nil, err
	}
	if len(data) < 4 || data[0] != 255 || data[1] != 216 {
		return nil, ErrUnsupported
	}
	var chunks [][]byte
	scanned := false
	for at := 2; at < len(data); {
		if data[at] != 255 {
			return nil, ErrUnsupported
		}
		for at < len(data) && data[at] == 255 {
			at++
		}
		if at >= len(data) {
			return nil, ErrUnsupported
		}
		marker := data[at]
		at++
		if marker == 217 {
			if at != len(data) {
				return nil, ErrUnsupported
			}
			return chunks, nil
		}
		if marker == 216 || marker == 0 || marker >= 208 && marker <= 215 {
			return nil, ErrUnsupported
		}
		if len(data)-at < 2 {
			return nil, ErrUnsupported
		}
		n := int(binary.BigEndian.Uint16(data[at : at+2]))
		if n < 2 || n > len(data)-at {
			return nil, ErrUnsupported
		}
		payload := data[at+2 : at+n]
		if marker >= 224 && marker <= 239 || marker == 254 {
			if scanned {
				return nil, ErrUnsupported
			}
			if bytes.HasPrefix(payload, []byte("MPF\x00")) || marker == 235 {
				return nil, ErrUnsupported
			}
			chunks = append(chunks, append([]byte{marker}, payload...))
		}
		if marker >= 192 && marker <= 207 && marker != 196 && marker != 200 && marker != 204 {
			if marker != 192 && marker != 194 || len(payload) < 6 || payload[0] != 8 {
				return nil, ErrUnsupported
			}
			w, h := binary.BigEndian.Uint16(payload[3:5]), binary.BigEndian.Uint16(payload[1:3])
			if w == 0 || h == 0 || uint64(w)*uint64(h) > maxPixels {
				return nil, ErrUnsupported
			}
		}
		at += n
		if marker == 218 {
			scanned = true
			for at < len(data)-1 {
				if data[at] != 255 {
					at++
					continue
				}
				if data[at+1] == 0 || data[at+1] >= 208 && data[at+1] <= 215 {
					at += 2
					continue
				}
				break
			}
		}
	}
	return nil, ErrUnsupported
}

// restoreJPEGMarkers replaces the optimiser's generated JFIF/Adobe headers
// with the source's exact APP and comment markers. Coefficients are untouched.
func restoreJPEGMarkers(ctx context.Context, path string, metadata [][]byte) error {
	data, err := readBounded(ctx, path)
	if err != nil {
		return err
	}
	var out bytes.Buffer
	_, _ = out.Write(data[:2])
	for _, marker := range metadata {
		_, _ = out.Write([]byte{255, marker[0]})
		var size [2]byte
		binary.BigEndian.PutUint16(size[:], uint16(len(marker)+1))
		_, _ = out.Write(size[:])
		_, _ = out.Write(marker[1:])
	}
	for at := 2; at < len(data); {
		start := at
		if data[at] != 255 || at+3 >= len(data) {
			return ErrUnsupported
		}
		for at < len(data) && data[at] == 255 {
			at++
		}
		if at >= len(data) {
			return ErrUnsupported
		}
		marker := data[at]
		at++
		if marker == 218 || marker == 217 {
			_, _ = out.Write(data[start:])
			break
		}
		if at+2 > len(data) {
			return ErrUnsupported
		}
		n := int(binary.BigEndian.Uint16(data[at : at+2]))
		if n < 2 || n > len(data)-at {
			return ErrUnsupported
		}
		at += n
		if (marker < 224 || marker > 239) && marker != 254 {
			_, _ = out.Write(data[start:at])
		}
	}
	return os.WriteFile(path, out.Bytes(), 0o600)
}
