package testenv

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
	"testing"
)

// DNG describes a small Adobe DNG, a camera RAW file, for tests: uncompressed
// 16-bit sensor data behind a Bayer filter (red, two greens, blue), lit red on its
// left half and blue on its right, with a plain green 160x120 JPEG as its
// embedded preview. Its camera is "Lumen Test Camera" and it was taken at
// 2024-03-05 10:20:30. It is built here rather than kept as a file so each
// test can say what it wants.
type DNG struct {
	Orientation uint16 // EXIF 1..8; 0 is 1
	Cut         bool   // the file ends halfway through its sensor data
}

// The sensor size of a DNG, before its orientation is applied.
const (
	DNGWidth  = 64
	DNGHeight = 48
)

type tiffTag struct {
	id, kind uint16
	count    uint32
	data     []byte
}

func tiffShort(id, v uint16) tiffTag {
	return tiffTag{id, 3, 1, binary.LittleEndian.AppendUint16(nil, v)}
}

func tiffLong(id uint16, v uint32) tiffTag {
	return tiffTag{id, 4, 1, binary.LittleEndian.AppendUint32(nil, v)}
}

func tiffASCII(id uint16, s string) tiffTag {
	return tiffTag{id, 2, uint32(len(s) + 1), append([]byte(s), 0)}
}

// appendIFD writes tags (in ascending order) as a TIFF directory at the end
// of buf, and returns where the directory starts and where each tag's value
// field is, for offsets that are only known later.
func appendIFD(buf []byte, tags []tiffTag) (out []byte, start int, fields map[uint16]int) {
	le := binary.LittleEndian
	start = len(buf)
	buf = le.AppendUint16(buf, uint16(len(tags)))
	buf = append(buf, make([]byte, 12*len(tags)+4)...)
	fields = map[uint16]int{}
	for i, tag := range tags {
		at := start + 2 + 12*i
		le.PutUint16(buf[at:], tag.id)
		le.PutUint16(buf[at+2:], tag.kind)
		le.PutUint32(buf[at+4:], tag.count)
		if len(tag.data) <= 4 {
			copy(buf[at+8:], tag.data)
		} else {
			if len(buf)%2 == 1 {
				buf = append(buf, 0)
			}
			le.PutUint32(buf[at+8:], uint32(len(buf)))
			buf = append(buf, tag.data...)
		}
		fields[tag.id] = at + 8
	}
	return buf, start, fields
}

// Bytes is the file.
func (d DNG) Bytes(tb testing.TB) []byte {
	tb.Helper()
	le := binary.LittleEndian
	green := image.NewRGBA(image.Rect(0, 0, 160, 120))
	for i := 0; i < len(green.Pix); i += 4 {
		green.Pix[i+1], green.Pix[i+3] = 200, 255
	}
	var preview bytes.Buffer
	if err := jpeg.Encode(&preview, green, nil); err != nil {
		tb.Fatal(err)
	}
	identity := make([]byte, 0, 72) // nine signed rationals: the sensor's colours are XYZ
	for i := range 9 {
		identity = le.AppendUint32(le.AppendUint32(identity, uint32(1-min(i%4, 1))), 1)
	}
	neutral := make([]byte, 0, 24)
	for range 3 {
		neutral = le.AppendUint32(le.AppendUint32(neutral, 1), 1)
	}
	buf := []byte("II\x2a\x00\x08\x00\x00\x00")
	buf, _, main := appendIFD(buf, []tiffTag{
		tiffLong(254, 0), tiffLong(256, DNGWidth), tiffLong(257, DNGHeight), tiffShort(258, 16),
		tiffShort(259, 1), tiffShort(262, 32803), // uncompressed, a colour filter array
		tiffASCII(271, "Lumen"), tiffASCII(272, "Test Camera"),
		tiffLong(273, 0), tiffShort(274, max(d.Orientation, 1)), tiffShort(277, 1), tiffLong(278, DNGHeight),
		tiffLong(279, DNGWidth*DNGHeight*2), tiffShort(284, 1), tiffASCII(306, "2024:03:05 10:20:30"), tiffLong(330, 0),
		{33421, 3, 2, []byte{2, 0, 2, 0}}, {33422, 1, 4, []byte{0, 1, 1, 2}}, // the 2x2 pattern: R G / G B
		{50706, 1, 4, []byte{1, 4, 0, 0}}, {50707, 1, 4, []byte{1, 1, 0, 0}},
		tiffASCII(50708, "Lumen Test Camera"), tiffLong(50717, 65535),
		{50721, 10, 9, identity}, {50728, 5, 3, neutral}, tiffShort(50778, 21),
	})
	buf, subIFD, sub := appendIFD(buf, []tiffTag{
		tiffLong(254, 1), tiffLong(256, 160), tiffLong(257, 120), {258, 3, 3, []byte{8, 0, 8, 0, 8, 0}},
		tiffShort(259, 7), tiffShort(262, 6), tiffLong(273, 0), tiffShort(277, 3), tiffLong(278, 120),
		tiffLong(279, uint32(preview.Len())), tiffShort(284, 1),
	})
	le.PutUint32(buf[main[330]:], uint32(subIFD))
	// The preview goes before the sensor data, as cameras write it, so a file
	// cut short keeps it.
	le.PutUint32(buf[sub[273]:], uint32(len(buf)))
	buf = append(buf, preview.Bytes()...)
	le.PutUint32(buf[main[273]:], uint32(len(buf)))
	for y := range DNGHeight {
		for x := range DNGWidth {
			red, blue := y%2 == 0 && x%2 == 0, y%2 == 1 && x%2 == 1
			v := uint16(3000)
			if (x < DNGWidth/2 && red) || (x >= DNGWidth/2 && blue) {
				v = 50000
			}
			buf = le.AppendUint16(buf, v)
		}
	}
	if d.Cut {
		buf = buf[:len(buf)-DNGWidth*DNGHeight]
	}
	return buf
}

// TIFF is a TIFF file with one 40x30 page of each colour, in order.
func TIFF(pages ...[3]byte) []byte {
	const w, h = 40, 30
	le := binary.LittleEndian
	buf := []byte("II\x2a\x00\x08\x00\x00\x00")
	next := 4 // where the offset of the next directory goes
	for _, c := range pages {
		if len(buf)%2 == 1 {
			buf = append(buf, 0)
		}
		le.PutUint32(buf[next:], uint32(len(buf)))
		tags := []tiffTag{
			tiffLong(256, w), tiffLong(257, h), {258, 3, 3, []byte{8, 0, 8, 0, 8, 0}}, tiffShort(259, 1),
			tiffShort(262, 2), tiffLong(273, 0), tiffShort(277, 3), tiffLong(278, h), tiffLong(279, w*h*3),
		}
		var start int
		var fields map[uint16]int
		buf, start, fields = appendIFD(buf, tags)
		next = start + 2 + 12*len(tags)
		le.PutUint32(buf[fields[273]:], uint32(len(buf)))
		for range w * h {
			buf = append(buf, c[:]...)
		}
	}
	return buf
}
