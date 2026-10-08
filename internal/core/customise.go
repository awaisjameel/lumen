package core

import (
	"reflect"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// Motion is how much the interface moves.
type Motion string

// The motion choices. MotionSystem follows Windows' animation setting.
const (
	MotionSystem  Motion = "system"
	MotionReduced Motion = "reduced"
	MotionFull    Motion = "full"
)

// ViewerBackground is what surrounds a photo in the viewer. Each is dark:
// the viewer's controls are light in either theme.
type ViewerBackground string

// The viewer backgrounds.
const (
	ViewerBackgroundDark  ViewerBackground = "dark"
	ViewerBackgroundBlack ViewerBackground = "black"
	ViewerBackgroundGrey  ViewerBackground = "grey"
)

// StartView is the screen Lumen opens on.
type StartView string

// The start views.
const (
	StartLibrary    StartView = "library"
	StartFavourites StartView = "favourites"
	StartVideos     StartView = "videos"
	StartPeople     StartView = "people"
	StartThings     StartView = "things"
	StartPlaces     StartView = "places"
	StartEvents     StartView = "events"
	StartAlbums     StartView = "albums"
)

// ThumbFit is how a thumbnail fills its tile.
type ThumbFit string

// The thumbnail fits: ThumbCover crops to fill the tile, ThumbContain shows
// the whole picture.
const (
	ThumbCover   ThumbFit = "cover"
	ThumbContain ThumbFit = "contain"
)

// FocusRing is how strongly keyboard focus is drawn.
type FocusRing string

// The focus rings.
const (
	FocusStandard FocusRing = "standard"
	FocusStrong   FocusRing = "strong"
)

// ViewerOpen is how a photo is first shown in the viewer.
type ViewerOpen string

// The ways a photo opens.
const (
	ViewerOpenFit    ViewerOpen = "fit"    // the whole picture, fitted to the window
	ViewerOpenActual ViewerOpen = "actual" // one image pixel per screen pixel
)

// ViewerWheel is what the mouse wheel does over a photo.
type ViewerWheel string

// The wheel behaviours. With ViewerWheelNavigate, Ctrl and the wheel zoom.
const (
	ViewerWheelZoom     ViewerWheel = "zoom"
	ViewerWheelNavigate ViewerWheel = "navigate"
)

// The sizes a viewing copy (see MediaCaps.Render) may be asked for, by its
// longest edge. frontend/src/bridge mirrors them.
const (
	RenderSideStandard = 2048
	RenderSideHigh     = 4096 // media.RenderSide
)

// The bounds of what an export preset may hold. They are those an export
// itself is held to (media.RenderSideMin, media.RenderSide);
// services/contract_test.go checks.
const (
	ExportSideMin        = 256
	ExportSideMax        = 4096
	ExportQualityMin     = 1
	ExportQualityMax     = 100
	ExportQualityDefault = 92
	MaxExportPresets     = 12
)

// MaxShortcuts is how many shortcuts the user may have reassigned.
const MaxShortcuts = 64

// maxSubtitleLanguage bounds the preferred subtitle language, a tag such as
// "en" or "pt-BR".
const maxSubtitleLanguage = 16

// ExportPreset is a named set of export choices the user saved. It holds no
// destination: where files go is chosen at each export.
type ExportPreset struct {
	Name    string       `json:"name"`
	Format  ExportFormat `json:"format"`
	Side    int          `json:"side"`    // ExportSideMin..ExportSideMax, the longest edge of a rendered copy
	Quality int          `json:"quality"` // ExportQualityMin..ExportQualityMax, for JPEG
	Adjust  bool         `json:"adjust"`  // bake the display rotation and flip into a rendered copy
}

// ValidMotion reports whether m is one of the motion choices.
func ValidMotion(m Motion) bool { return m == MotionSystem || m == MotionReduced || m == MotionFull }

// ValidViewerBackground reports whether b is one of the viewer backgrounds.
func ValidViewerBackground(b ViewerBackground) bool {
	return b == ViewerBackgroundDark || b == ViewerBackgroundBlack || b == ViewerBackgroundGrey
}

// StartViews lists the start views, in the order the setting offers them.
func StartViews() []StartView {
	return []StartView{StartLibrary, StartFavourites, StartVideos, StartPeople, StartThings, StartPlaces,
		StartEvents, StartAlbums}
}

// ValidStartView reports whether v is one of the start views.
func ValidStartView(v StartView) bool { return slices.Contains(StartViews(), v) }

// ValidThumbFit reports whether f is one of the thumbnail fits.
func ValidThumbFit(f ThumbFit) bool { return f == ThumbCover || f == ThumbContain }

// ValidFocusRing reports whether f is one of the focus rings.
func ValidFocusRing(f FocusRing) bool { return f == FocusStandard || f == FocusStrong }

// ValidExportFormat reports whether f is a format an export can write.
func ValidExportFormat(f ExportFormat) bool {
	return f == ExportOriginal || f == ExportJPEG || f == ExportPNG
}

// A shortcut is written as the UI shows it: "F", "Ctrl+K", "Shift+Delete".
// Modifiers come in the order Ctrl, Shift, Alt; the key is a letter or digit,
// a named key ("ArrowLeft", "F2") or one other character. "+" cannot be the
// key, since it joins the parts.
var (
	shortcutCommand = regexp.MustCompile(`^[a-z][A-Za-z]*(\.[A-Za-z]+)+$`)
	shortcutKeys    = regexp.MustCompile(`^(Ctrl\+)?(Shift\+)?(Alt\+)?([A-Za-z0-9]{1,12}|[^\sA-Za-z0-9+])$`)
)

// reservedKeys are never assigned: they leave a dialog, the viewer or a
// field, whatever else is set.
var reservedKeys = []string{"escape", "tab"}

// ValidShortcut reports whether keys may be assigned to a command. "" is
// valid: it leaves the command without a shortcut.
func ValidShortcut(keys string) bool {
	if keys == "" {
		return true
	}
	if !shortcutKeys.MatchString(keys) {
		return false
	}
	key := strings.ToLower(keys[strings.LastIndex(keys, "+")+1:])
	return !slices.Contains(reservedKeys, key) && !strings.EqualFold(keys, "Alt+F4")
}

// normalisedShortcuts is the reassigned shortcuts that can be applied: each
// names a command and holds a valid shortcut, MaxShortcuts at most. Nil when
// there are none.
func normalisedShortcuts(in map[string]string) map[string]string {
	ids := make([]string, 0, len(in))
	for id, keys := range in {
		if id != "viewer.close" && len(id) <= 48 && shortcutCommand.MatchString(id) && ValidShortcut(keys) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	slices.Sort(ids) // which survive the bound must not depend on map order
	ids = ids[:min(len(ids), MaxShortcuts)]
	out := make(map[string]string, len(ids))
	for _, id := range ids {
		out[id] = in[id]
	}
	return out
}

// normalisedExportPresets is the presets an export can use: named, with a
// known format and values in range, one per name, MaxExportPresets at most.
// Nil when there are none.
func normalisedExportPresets(in []ExportPreset) []ExportPreset {
	var out []ExportPreset
	seen := map[string]bool{}
	for _, p := range in {
		p.Name = strings.TrimSpace(p.Name)
		key := Fold(p.Name)
		if p.Name == "" || utf8.RuneCountInString(p.Name) > MaxNameLen || seen[key] || !ValidExportFormat(p.Format) {
			continue
		}
		if len(out) == MaxExportPresets {
			break
		}
		seen[key] = true
		p.Side = min(max(p.Side, ExportSideMin), ExportSideMax)
		p.Quality = min(max(p.Quality, ExportQualityMin), ExportQualityMax)
		out = append(out, p)
	}
	return out
}

// normalisedSubtitleLanguage is a language tag held to letters, digits and
// hyphens, lower case; "" if it is anything else.
func normalisedSubtitleLanguage(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) > maxSubtitleLanguage {
		return ""
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return ""
		}
	}
	return s
}

