package core

import (
	"path/filepath"
	"strings"
	"time"
)

// Kind is what sort of media an asset is.
type Kind string

const (
	KindPhoto Kind = "photo"
	KindVideo Kind = "video"
)

// Stage names one step of the indexing pipeline. Every asset has one
// asset_jobs row per stage, so a stage can be re-run on its own later.
type Stage string

const (
	StageMetadata  Stage = "metadata"
	StageThumbnail Stage = "thumbnail" // also produces the perceptual hash
	StageEmbedding Stage = "embedding" // SigLIP image embedding, plus AI tags
)

// Stages lists every stage in pipeline order. A new asset gets a job for each.
var Stages = []Stage{StageMetadata, StageThumbnail, StageEmbedding}

// JobStatus is the state of one stage for one asset.
type JobStatus string

const (
	JobPending JobStatus = "pending"
	JobDone    JobStatus = "done"
	JobError   JobStatus = "error"
	// JobSkipped means this machine can't process the file (e.g. a video
	// before video support, or HEIC without a decoder). Not a failure: it
	// re-runs when the stage's version changes.
	JobSkipped JobStatus = "skipped"
)

// Settings keys.
const (
	SettingVideos        = "videos_enabled"     // bool: index videos too (off by default)
	SettingAIOnBattery   = "ai_on_battery"      // bool: keep AI indexing going on battery (off by default)
	SettingTagConfidence = "tag_confidence"     // float: minimum AI probability for a tag to apply
	SettingVocabulary    = "vocabulary_version" // int: built-in Things last synced
)

// Settings are the user-facing preferences.
type Settings struct {
	Videos        bool    `json:"videos"`
	AIOnBattery   bool    `json:"aiOnBattery"`
	TagConfidence float64 `json:"tagConfidence"` // TagConfidenceMin..Max
}

// Tag confidence: the default, and the range the setting may take. The
// lower bound is also the lowest score stored, so the slider never needs a
// re-scan.
const (
	TagConfidenceDefault = 0.02
	TagConfidenceMin     = 0.002
	TagConfidenceMax     = 0.3
)

// DateSource says where an asset's capture date came from, best first.
type DateSource string

const (
	DateFromEXIF     DateSource = "exif"
	DateFromFilename DateSource = "filename"
	DateFromFile     DateSource = "file" // the file's modified time
)

// Thumbnail sizes (longest edge, in pixels). frontend/src/bridge mirrors them.
const (
	ThumbSmall = 256  // grid
	ThumbLarge = 1024 // viewer preview
)

// Source is a folder the user asked Lumen to watch.
type Source struct {
	ID      int64  `json:"id"`
	Path    string `json:"path"`
	Enabled bool   `json:"enabled"`
}

// GridItem is the minimum the grid needs to lay out and draw one cell.
// Field names are short on purpose: a big library sends many of these.
type GridItem struct {
	ID        int64 `json:"id"`
	W         int   `json:"w"`
	H         int   `json:"h"`
	Video     bool  `json:"v,omitempty"`
	Favourite bool  `json:"f,omitempty"`
	Thumb     int   `json:"t"` // thumbnail version; 0 = not made yet
}

// Filter narrows the grid to a subset of the library.
type Filter string

const (
	FilterAll        Filter = ""
	FilterFavourites Filter = "favourites"
	FilterVideos     Filter = "videos"
)

// View is what the timeline grid shows: a filter, optionally narrowed to
// one tag.
type View struct {
	Filter Filter `json:"filter"`
	Tag    int64  `json:"tag,omitempty"` // 0 = any
}

// TagKind says where a tag comes from.
type TagKind string

const (
	TagBuiltin TagKind = "builtin" // Lumen's vocabulary; the AI applies it
	TagLabel   TagKind = "label"   // made by the user; the AI applies it
	TagManual  TagKind = "manual"  // made by the user; only the user applies it
)

// TagSource says who put a tag on an asset.
type TagSource string

const (
	TagByAI     TagSource = "ai"
	TagByUser   TagSource = "user"
	TagRejected TagSource = "rejected" // the user removed an AI tag; the AI never re-adds it
)

// Tag is one Thing, with how many visible assets carry it and one to show.
type Tag struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	Kind       TagKind `json:"kind"`
	Hidden     bool    `json:"hidden"`
	Count      int     `json:"count"`
	Cover      int64   `json:"cover"`      // asset id; 0 = none
	CoverThumb int     `json:"coverThumb"` // its thumbnail version
}

