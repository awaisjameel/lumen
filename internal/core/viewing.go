package core

import "math"

// RenderInfo describes the pixels actually available to a viewer. A bounded
// render must not claim to contain every source pixel when it was reduced.
type RenderInfo struct {
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Limited  bool   `json:"limited"` // fewer pixels than the source has
	Preview  bool   `json:"preview"` // a RAW file's embedded preview: its sensor data could not be developed
	Page     int    `json:"page"`    // which page of the file this is, from 0
	Revision string `json:"revision"`
}

// ViewerPreferences are saved with the library's interface preferences.
type ViewerPreferences struct {
	Filmstrip        bool    `json:"filmstrip"`
	AutoHide         bool    `json:"autoHide"`
	SlideshowSeconds int     `json:"slideshowSeconds"`
	Loop             bool    `json:"loop"`
	Autoplay         bool    `json:"autoplay"`
	Volume           float64 `json:"volume"`
	Muted            bool    `json:"muted"`
	Speed            float64 `json:"speed"`
	SkipSeconds      int     `json:"skipSeconds"`
	Resume           bool    `json:"resume"`
}

// The ranges of the viewer preferences that are numbers.
// frontend/src/bridge mirrors them; services/contract_test.go checks.
const (
	SlideshowSecondsMin = 1
	SlideshowSecondsMax = 60
	SkipSecondsMin      = 1
	SkipSecondsMax      = 60
	PlaybackSpeedMin    = 0.25
	PlaybackSpeedMax    = 2
)

// DefaultViewerPreferences supplies the initial viewing and playback choices.
func DefaultViewerPreferences() ViewerPreferences {
	return ViewerPreferences{SlideshowSeconds: 5, Autoplay: true, Volume: 1, Speed: 1, SkipSeconds: 10}
}

// Normalised bounds the values that controls and playback can apply.
func (v ViewerPreferences) Normalised() ViewerPreferences {
	if math.IsNaN(v.Volume) || math.IsInf(v.Volume, 0) {
		v.Volume = 1
	}
	if math.IsNaN(v.Speed) || math.IsInf(v.Speed, 0) {
		v.Speed = 1
	}
	v.SlideshowSeconds = min(max(SlideshowSecondsMin, v.SlideshowSeconds), SlideshowSecondsMax)
	v.Volume = min(max(0, v.Volume), 1)
	v.Speed = min(max(PlaybackSpeedMin, v.Speed), PlaybackSpeedMax)
	v.SkipSeconds = min(max(SkipSecondsMin, v.SkipSeconds), SkipSecondsMax)
	return v
}

// DisplayAdjustment changes presentation only, never source or AI pixels.
type DisplayAdjustment struct {
	Rotation int  `json:"rotation"` // clockwise quarter turns
	Flip     bool `json:"flip"`     // horizontal flip after rotation
}

// MediaInspection is read from the available file rather than inferred.
type MediaInspection struct {
	Orientation int          `json:"orientation"`
	HasProfile  bool         `json:"hasProfile"` // a colour profile is embedded
	Profile     string       `json:"profile"`    // what that profile calls itself; "" when it does not say
	Lens        string       `json:"lens"`
	Exposure    string       `json:"exposure"`
	Aperture    string       `json:"aperture"`
	ISO         string       `json:"iso"`
	FrameRate   string       `json:"frameRate"`
	Pages       int          `json:"pages"`
	Tracks      []MediaTrack `json:"tracks"`
}

// TrackKind is which of a video's selectable streams a track is.
type TrackKind string

// The kinds of track.
const (
	TrackAudio    TrackKind = "audio"
	TrackSubtitle TrackKind = "subtitle"
)

// MediaTrack describes an actual audio or subtitle stream and its support.
type MediaTrack struct {
	Index     int       `json:"index"`
	Kind      TrackKind `json:"kind"`
	Codec     string    `json:"codec"`
	Language  string    `json:"language"`
	Title     string    `json:"title"`
	Supported bool      `json:"supported"`
}

// ExportFormat is what an export writes.
type ExportFormat string

// The formats an export can write.
const (
	ExportOriginal ExportFormat = "original" // the file's own bytes, metadata included
	ExportJPEG     ExportFormat = "jpeg"
	ExportPNG      ExportFormat = "png"
)

// ExportState is where one file of an export stands in its journal.
type ExportState string

// The states of one exported file.
const (
	ExportPlanned   ExportState = "planned"
	ExportWriting   ExportState = "writing" // a temporary file beside the destination is being written
	ExportReady     ExportState = "ready"   // its digest is journaled; publication is next
	ExportDone      ExportState = "done"
	ExportFailed    ExportState = "failed"
	ExportCancelled ExportState = "cancelled"
)

// ExportOptions describes exactly what the new file includes. Originals
// retain their metadata; rendered JPEG/PNG copies contain upright sRGB
// pixels without source metadata and never replace an existing file.
type ExportOptions struct {
	Format  ExportFormat `json:"format"`
	Side    int          `json:"side"`
	Quality int          `json:"quality"`
	Adjust  bool         `json:"adjust"`
	Frame   int64        `json:"frame"` // -1 for originals/photos; source time for a video frame
}

// ExportJob is a durable per-file outcome; completed exports have no Undo.
type ExportJob struct {
	ID      int64         `json:"id"`
	Batch   int64         `json:"batch"`
	AssetID int64         `json:"assetId"`
	From    string        `json:"from"`
	To      string        `json:"to"`
	Size    int64         `json:"size"`
	MTime   int64         `json:"mtime"`
	State   ExportState   `json:"state"`
	Error   string        `json:"error"`
	Temp    string        `json:"temp"`
	Digest  string        `json:"digest"`
	Options ExportOptions `json:"options"`
}
