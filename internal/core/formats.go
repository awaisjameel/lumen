package core

import (
	"path/filepath"
	"slices"
	"strings"
)

// Family names one file format, whatever extensions its files carry. Code
// that treats a format specially (which decoder reads it, how it ranks among
// copies) asks for the family, never for an extension.
type Family string

// The photo families.
const (
	FamilyJPEG Family = "jpeg"
	FamilyPNG  Family = "png"
	FamilyGIF  Family = "gif"
	FamilyWebP Family = "webp"
	FamilyBMP  Family = "bmp"
	FamilyTIFF Family = "tiff"
	FamilyHEIF Family = "heif"
	FamilyAVIF Family = "avif"
	FamilyJXL  Family = "jxl" // JPEG XL
	// FamilyRAW is a camera's sensor data, whichever maker's: Adobe DNG,
	// Canon, Nikon, Sony, Fujifilm, Olympus and OM System, Panasonic, Pentax.
	// Being listed here is not a promise for every camera model; what a file
	// can do is found by reading it (see media.Decoders and MediaCaps).
	FamilyRAW Family = "raw"
)

// The video families, by container.
const (
	FamilyMP4       Family = "mp4" // ISO base media: MP4, M4V and QuickTime
	FamilyWebM      Family = "webm"
	FamilyAVI       Family = "avi"
	FamilyMatroska  Family = "matroska"
	Family3GP       Family = "3gp"
	FamilyWMV       Family = "wmv"       // ASF, the container of Windows Media
	FamilyTransport Family = "transport" // MPEG transport streams: AVCHD, Blu-ray, broadcast recordings
	FamilyMPEG      Family = "mpeg"      // MPEG program streams: MPEG-1 and MPEG-2 files, DVD video
	FamilyOgg       Family = "ogg"
)

// Format is one extension Lumen recognises.
type Format struct {
	Ext    string // lower case, with its dot
	Kind   Kind
	Family Family
	// WebType is the MIME type the file is sent to the WebView as when it is
	// sent as it is: a photo the WebView displays, or a video container it
	// may play (whether it does depends on the codecs inside: PlaybackOf).
	// "" for a file the WebView is never sent.
	WebType string
}

// formats is the catalogue: every extension Lumen takes for media. The
// scanner, the decoders and the media services all read it, so a format is
// added here and nowhere else.
var formats = index([]Format{
	{".jpg", KindPhoto, FamilyJPEG, "image/jpeg"},
	{".jpeg", KindPhoto, FamilyJPEG, "image/jpeg"},
	{".jpe", KindPhoto, FamilyJPEG, "image/jpeg"},
	{".png", KindPhoto, FamilyPNG, "image/png"},
	{".apng", KindPhoto, FamilyPNG, "image/apng"}, // an animated PNG; a .png can be one too
	{".gif", KindPhoto, FamilyGIF, "image/gif"},
	{".webp", KindPhoto, FamilyWebP, "image/webp"},
	{".bmp", KindPhoto, FamilyBMP, "image/bmp"},
	{".tif", KindPhoto, FamilyTIFF, ""},
	{".tiff", KindPhoto, FamilyTIFF, ""},
	{".heic", KindPhoto, FamilyHEIF, ""},
	{".heif", KindPhoto, FamilyHEIF, ""},
	{".avif", KindPhoto, FamilyAVIF, "image/avif"},
	{".jxl", KindPhoto, FamilyJXL, ""},

	{".dng", KindPhoto, FamilyRAW, ""}, // Adobe, phones, Leica, Pentax, DJI
	{".cr2", KindPhoto, FamilyRAW, ""}, // Canon
	{".cr3", KindPhoto, FamilyRAW, ""},
	{".crw", KindPhoto, FamilyRAW, ""},
	{".nef", KindPhoto, FamilyRAW, ""}, // Nikon
	{".nrw", KindPhoto, FamilyRAW, ""},
	{".arw", KindPhoto, FamilyRAW, ""}, // Sony
	{".sr2", KindPhoto, FamilyRAW, ""},
	{".srf", KindPhoto, FamilyRAW, ""},
	{".raf", KindPhoto, FamilyRAW, ""}, // Fujifilm
	{".orf", KindPhoto, FamilyRAW, ""}, // Olympus, OM System
	{".rw2", KindPhoto, FamilyRAW, ""}, // Panasonic
	{".pef", KindPhoto, FamilyRAW, ""}, // Pentax

	{".mp4", KindVideo, FamilyMP4, "video/mp4"},
	{".m4v", KindVideo, FamilyMP4, "video/mp4"},
	{".mov", KindVideo, FamilyMP4, "video/quicktime"},
	{".webm", KindVideo, FamilyWebM, "video/webm"},
	{".avi", KindVideo, FamilyAVI, ""},
	{".mkv", KindVideo, FamilyMatroska, ""},
	{".3gp", KindVideo, Family3GP, ""},
	{".wmv", KindVideo, FamilyWMV, ""},
	{".asf", KindVideo, FamilyWMV, ""},
	{".mts", KindVideo, FamilyTransport, ""},
	{".m2ts", KindVideo, FamilyTransport, ""},
	// TypeScript sources share this name; the scanner tells them apart by
	// reading the file's first packets.
	{".ts", KindVideo, FamilyTransport, ""},
	{".mpg", KindVideo, FamilyMPEG, ""},
	{".mpeg", KindVideo, FamilyMPEG, ""},
	// The WebView no longer decodes Theora, so an Ogg video is always converted.
	{".ogv", KindVideo, FamilyOgg, ""},
})

func index(list []Format) map[string]Format {
	m := make(map[string]Format, len(list))
	for _, f := range list {
		m[f.Ext] = f
	}
	return m
}

// Formats is the catalogue, ordered by extension.
func Formats() []Format {
	list := make([]Format, 0, len(formats))
	for _, f := range formats {
		list = append(list, f)
	}
	slices.SortFunc(list, func(a, b Format) int { return strings.Compare(a.Ext, b.Ext) })
	return list
}

// FormatOf is the format of a file, by its path or its extension alone
// (".CR2"), and whether Lumen recognises it.
func FormatOf(name string) (Format, bool) {
	f, ok := formats[strings.ToLower(filepath.Ext(name))]
	return f, ok
}

// FamilyOf is the family of a file, by its path or its extension alone; ""
// if Lumen does not recognise it.
func FamilyOf(name string) Family {
	f, _ := FormatOf(name)
	return f.Family
}

// KindOf returns the media kind for a path, or "" if Lumen ignores it.
func KindOf(path string) Kind {
	f, _ := FormatOf(path)
	return f.Kind
}

// WebNative reports whether the WebView can display a photo of this extension.
func WebNative(ext string) bool {
	f, _ := FormatOf(ext)
	return f.Kind == KindPhoto && f.WebType != ""
}

// ContentType is the MIME type to send a file of this extension as, when
// the WebView is sent it as it is: a photo it displays or a video it plays
// (from Lumen's own table: Windows' registry, which Go would ask, varies by PC).
func ContentType(ext string) string {
	if f, _ := FormatOf(ext); f.WebType != "" {
		return f.WebType
	}
	return "application/octet-stream"
}
