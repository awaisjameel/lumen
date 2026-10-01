package media

import (
	"encoding/binary"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"lumen/internal/media/vips"
)

// UnmanagedThumbnailVersion is the last ThumbnailVersion before colour
// profiles were converted. Faces found before ai.FaceAnalysisVersion 2 were
// found in pixels decoded as it did.
const UnmanagedThumbnailVersion = "vips-8.18-1"

// HEIFProfilesVersion is the last ThumbnailVersion before HEIC decoded by
// FFmpeg (on PCs without the HEVC extension) had its profile converted too.
const HEIFProfilesVersion = "vips-8.18-2"

// EpochDatesMetadataVersion is the last photo MetadataVersion that took an
// unset clock's epoch (1970 or 1904) for a capture date.
const EpochDatesMetadataVersion = "imagemeta-1.1+goexif+vips-3"

// DecodesAsAt reports whether Thumbnail and Decode make the same pixels of
// a photo now as they did at an earlier ThumbnailVersion, so what was made
// from them then (thumbnails, faces) still stands and an upgrade needn't
// decode the photo again. It reads at most the file's header, and is false
// when unsure: a version it doesn't know, a header that won't read.
func DecodesAsAt(path, version string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	heif := ext == ".heic" || ext == ".heif"
	switch version {
	case HEIFProfilesVersion:
		// Only HEIC changed since, and only in a profile other than sRGB
		// (which WIC converted then too, where it decoded the file).
		if !heif {
			return true
		}
		icc, err := vips.Profile(path)
		return err == nil && (icc == nil || isSRGB(icc))
	case UnmanagedThumbnailVersion:
	default:
		return false
	}
	// Since then colour profiles are converted to sRGB (by libvips, and by
	// WIC for HEIC, which iPhones save in Display P3) and pixels Go decodes
	// keep straight alpha.
	if heif || ext == ".bmp" {
		return false
	}
	icc, err := vips.Profile(path)
	return err == nil && (icc == nil || isSRGB(icc))
}

// isSRGB reports whether an ICC profile is sRGB's, by its description:
// converting from it changes no pixel that matters.
func isSRGB(icc []byte) bool {
	if len(icc) < 132 || string(icc[16:20]) != "RGB " {
		return false
	}
	d := strings.ToLower(iccDescription(icc))
	return strings.Contains(d, "srgb") && !strings.Contains(d, "linear")
}

// iccDescription returns an ICC profile's description (its 'desc' tag),
// "" if it has none that reads.
func iccDescription(icc []byte) string {
	be := binary.BigEndian
	n := int(be.Uint32(icc[128:]))
	for i := range min(n, 100) {
		e := 132 + 12*i
		if e+12 > len(icc) {
			return ""
		}
		if string(icc[e:e+4]) != "desc" {
			continue
		}
		off, size := int(be.Uint32(icc[e+4:])), int(be.Uint32(icc[e+8:]))
		if off < 0 || size < 12 || off+size > len(icc) || off+size < off {
			return ""
		}
		tag := icc[off : off+size]
		switch string(tag[:4]) {
		case "desc": // ICC v2 textDescriptionType: an ASCII count, then the text
			count := int(be.Uint32(tag[8:]))
			if count > len(tag)-12 || count < 0 {
				return ""
			}
			return strings.TrimRight(string(tag[12:12+count]), "\x00")
		case "mluc": // ICC v4 multiLocalizedUnicodeType: the first record's UTF-16
			if len(tag) < 28 || be.Uint32(tag[8:]) == 0 {
				return ""
			}
			l, o := int(be.Uint32(tag[20:])), int(be.Uint32(tag[24:]))
			if o < 0 || l < 0 || o+l > len(tag) || o+l < o {
				return ""
			}
			u := make([]uint16, l/2)
			for k := range u {
				u[k] = be.Uint16(tag[o+2*k:])
			}
			return string(utf16.Decode(u))
		}
		return ""
	}
	return ""
}