// AssetTag is a tag on one asset.
type AssetTag struct {
	ID     int64     `json:"id"`
	Name   string    `json:"name"`
	Kind   TagKind   `json:"kind"`
	Source TagSource `json:"source"`
	Score  float32   `json:"score"` // AI probability; 0 for user tags
}

// SearchResult is the answer to a natural-language search, best match first.
type SearchResult struct {
	Items    []GridItem `json:"items"`
	Pending  int        `json:"pending"`  // photos not yet understood, so not searchable yet
	Complete bool       `json:"complete"` // false: more matched than are shown
}

// DayBucket is how many visible assets were taken on one calendar day.
// The grid builds its whole layout from these before loading any items.
type DayBucket struct {
	Day   string `json:"day"` // YYYY-MM-DD in the photo's wall-clock time
	Count int    `json:"count"`
}

// DayItems is one day's worth of grid items, newest first.
type DayItems struct {
	Day   string     `json:"day"`
	Items []GridItem `json:"items"`
}

// AssetDetail is everything the details panel shows.
type AssetDetail struct {
	ID        int64      `json:"id"`
	Kind      Kind       `json:"kind"`
	TakenAt   int64      `json:"takenAt"` // wall-clock seconds, see WallClock
	DateFrom  DateSource `json:"dateFrom"`
	Width     int        `json:"width"`
	Height    int        `json:"height"`
	Camera    string     `json:"camera"`
	Lat       *float64   `json:"lat"`
	Lon       *float64   `json:"lon"`
	Favourite bool       `json:"favourite"`
	Paths     []string   `json:"paths"`
	Size      int64      `json:"size"`
	Ext       string     `json:"ext"`
	WebNative bool       `json:"webNative"` // the WebView can show the original directly
}

// Progress is the payload of the index:progress event.
type Progress struct {
	Scanning   bool `json:"scanning"`
	Discovered int  `json:"discovered"` // files found in this scan so far
	Total      int  `json:"total"`      // assets in the library
	Metadata   int  `json:"metadata"`   // assets still waiting for metadata
	Thumbnail  int  `json:"thumbnail"`  // assets still waiting for thumbnails
	Embedding  int  `json:"embedding"`  // assets still waiting for the AI
	Errors     int  `json:"errors"`
	Skipped    int  `json:"skipped"`
	Paused     bool `json:"paused"`
	OnBattery  bool `json:"onBattery"` // AI work is waiting for mains power
	AI         AI   `json:"ai"`
}

// AI is the state of the on-device models.
type AI struct {
	Device string `json:"device"` // "gpu" or "cpu" while the image model is loaded, else ""
	Error  string `json:"error"`  // why the AI can't run (models missing, runtime failed)
}

// WallClock converts t to "wall-clock seconds": the local date and time as it
// read on the camera's clock, encoded as if it were UTC. EXIF dates carry no
// time zone, so storing every date this way keeps a photo taken at 23:30 on
// the 14th on the 14th, whatever zone the user is in now.
func WallClock(t time.Time) int64 {
	y, mo, d := t.Date()
	h, mi, s := t.Clock()
	return time.Date(y, mo, d, h, mi, s, 0, time.UTC).Unix()
}

// Media file extensions Lumen recognises (lower case, with the dot).
var (
	photoExts = map[string]bool{
		".jpg": true, ".jpeg": true, ".jpe": true, ".png": true, ".gif": true,
		".webp": true, ".bmp": true, ".tif": true, ".tiff": true,
		".heic": true, ".heif": true, ".avif": true,
	}
	videoExts = map[string]bool{
		".mp4": true, ".m4v": true, ".mov": true, ".avi": true, ".mkv": true,
		".webm": true, ".3gp": true, ".wmv": true, ".mts": true, ".m2ts": true,
	}
	// Formats WebView2 renders natively, so the viewer can show the original.
	webNativeExts = map[string]bool{
		".jpg": true, ".jpeg": true, ".jpe": true, ".png": true, ".gif": true,
		".webp": true, ".bmp": true, ".avif": true, ".mp4": true, ".webm": true,
		".m4v": true,
	}
)

// KindOf returns the media kind for a path, or "" if Lumen ignores it.
func KindOf(path string) Kind {
	ext := strings.ToLower(filepath.Ext(path))
	switch {
	case photoExts[ext]:
		return KindPhoto
	case videoExts[ext]:
		return KindVideo
	}
	return ""
}

// WebNative reports whether the WebView can display a file of this extension.
func WebNative(ext string) bool { return webNativeExts[strings.ToLower(ext)] }
