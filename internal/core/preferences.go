package core

// Theme is which colours the UI uses.
type Theme string

// The themes. ThemeSystem follows Windows' light or dark setting.
const (
	ThemeSystem Theme = "system"
	ThemeLight  Theme = "light"
	ThemeDark   Theme = "dark"
)

// Accent is the UI's one accent colour, from the curated set.
type Accent string

// The accents. Status colours (danger, success) keep their meaning under
// every one of them.
const (
	AccentBlue   Accent = "blue"
	AccentTeal   Accent = "teal"
	AccentGreen  Accent = "green"
	AccentViolet Accent = "violet"
	AccentRose   Accent = "rose"
	AccentAmber  Accent = "amber"
)

// Density is how much room controls and rows take.
type Density string

// The densities.
const (
	DensityComfortable Density = "comfortable"
	DensityCompact     Density = "compact"
)

// The ranges and defaults of the numeric preferences.
// frontend/src/bridge mirrors them.
const (
	TextScaleMin     = 90 // percent of the UI's base text size
	TextScaleMax     = 130
	TextScaleDefault = 100

	TileSizeMin     = 96 // the grid's target thumbnail edge, CSS px
	TileSizeMax     = 320
	TileSizeDefault = 168

	SidebarWidthMin     = 200 // CSS px
	SidebarWidthMax     = 360
	SidebarWidthDefault = 232

	InspectorWidthMin     = 280 // CSS px
	InspectorWidthMax     = 480
	InspectorWidthDefault = 320
)

// Settings keys of the interface preferences (see Preferences).
const (
	SettingUITheme            = "ui_theme"
	SettingUIAccent           = "ui_accent"
	SettingUIDensity          = "ui_density"
	SettingUITextScale        = "ui_text_scale"
	SettingUITileSize         = "ui_tile_size"
	SettingUISidebarCollapsed = "ui_sidebar_collapsed"
	SettingUISidebarWidth     = "ui_sidebar_width"
	SettingUIInspectorWidth   = "ui_inspector_width"
	SettingUIViewer           = "ui_viewer"
	SettingUIMotion           = "ui_motion"
	SettingUIViewerBackground = "ui_viewer_background"
	SettingUIStartView        = "ui_start_view"
	SettingUIThumbFit         = "ui_thumb_fit"
	SettingUIFocusRing        = "ui_focus_ring"
	SettingUIHideFavourite    = "ui_hide_favourite_badge"
	SettingUIHideVideo        = "ui_hide_video_badge"
	SettingUIHideDuration     = "ui_hide_duration_badge"
	SettingUIShortcuts        = "ui_shortcuts"      // JSON: command id to shortcut
	SettingUIExportPresets    = "ui_export_presets" // JSON: []ExportPreset
	// SettingUILegacyMigrated marks that what an older Lumen kept in the
	// WebView's storage was offered to this library once (bool).
	SettingUILegacyMigrated = "ui_legacy_migrated"
)

// Preferences are how the user likes the interface to look. The library's
// settings are their one durable owner: the UI keeps only a copy for
// painting its first frame.
type Preferences struct {
	Viewer           *ViewerPreferences `json:"viewer,omitempty"`
	Theme            Theme              `json:"theme"`
	Accent           Accent             `json:"accent"`
	Density          Density            `json:"density"`
	TextScale        int                `json:"textScale"` // TextScaleMin..TextScaleMax, percent
	TileSize         int                `json:"tileSize"`  // TileSizeMin..TileSizeMax
	SidebarCollapsed bool               `json:"sidebarCollapsed"`
	SidebarWidth     int                `json:"sidebarWidth"`   // SidebarWidthMin..SidebarWidthMax
	InspectorWidth   int                `json:"inspectorWidth"` // InspectorWidthMin..InspectorWidthMax
	Motion           Motion             `json:"motion"`
	ViewerBackground ViewerBackground   `json:"viewerBackground"`
	StartView        StartView          `json:"startView"`
	ThumbFit         ThumbFit           `json:"thumbFit"`
	FocusRing        FocusRing          `json:"focusRing"`
	// The badges a thumbnail carries are shown unless hidden here.
	HideFavouriteBadge bool `json:"hideFavouriteBadge"`
	HideVideoBadge     bool `json:"hideVideoBadge"`
	HideDurationBadge  bool `json:"hideDurationBadge"`
	// Shortcuts holds the shortcuts the user reassigned, by command id; ""
	// leaves a command without one. A command not listed keeps its own.
	Shortcuts     map[string]string `json:"shortcuts"`
	ExportPresets []ExportPreset    `json:"exportPresets"`
}

