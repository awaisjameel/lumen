package core

import "math"

// RenderInfo describes the pixels actually available to a viewer. A bounded
// render must not claim to contain every source pixel when it was reduced.
type RenderInfo struct {
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	SourceWidth  int    `json:"sourceWidth"`
	SourceHeight int    `json:"sourceHeight"`
	Limited      bool   `json:"limited"`
	Revision     string `json:"revision"`
	Provenance   string `json:"provenance"`
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
	v.SlideshowSeconds = min(max(1, v.SlideshowSeconds), 60)
	v.Volume = min(max(0, v.Volume), 1)
	v.Speed = min(max(0.25, v.Speed), 2)
	v.SkipSeconds = min(max(1, v.SkipSeconds), 60)
	return v
}

// DisplayAdjustment changes presentation only, never source or AI pixels.
type DisplayAdjustment struct {
	Rotation int  `json:"rotation"` // clockwise quarter turns
	Flip     bool `json:"flip"`     // horizontal flip after rotation
	Version  int  `json:"version"`
}

// MediaInspection is read from the available file rather than inferred.
type MediaInspection struct {
	Orientation int          `json:"orientation"`
	Profile     string       `json:"profile"`
	Lens        string       `json:"lens"`
	Exposure    string       `json:"exposure"`
	Aperture    string       `json:"aperture"`
	ISO         string       `json:"iso"`
	FrameRate   string       `json:"frameRate"`
	Pages       int          `json:"pages"`
	Tracks      []MediaTrack `json:"tracks"`
}

// MediaTrack describes an actual audio or subtitle stream and its support.
type MediaTrack struct {
	Index     int    `json:"index"`
	Kind      string `json:"kind"`
	Codec     string `json:"codec"`
	Language  string `json:"language"`
	Title     string `json:"title"`
	Supported bool   `json:"supported"`
}

// ExportOptions describes exactly what the new file includes. Originals
// retain their metadata; rendered JPEG/PNG copies contain upright sRGB
// pixels without source metadata and never replace an existing file.
type ExportOptions struct {
	Format  string `json:"format"` // original, jpeg or png
	Side    int    `json:"side"`
	Quality int    `json:"quality"`
	Adjust  bool   `json:"adjust"`
	Frame   int64  `json:"frame"` // -1 for originals/photos; source time for a video frame
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
	State   string        `json:"state"`
	Error   string        `json:"error"`
	Temp    string        `json:"temp"`
	Digest  string        `json:"digest"`
	Options ExportOptions `json:"options"`
}
