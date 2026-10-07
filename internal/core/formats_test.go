package core

import (
	"strings"
	"testing"
)

func TestCatalogueEntriesAreWellFormed(t *testing.T) {
	list := Formats()
	if len(list) != len(formats) {
		t.Fatalf("Formats lists %d of %d", len(list), len(formats))
	}
	for i, f := range list {
		if !strings.HasPrefix(f.Ext, ".") || f.Ext != strings.ToLower(f.Ext) {
			t.Errorf("%q: an extension is lower case with its dot", f.Ext)
		}
		if f.Kind != KindPhoto && f.Kind != KindVideo {
			t.Errorf("%s: kind %q", f.Ext, f.Kind)
		}
		if f.Family == "" {
			t.Errorf("%s: no family", f.Ext)
		}
		// The WebView is sent a photo as an image and a video as a video.
		if f.WebType != "" && !strings.HasPrefix(f.WebType, map[Kind]string{KindPhoto: "image/", KindVideo: "video/"}[f.Kind]) {
			t.Errorf("%s: a %s is sent as %s", f.Ext, f.Kind, f.WebType)
		}
		if i > 0 && list[i-1].Ext >= f.Ext {
			t.Errorf("Formats is not in order at %s", f.Ext)
		}
	}
}

func TestFormatsAreFoundByPathOrExtensionWhateverTheirCase(t *testing.T) {
	for name, want := range map[string]Family{
		`C:\Photos\IMG_0001.CR2`: FamilyRAW, ".dng": FamilyRAW, "a.b.NEF": FamilyRAW, "x.arw": FamilyRAW,
		"x.raf": FamilyRAW, "x.orf": FamilyRAW, "x.rw2": FamilyRAW, "x.pef": FamilyRAW, "x.cr3": FamilyRAW,
		"photo.JPeG": FamilyJPEG, ".heic": FamilyHEIF, "clip.MOV": FamilyMP4, "clip.m2ts": FamilyTransport,
		"notes.txt": "", "noextension": "", "": "", "cr2": "",
	} {
		if got := FamilyOf(name); got != want {
			t.Errorf("FamilyOf(%q) = %q, want %q", name, got, want)
		}
	}
	if KindOf("a.CR2") != KindPhoto || KindOf("a.mkv") != KindVideo || KindOf("a.txt") != "" {
		t.Error("KindOf disagrees with the catalogue")
	}
}

func TestOnlyWhatTheWebViewShowsIsSentAsItIs(t *testing.T) {
	for ext, want := range map[string]bool{
		".jpg": true, ".PNG": true, ".avif": true, ".gif": true,
		".tiff": false, ".heic": false, ".dng": false, ".cr2": false, // decoded by Lumen, shown as a render
		".mp4": false, // a video is played, not displayed as a photo
	} {
		if got := WebNative(ext); got != want {
			t.Errorf("WebNative(%s) = %t, want %t", ext, got, want)
		}
	}
	for ext, want := range map[string]string{
		".JPG": "image/jpeg", ".mov": "video/quicktime", ".webm": "video/webm",
		".nef": "application/octet-stream", ".mkv": "application/octet-stream", ".zip": "application/octet-stream",
	} {
		if got := ContentType(ext); got != want {
			t.Errorf("ContentType(%s) = %s, want %s", ext, got, want)
		}
	}
}
