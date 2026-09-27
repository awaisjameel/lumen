package core

import (
	"path/filepath"
	"strings"
	"time"
)

// Kind is what sort of media an asset is.
type Kind string

// The kinds of media.
const (
	KindPhoto Kind = "photo"
	KindVideo Kind = "video"
)

// Stage names one step of the indexing pipeline. Every asset has one
// asset_jobs row per stage, so a stage can be re-run on its own later.
type Stage string

// The stages, in pipeline order.
const (
	StageMetadata  Stage = "metadata"
	StageThumbnail Stage = "thumbnail" // also produces the perceptual hash
	StageEmbedding Stage = "embedding" // SigLIP image embedding, plus AI tags
	StageFaces     Stage = "faces"     // face detection and identity vectors
)

// Stages lists every stage in pipeline order. A new asset gets a job for each.
var Stages = []Stage{StageMetadata, StageThumbnail, StageEmbedding, StageFaces}

// AI reports whether a stage runs the AI models: heavy work that waits for
// mains power unless the user allows it on battery.
func (s Stage) AI() bool { return s == StageEmbedding || s == StageFaces }

// JobStatus is the state of one stage for one asset.
type JobStatus string

// The states a job can be in.
const (
	JobPending JobStatus = "pending"
	JobDone    JobStatus = "done"
	JobError   JobStatus = "error"
	// JobSkipped means this machine can't process the file (e.g. HEIC
	// without a decoder, or a "video" with no picture). Not a failure: it
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

// Where a date can come from.
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

// A video's hover preview (its "sprite") is one JPEG of SpriteCols x
// SpriteRows frames spread evenly over its length, left to right then top to
// bottom, each at most ThumbSmall on its long edge. frontend/src/bridge
// mirrors it.
const (
	SpriteCols = 4
	SpriteRows = 4
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
	Duration  int64 `json:"d,omitempty"` // a video's length, ms
	Favourite bool  `json:"f,omitempty"`
	Thumb     int   `json:"t"`            // thumbnail version; 0 = not made yet
	At        int64 `json:"at,omitempty"` // search results: the best-matching moment of a video, ms
}

// Filter narrows the grid to a subset of the library.
type Filter string

// The grid filters.
const (
	FilterAll        Filter = ""
	FilterFavourites Filter = "favourites"
	FilterVideos     Filter = "videos"
)

// View is what the timeline grid shows: a filter, optionally narrowed to
// one tag or one person.
type View struct {
	Filter Filter `json:"filter"`
	Tag    int64  `json:"tag,omitempty"`    // 0 = any
	Person int64  `json:"person,omitempty"` // 0 = anyone
}

// TagKind says where a tag comes from.
type TagKind string

// The kinds of tag.
const (
	TagBuiltin TagKind = "builtin" // Lumen's vocabulary; the AI applies it
	TagLabel   TagKind = "label"   // made by the user; the AI applies it
	TagManual  TagKind = "manual"  // made by the user; only the user applies it
)

// TagSource says who put a tag on an asset.
type TagSource string

// Who can put a tag on an asset.
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
	People   []string   `json:"people"`   // people named in the query: results show all of them
	Pending  int        `json:"pending"`  // photos not yet understood, so not searchable yet
	Complete bool       `json:"complete"` // false: more matched than are shown
}

// Face is one face in a photo or video: where it is, and who it is.
type Face struct {
	ID      int64   `json:"id"`
	AssetID int64   `json:"assetId"`
	Thumb   int     `json:"thumb"` // the photo's thumbnail version
	AssetW  int     `json:"assetW"`
	AssetH  int     `json:"assetH"`
	Video   bool    `json:"video"` // the face is in a video's frame, not its thumbnail
	Frame   int64   `json:"frame"` // which frame of a video, ms; 0 for photos
	X       float32 `json:"x"`     // the box, as fractions of the upright photo or frame
	Y       float32 `json:"y"`
	W       float32 `json:"w"`
	H       float32 `json:"h"`
	Person  int64   `json:"person"` // 0 = not recognised yet
	Name    string  `json:"name"`   // the person's name; "" = unnamed or nobody
	ByUser  bool    `json:"byUser"` // the user put this face with this person
}