// PrefCategory is a group of settings the user can put back to their
// defaults together. They are the sections the settings screen shows.
type PrefCategory string

// The categories. The first five hold interface preferences (Preferences)
// only; PrefIndexing and PrefSearch hold engine settings (Settings) only;
// PrefFiles holds one of each.
const (
	PrefAppearance    PrefCategory = "appearance"    // theme, accent, density, text size, motion, viewer background
	PrefLibrary       PrefCategory = "library"       // start view, thumbnails, badges, panels
	PrefViewing       PrefCategory = "viewing"       // how photos open, zoom, slideshow, viewing copies
	PrefVideo         PrefCategory = "video"         // playback
	PrefAccessibility PrefCategory = "accessibility" // focus ring, shortcuts
	PrefIndexing      PrefCategory = "indexing"      // what indexing waits for, threads, cache cap, formats
	PrefSearch        PrefCategory = "search"        // working on battery, tag confidence
	PrefFiles         PrefCategory = "files"         // event gap, export presets
)

// PrefCategories lists every category, in the order the settings show them.
func PrefCategories() []PrefCategory {
	return []PrefCategory{PrefAppearance, PrefLibrary, PrefViewing, PrefVideo, PrefAccessibility, PrefIndexing,
		PrefSearch, PrefFiles}
}

