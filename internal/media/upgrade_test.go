package media

import (
	"bytes"
	"encoding/binary"
	"image/jpeg"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"testing"
	"unicode/utf16"

	"lumen/internal/media/vips"
)

// iccProfile is a minimal RGB profile holding only a description, as an
// ICC v2 'desc' or a v4 'mluc' tag.
func iccProfile(desc string, v4 bool) []byte {
	be := binary.BigEndian
	var tag []byte
	if v4 {
		u := utf16.Encode([]rune(desc))
		tag = append([]byte("mluc\x00\x00\x00\x00"), be.AppendUint32(be.AppendUint32(nil, 1), 12)...)
		tag = append(tag, "enUS"...)
		tag = be.AppendUint32(be.AppendUint32(tag, uint32(2*len(u))), 28)
		for _, c := range u {
			tag = be.AppendUint16(tag, c)
		}
	} else {
		tag = append([]byte("desc\x00\x00\x00\x00"), be.AppendUint32(nil, uint32(len(desc)+1))...)
		tag = append(append(tag, desc...), 0)
	}
	head := make([]byte, 128)
	copy(head[12:], "mntr")
	copy(head[16:], "RGB ")
	copy(head[20:], "XYZ ")
	copy(head[36:], "acsp")
	table := be.AppendUint32(nil, 1) // one tag
	table = append(table, "desc"...)
	table = be.AppendUint32(be.AppendUint32(table, 144), uint32(len(tag)))
	icc := slices.Concat(head, table, tag)
	be.PutUint32(icc, uint32(len(icc)))
	return icc
}

// withICC inserts a colour profile (an APP2 ICC_PROFILE segment) into a JPEG.
func withICC(jpg, icc []byte) []byte {
	payload := append([]byte("ICC_PROFILE\x00\x01\x01"), icc...)
	seg := []byte{0xFF, 0xE2, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(payload)+2))
	out := append([]byte{0xFF, 0xD8}, seg...)
	out = append(out, payload...)
	return append(out, jpg[2:]...)
}

func TestSRGBProfilesAreKnownByTheirDescription(t *testing.T) {
	for _, c := range []struct {
		desc string
		v4   bool
		want bool
	}{
		{"sRGB IEC61966-2.1", false, true},
		{"sRGB built-in", true, true},
		{"Display P3", true, false},
		{"Adobe RGB (1998)", false, false},
		{"Linear sRGB", false, false},
	} {
		if got := isSRGB(iccProfile(c.desc, c.v4)); got != c.want {
			t.Errorf("isSRGB(%q, v4 %v) = %v, want %v", c.desc, c.v4, got, c.want)
		}
	}
	// A damaged profile is not sRGB, and never panics.
	good := iccProfile("sRGB IEC61966-2.1", false)
	r := rand.New(rand.NewPCG(1, 2))
	for range 20000 {
		b := bytes.Clone(good[:r.IntN(len(good)+1)])
		for range r.IntN(4) {
			if len(b) > 0 {
				b[r.IntN(len(b))] = byte(r.Uint32())
			}
		}
		isSRGB(b)
	}
}

func TestDecodesAsAtTellsWhichPhotosTheColourFixChanged(t *testing.T) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, halves(64, 48, red, blue), nil); err != nil {
		t.Fatal(err)
	}
	plain := write(t, "plain.jpg", buf.Bytes())
	srgb := write(t, "srgb.jpg", withICC(buf.Bytes(), iccProfile("sRGB IEC61966-2.1", false)))
	if icc, err := vips.Profile(srgb); err != nil || icc == nil {
		t.Fatalf("the sRGB profile didn't read: %v", err)
	}
	p3 := filepath.Join("testdata", "red-p3.jpg")
	for _, c := range []struct {
		path, version string
		want          bool
	}{
		{plain, UnmanagedThumbnailVersion, true},
		{srgb, UnmanagedThumbnailVersion, true},
		{p3, UnmanagedThumbnailVersion, false},                      // converted now
		{write(t, "a.heic", nil), UnmanagedThumbnailVersion, false}, // WIC converts it now
		{filepath.Join(t.TempDir(), "gone.jpg"), UnmanagedThumbnailVersion, false},
		{plain, "vips-8.17-1", false}, // a version it knows nothing about
		{plain, HEIFProfilesVersion, true},
		{p3, HEIFProfilesVersion, true},                       // a JPEG: only HEIC changed since
		{write(t, "b.heic", nil), HEIFProfilesVersion, false}, // no header to read: unsure
	} {
		if got := DecodesAsAt(c.path, c.version); got != c.want {
			t.Errorf("DecodesAsAt(%s, %s) = %v, want %v", filepath.Base(c.path), c.version, got, c.want)
		}
	}
}