// DefaultPreferences is what a library starts with.
func DefaultPreferences() Preferences {
	v := DefaultViewerPreferences()
	return Preferences{
		Viewer:         &v,
		Theme:          ThemeSystem,
		Accent:         AccentBlue,
		Density:        DensityComfortable,
		TextScale:      TextScaleDefault,
		TileSize:       TileSizeDefault,
		SidebarWidth:   SidebarWidthDefault,
		InspectorWidth: InspectorWidthDefault,
		Motion:         MotionSystem, ViewerBackground: ViewerBackgroundDark, StartView: StartLibrary,
		ThumbFit: ThumbCover, FocusRing: FocusStandard,
	}
}

// ValidTheme reports whether t is one of the themes.
func ValidTheme(t Theme) bool { return t == ThemeSystem || t == ThemeLight || t == ThemeDark }

// ValidAccent reports whether a is one of the curated accents.
func ValidAccent(a Accent) bool {
	switch a {
	case AccentBlue, AccentTeal, AccentGreen, AccentViolet, AccentRose, AccentAmber:
		return true
	}
	return false
}

// ValidDensity reports whether d is one of the densities.
func ValidDensity(d Density) bool { return d == DensityComfortable || d == DensityCompact }

// Normalised is p with every value made one the UI can apply: a choice that
// is not offered becomes the default, a number outside its range the nearest
// end. A stored value from a newer or older Lumen therefore never breaks the
// interface.
func (p Preferences) Normalised() Preferences {
	def := DefaultPreferences()
	if p.Viewer == nil {
		p.Viewer = def.Viewer
	} else {
		v := p.Viewer.Normalised()
		p.Viewer = &v
	}
	if !ValidTheme(p.Theme) {
		p.Theme = def.Theme
	}
	if !ValidAccent(p.Accent) {
		p.Accent = def.Accent
	}
	if !ValidDensity(p.Density) {
		p.Density = def.Density
	}
	p.TextScale = min(max(p.TextScale, TextScaleMin), TextScaleMax)
	p.TileSize = min(max(p.TileSize, TileSizeMin), TileSizeMax)
	p.SidebarWidth = min(max(p.SidebarWidth, SidebarWidthMin), SidebarWidthMax)
	p.InspectorWidth = min(max(p.InspectorWidth, InspectorWidthMin), InspectorWidthMax)
	if !ValidMotion(p.Motion) {
		p.Motion = def.Motion
	}
	if !ValidViewerBackground(p.ViewerBackground) {
		p.ViewerBackground = def.ViewerBackground
	}
	if !ValidStartView(p.StartView) {
		p.StartView = def.StartView
	}
	if !ValidThumbFit(p.ThumbFit) {
		p.ThumbFit = def.ThumbFit
	}
	if !ValidFocusRing(p.FocusRing) {
		p.FocusRing = def.FocusRing
	}
	p.Shortcuts = normalisedShortcuts(p.Shortcuts)
	p.ExportPresets = normalisedExportPresets(p.ExportPresets)
	return p
}

// LegacyPreferences are the choices an older Lumen kept in the WebView's own
// storage ("lumen.theme", "lumen.zoom"). The zero value of each means it was
// never chosen.
type LegacyPreferences struct {
	Theme    Theme `json:"theme"`
	TileSize int   `json:"tileSize"`
}

// MediaCaps says what Lumen can do with one asset, as file inspection and
// the available decoder, viewer and playback paths have shown rather than as
// the file's extension suggests. Screens offer an action from these, so a
// format gains a capability in one place. A false field means "not offered
// now", not "never": why (pending, unsupported, failed) stays with the job
// that would make it true.
type MediaCaps struct {
	// Preview: a still preview of the current file has been made.
	Preview bool `json:"preview"`
	// Original: the WebView shows the original photo as it is, through the
	// original endpoint. A native decoder for the format is not enough.
	Original bool `json:"original"`
	// Render: the runtime can prepare a bounded upright sRGB copy. The service
	// supplies this capability from its renderer and known source dimensions;
	// RenderInfo reports whether the available pixels were reduced.
	Render bool `json:"render"`
	// Paged: the file's format can hold several pages (TIFF), which the
	// viewer turns through; how many this file holds is in its inspection.
	Paged bool `json:"paged"`
	// DirectPlay: the WebView may play the video file itself.
	DirectPlay bool `json:"directPlay"`
	// ConvertPlay: Lumen can convert the video while it plays.
	ConvertPlay bool `json:"convertPlay"`
}

// CapsOf is what can be done with an asset of this kind and extension;
// hasPreview says whether its thumbnail exists, and play how its video plays
// ("" for a photo).
func CapsOf(kind Kind, ext string, hasPreview bool, play Playback) MediaCaps {
	c := MediaCaps{Preview: hasPreview}
	switch kind {
	case KindPhoto:
		c.Original = WebNative(ext)
		c.Paged = FamilyOf(ext) == FamilyTIFF
	case KindVideo:
		// PlayHEVC plays directly where the WebView decodes HEVC; that holds
		// until the player has tried. Any video can be converted as it plays,
		// which is also where a direct play that fails goes.
		c.DirectPlay = play == PlayDirect || play == PlayHEVC
		c.ConvertPlay = true
	}
	return c
}