// Person is someone who appears in the photos: a group of faces the AI
// found, or the user made, named or not.
type Person struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"` // "" until the user names them
	Hidden bool   `json:"hidden"`
	Count  int    `json:"count"` // visible photos they appear in
	Cover  Face   `json:"cover"` // the face to show; ID 0 = none
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
	WebNative bool       `json:"webNative"` // a photo the WebView can show directly
	Video     *Video     `json:"video"`     // nil for photos
}

// Video is what the details panel and the player need to know about a video.
type Video struct {
	Duration int64    `json:"duration"` // ms
	Codec    string   `json:"codec"`    // FFmpeg's name for the picture's codec, e.g. "h264", "hevc"
	Audio    string   `json:"audio"`    // the sound's codec; "" = silent
	Play     Playback `json:"play"`
}

// Playback says how the viewer plays a video.
type Playback string

// The ways a video can play.
const (
	PlayDirect  Playback = "direct"  // the WebView plays the file itself
	PlayHEVC    Playback = "hevc"    // directly where the WebView decodes HEVC, else as PlayConvert
	PlayConvert Playback = "convert" // Lumen converts it to H.264 while it plays
)

// PlaybackOf decides how a video with this extension and these codecs (FFmpeg
// names; audio "" = none) plays: directly when the WebView supports the
// container and both codecs, else converted.
func PlaybackOf(ext, video, audio string) Playback {
	switch strings.ToLower(ext) {
	case ".mp4", ".m4v", ".mov":
		if !mp4Audio[audio] {
			return PlayConvert
		}
		switch video {
		case "h264", "vp9", "av1":
			return PlayDirect
		case "hevc":
			return PlayHEVC
		}
	case ".webm":
		if webmVideo[video] && webmAudio[audio] {
			return PlayDirect
		}
	}
	return PlayConvert
}

// Codecs WebView2 plays in each container (Chromium's lists).
var (
	mp4Audio  = map[string]bool{"": true, "aac": true, "mp3": true, "opus": true, "flac": true}
	webmVideo = map[string]bool{"vp8": true, "vp9": true, "av1": true}
	webmAudio = map[string]bool{"": true, "opus": true, "vorbis": true}
)

// Progress is the payload of the index:progress event.
type Progress struct {
	Scanning   bool `json:"scanning"`
	Discovered int  `json:"discovered"` // files found in this scan so far
	Total      int  `json:"total"`      // assets in the library
	Metadata   int  `json:"metadata"`   // assets still waiting for metadata
	Thumbnail  int  `json:"thumbnail"`  // assets still waiting for thumbnails
	Embedding  int  `json:"embedding"`  // assets still waiting for the AI
	Faces      int  `json:"faces"`      // assets still waiting for face recognition
	Errors     int  `json:"errors"`
	Skipped    int  `json:"skipped"`
	Paused     bool `json:"paused"`
	OnBattery  bool `json:"onBattery"` // AI work is waiting for mains power
	AI         AI   `json:"ai"`
}

// AI is the state of the on-device models.
type AI struct {
	Device     string `json:"device"`     // "gpu" or "cpu" while the image model is loaded, else ""
	Error      string `json:"error"`      // why search and Things can't run (models missing, runtime failed)
	FacesError string `json:"facesError"` // why people can't be recognised
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
	// Photo formats WebView2 renders natively, so the viewer can show the
	// original. (Videos: see PlaybackOf.)
	webNativeExts = map[string]bool{
		".jpg": true, ".jpeg": true, ".jpe": true, ".png": true, ".gif": true,
		".webp": true, ".bmp": true, ".avif": true,
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

// WebNative reports whether the WebView can display a photo of this extension.
func WebNative(ext string) bool { return webNativeExts[strings.ToLower(ext)] }