// Reset is p with one category's interface preferences put back to their
// defaults. A category of engine settings alone leaves p as it is.
func (p Preferences) Reset(c PrefCategory) Preferences {
	def := DefaultPreferences()
	viewer, defViewer := DefaultViewerPreferences(), DefaultViewerPreferences()
	if p.Viewer != nil {
		viewer = *p.Viewer
	}
	switch c {
	case PrefAppearance:
		p.Theme, p.Accent, p.Density, p.TextScale = def.Theme, def.Accent, def.Density, def.TextScale
		p.Motion, p.ViewerBackground = def.Motion, def.ViewerBackground
	case PrefLibrary:
		p.StartView, p.TileSize, p.ThumbFit = def.StartView, def.TileSize, def.ThumbFit
		p.HideFavouriteBadge, p.HideVideoBadge, p.HideDurationBadge = false, false, false
		p.SidebarCollapsed, p.SidebarWidth, p.InspectorWidth = def.SidebarCollapsed, def.SidebarWidth, def.InspectorWidth
	case PrefViewing:
		viewer.Filmstrip, viewer.AutoHide = defViewer.Filmstrip, defViewer.AutoHide
		viewer.SlideshowSeconds, viewer.Loop = defViewer.SlideshowSeconds, defViewer.Loop
		viewer.Open, viewer.Wheel = defViewer.Open, defViewer.Wheel
		viewer.StillAnimations, viewer.RenderSide = defViewer.StillAnimations, defViewer.RenderSide
	case PrefVideo:
		viewer.Autoplay, viewer.Volume, viewer.Muted = defViewer.Autoplay, defViewer.Volume, defViewer.Muted
		viewer.Speed, viewer.SkipSeconds, viewer.Resume = defViewer.Speed, defViewer.SkipSeconds, defViewer.Resume
		viewer.VideoLoop, viewer.Subtitles = defViewer.VideoLoop, defViewer.Subtitles
		viewer.SubtitleLanguage = defViewer.SubtitleLanguage
	case PrefAccessibility:
		p.FocusRing, p.Shortcuts = def.FocusRing, nil
	case PrefFiles:
		p.ExportPresets = nil
	case PrefIndexing, PrefSearch:
		return p // engine settings: not part of the preferences
	}
	p.Viewer = &viewer
	return p
}

// PreferenceChanges names the preferences that differ between two sets, by
// their JSON names ("theme", "viewer.speed"), in the order they are declared.
func PreferenceChanges(from, to Preferences) []string {
	from, to = from.Normalised(), to.Normalised()
	var out []string
	a, b := reflect.ValueOf(from), reflect.ValueOf(to)
	for i := range a.NumField() {
		name := jsonName(a.Type().Field(i))
		if name == "viewer" {
			va, vb := reflect.ValueOf(*from.Viewer), reflect.ValueOf(*to.Viewer)
			for j := range va.NumField() {
				if !reflect.DeepEqual(va.Field(j).Interface(), vb.Field(j).Interface()) {
					out = append(out, "viewer."+jsonName(va.Type().Field(j)))
				}
			}
			continue
		}
		if !reflect.DeepEqual(a.Field(i).Interface(), b.Field(i).Interface()) {
			out = append(out, name)
		}
	}
	return out
}

func jsonName(f reflect.StructField) string {
	name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
	return name
}

// CustomisationPreview is a customisation file read and checked, before
// anything is applied: the preferences it holds, held to what the UI can
// apply, and which of them differ from the library's.
type CustomisationPreview struct {
	// Name is the file's name, without its folder; "" if the user chose none.
	Name        string      `json:"name"`
	Preferences Preferences `json:"preferences"`
	Changed     []string    `json:"changed"` // see PreferenceChanges
}

// ExcludableFamilies lists the photo formats indexing can be told to leave
// out, in the order the setting offers them. Videos have their own setting.
func ExcludableFamilies() []Family {
	return []Family{FamilyJPEG, FamilyPNG, FamilyGIF, FamilyWebP, FamilyBMP, FamilyTIFF, FamilyHEIF, FamilyAVIF,
		FamilyJXL, FamilyRAW}
}

// NormalisedFamilies is the excludable families among list, each once, in
// the order ExcludableFamilies gives them. Nil when there are none.
func NormalisedFamilies(list []Family) []Family {
	var out []Family
	for _, f := range ExcludableFamilies() {
		if slices.Contains(list, f) {
			out = append(out, f)
		}
	}
	return out
}
