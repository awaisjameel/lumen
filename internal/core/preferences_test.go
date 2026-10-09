package core

import (
	"reflect"
	"testing"
)

func TestNormalisedPreferencesReplaceWhatTheUICannotApply(t *testing.T) {
	got := Preferences{Theme: "sepia", Accent: "neon", Density: "tiny", TextScale: 400, TileSize: 1,
		SidebarWidth: 10_000, InspectorWidth: -5, SidebarCollapsed: true}.Normalised()
	want := Preferences{Theme: ThemeSystem, Accent: AccentBlue, Density: DensityComfortable,
		TextScale: TextScaleMax, TileSize: TileSizeMin, SidebarWidth: SidebarWidthMax,
		InspectorWidth: InspectorWidthMin, SidebarCollapsed: true}
	want = want.Normalised()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Normalised() = %+v, want %+v", got, want)
	}
}

func TestNormalisedPreferencesKeepValidChoices(t *testing.T) {
	p := Preferences{Theme: ThemeDark, Accent: AccentRose, Density: DensityCompact, TextScale: 110,
		TileSize: 200, SidebarWidth: 300, InspectorWidth: 400, Motion: MotionReduced,
		ViewerBackground: ViewerBackgroundGrey, StartView: StartPeople, ThumbFit: ThumbContain, FocusRing: FocusStrong,
		HideVideoBadge: true, Shortcuts: map[string]string{"viewer.zoomIn": "Z"},
		ExportPresets: []ExportPreset{{Name: "Web", Format: ExportJPEG, Side: 2048, Quality: 80}}}
	viewer := DefaultViewerPreferences()
	p.Viewer = &viewer
	p.Compression = DefaultCompressionOptions()
	if got := p.Normalised(); !reflect.DeepEqual(got, p) {
		t.Errorf("Normalised() = %+v, want it unchanged: %+v", got, p)
	}
	if def := DefaultPreferences(); !reflect.DeepEqual(def.Normalised(), def) {
		t.Errorf("the defaults are not valid: %+v", def)
	}
}

func TestCapsOfOffersOnlyWhatIsProven(t *testing.T) {
	for _, c := range []struct {
		name    string
		kind    Kind
		ext     string
		preview bool
		play    Playback
		want    MediaCaps
	}{
		{"a JPEG the WebView shows", KindPhoto, ".JPG", true, "", MediaCaps{Preview: true, Original: true}},
		{"a HEIC it cannot show", KindPhoto, ".heic", true, "", MediaCaps{Preview: true}},
		{"a camera RAW it cannot show", KindPhoto, ".CR2", true, "", MediaCaps{Preview: true}},
		{"a TIFF, which may hold pages", KindPhoto, ".tif", true, "", MediaCaps{Preview: true, Paged: true}},
		{"an animated PNG the WebView plays", KindPhoto, ".apng", true, "", MediaCaps{Preview: true, Original: true}},
		{"a photo not yet indexed", KindPhoto, ".png", false, "", MediaCaps{Original: true}},
		{"a video played directly", KindVideo, ".mp4", true, PlayDirect,
			MediaCaps{Preview: true, DirectPlay: true, ConvertPlay: true}},
		{"a converted video", KindVideo, ".avi", false, PlayConvert, MediaCaps{ConvertPlay: true}},
		{"HEVC, decided by the player", KindVideo, ".mov", true, PlayHEVC,
			MediaCaps{Preview: true, DirectPlay: true, ConvertPlay: true}},
	} {
		if got := CapsOf(c.kind, c.ext, c.preview, c.play); got != c.want {
			t.Errorf("%s: CapsOf = %+v, want %+v", c.name, got, c.want)
		}
	}
}
