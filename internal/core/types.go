package core

import (
	"runtime"
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
	// StageOCR reads the text in screenshots and documents. It is an
	// optional add-on, off by default, so it is not in Stages: only the
	// photos it applies to get a job, and only while it is on.
	StageOCR Stage = "ocr"
)

// Stages lists every stage in pipeline order. A new asset gets a job for each.
var Stages = []Stage{StageMetadata, StageThumbnail, StageEmbedding, StageFaces}

// AI reports whether a stage runs the AI models: heavy work that waits for
// mains power unless the user allows it on battery.
func (s Stage) AI() bool { return s == StageEmbedding || s == StageFaces || s == StageOCR }

// JobStatus is the state of one stage for one asset.
type JobStatus string

// The states a job can be in.
const (
	JobPending JobStatus = "pending"
	// JobWaiting is a job held back until an earlier stage it needs is
	// done: still to do, but not yet runnable.
	JobWaiting JobStatus = "waiting"
	JobDone    JobStatus = "done"
	JobError   JobStatus = "error"
	// JobSkipped means this machine can't process the file (e.g. HEIC
	// without a decoder, or a "video" with no picture). Not a failure: it
	// re-runs when the stage's version changes.
	JobSkipped JobStatus = "skipped"
)

// MaxNameLen is the longest name (in characters) a person, Thing or album
// may have. frontend/src/bridge mirrors it.
const MaxNameLen = 80

// The Blocked screen's PIN is PINMinLen to PINMaxLen digits.
// frontend/src/bridge mirrors them.
const (
	PINMinLen = 6
	PINMaxLen = 12
)

// Settings keys.
const (
	SettingVideos        = "videos_enabled"     // bool: index videos too (off by default)
	SettingAIOnBattery   = "ai_on_battery"      // bool: keep AI indexing going on battery (off by default)
	SettingTagConfidence = "tag_confidence"     // float: minimum AI probability for a tag to apply
	SettingVocabulary    = "vocabulary_version" // int: built-in Things last synced
	SettingEventGapHours = "event_gap_hours"    // split events after this many quiet hours
	SettingThumbCacheMB  = "thumb_cache_mb"     // int: the thumbnail cache's size cap, in MB
	SettingOCR           = "ocr_enabled"        // bool: read text in screenshots and documents (off by default)
	SettingXMP           = "xmp_enabled"        // bool: write XMP sidecars beside photos (off by default)
	SettingCPUThreads    = "cpu_threads"        // int: CPU threads indexing may keep busy (unset: DefaultCPUThreads)
	// What indexing gives way to, besides battery power (see WaitReason).
	SettingPauseFullScreen = "pause_full_screen" // bool: wait while a game or full-screen app is in front (on by default)
	SettingPauseBusy       = "pause_busy"        // bool: wait while other programs keep the PC busy (on by default)
	SettingIdleOnly        = "idle_only"         // bool: index only while the user is away (off by default)
	// SettingFormatsExcluded lists the photo formats indexing leaves out, as
	// comma-separated families (none by default).
	SettingFormatsExcluded = "formats_excluded"
	// SettingRecentSearches holds the searches the user ran last, newest
	// first, as a JSON list (see NormalisedRecentSearches).
	SettingRecentSearches = "recent_searches"
)

// DefaultOn reports whether an on/off setting is on until the user turns it
// off. Every other one starts off.
func DefaultOn(key string) bool { return key == SettingPauseFullScreen || key == SettingPauseBusy }

// MaxCPUThreads is the most CPU threads indexing may be given: the machine's.
func MaxCPUThreads() int { return runtime.NumCPU() }

// DefaultCPUThreads is how many CPU threads indexing keeps busy unless the
// user chooses otherwise: half the machine's (at least two), leaving the rest
// to the user's programs and Lumen's own window. All of them froze a laptop.
func DefaultCPUThreads() int { return min(MaxCPUThreads(), max(2, MaxCPUThreads()/2)) }

// Settings are the user-facing preferences.
type Settings struct {
	Videos        bool    `json:"videos"`
	AIOnBattery   bool    `json:"aiOnBattery"`
	TagConfidence float64 `json:"tagConfidence"` // TagConfidenceMin..Max
	EventGapHours int     `json:"eventGapHours"`
	ThumbCacheMB  int     `json:"thumbCacheMB"` // ThumbCacheMinMB..ThumbCacheMaxMB
	OCR           bool    `json:"ocr"`
	XMP           bool    `json:"xmp"`
	// CPUThreads is how many CPU threads indexing may keep busy: 1 to
	// MaxCPUThreads, DefaultCPUThreads unless the user chose. The machine's
	// two come with it, for the setting's range and its hint.
	CPUThreads        int `json:"cpuThreads"`
	MaxCPUThreads     int `json:"maxCPUThreads"`
	DefaultCPUThreads int `json:"defaultCPUThreads"`
	// What indexing waits for (SettingPauseFullScreen, SettingPauseBusy,
	// SettingIdleOnly).
	PauseFullScreen bool `json:"pauseFullScreen"`
	PauseBusy       bool `json:"pauseBusy"`
	IdleOnly        bool `json:"idleOnly"`
	// ExcludedFormats are the photo formats indexing leaves out: files of
	// these are not added or read again, and what the library already holds
	// of them stays.
	ExcludedFormats []Family `json:"excludedFormats"`
}

// XMPExport is what an export pass did.
type XMPExport struct {
	Written   int `json:"written"`   // sidecars made or brought up to date
	Unchanged int `json:"unchanged"` // sidecars that already said the same
	Skipped   int `json:"skipped"`   // sidecars Lumen did not write, or that someone edited since: left alone
}

// XMPDelete is what deleting Lumen's sidecars did.
type XMPDelete struct {
	Deleted int `json:"deleted"` // sidecars moved to the Recycle Bin
	Kept    int `json:"kept"`    // sidecars edited since Lumen wrote them, or that could not be recycled: left alone
}

// OrganiseRunState is where a run of moves stands.
type OrganiseRunState string

// The states of an organise run.
const (
	OrganisePlanned OrganiseRunState = "planned" // previewed, nothing moved
	OrganiseRunning OrganiseRunState = "running"
	OrganiseDone    OrganiseRunState = "done"
	OrganiseUndoing OrganiseRunState = "undoing"
	OrganiseUndone  OrganiseRunState = "undone"
	OrganiseFailed  OrganiseRunState = "failed" // stopped part-way: it can be resumed or undone
)

// OrganiseMoveState is where one file's move stands.
type OrganiseMoveState string

// The states of one move.
const (
	MovePlanned OrganiseMoveState = "planned"
	MoveMoving  OrganiseMoveState = "moving" // journaled; the file may or may not have moved yet
	MoveMoved   OrganiseMoveState = "moved"
	MoveUndoing OrganiseMoveState = "undoing"
	MoveUndone  OrganiseMoveState = "undone"
	MoveSkipped OrganiseMoveState = "skipped" // changed or no longer visible since the preview
	MoveFailed  OrganiseMoveState = "failed"
)

// OrganiseMove is one file's move, planned or made.
type OrganiseMove struct {
	Seq     int               `json:"seq"`
	AssetID int64             `json:"assetId"`
	From    string            `json:"from"`
	To      string            `json:"to"`
	State   OrganiseMoveState `json:"state"`
	Renamed bool              `json:"renamed"`           // its name took a " (2)" because another file had it
	Error   string            `json:"error,omitempty"`   // why it was skipped or failed
	Sidecar SidecarState      `json:"sidecar,omitempty"` // what became of its XMP sidecar; "" is none
}

// SidecarState is what a move does, or did, with the photo's XMP sidecar.
type SidecarState string

// The states of a move's sidecar.
const (
	SidecarPlanned SidecarState = "planned" // one to move with the photo
	SidecarMoved   SidecarState = "moved"
	SidecarKept    SidecarState = "kept"   // left where it was; the move's error says why
	SidecarUndone  SidecarState = "undone" // put back with the photo
)

// OrganisePlan is a dry run: the moves a template would make, saved as a
// planned run so that starting it moves exactly what was previewed.
type OrganisePlan struct {
	RunID    int64          `json:"runId"`
	Template string         `json:"template"`
	Moves    []OrganiseMove `json:"moves"`
	Renamed  int            `json:"renamed"`  // moves whose name changed for a collision
	InPlace  int            `json:"inPlace"`  // files already where the template puts them
	Sidecars int            `json:"sidecars"` // moves that take an XMP sidecar (IMG_1.jpg.xmp) along
}

// OrganiseRun is a run of moves, as the list of past runs shows it.
type OrganiseRun struct {
	ID        int64            `json:"id"`
	Template  string           `json:"template"`
	CreatedAt int64            `json:"createdAt"` // Unix seconds
	State     OrganiseRunState `json:"state"`
	Moved     int              `json:"moved"`
	Total     int              `json:"total"`
	// SidecarsKept is how many XMP sidecars stayed behind because something
	// was at their new name.
	SidecarsKept int `json:"sidecarsKept"`
}

// OrganiseProgress is a run's progress, sent while it moves or undoes files.
type OrganiseProgress struct {
	RunID int64            `json:"runId"`
	State OrganiseRunState `json:"state"`
	Done  int              `json:"done"`
	Total int              `json:"total"`
}

// The thumbnail cache's size cap, in MB: the default, and the range the
// setting may take. frontend/src/bridge mirrors them.
const (
	ThumbCacheDefaultMB = 2048
	ThumbCacheMinMB     = 256
	ThumbCacheMaxMB     = 102400
)

const (
	// EventGapDefault is the number of quiet hours that starts a new event.
	EventGapDefault = 12
	// EventGapMin is the shortest configurable gap in hours.
	EventGapMin = 1
	// EventGapMax is the longest configurable gap in hours.
	EventGapMax = 72
)

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
	DateFromUser     DateSource = "user" // a local correction; the metadata original is kept
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

// SourceAvailability is the last background check of a source's root.
type SourceAvailability string

// Source states are kept in memory; an unchecked source is not a missing one.
const (
	SourceChecking    SourceAvailability = "checking"
	SourceAvailable   SourceAvailability = "available"
	SourceUnavailable SourceAvailability = "unavailable"
)

// Source is a folder the user asked Lumen to watch.
type Source struct {
	ID           int64              `json:"id"`
	Path         string             `json:"path"`
	Enabled      bool               `json:"enabled"`
	Availability SourceAvailability `json:"availability,omitempty"`
}

// BlockKind is the kind of item excluded from browsing.
type BlockKind string

// Items supported by blocking. More kinds can be added without changing the
// existing block records.
const (
	BlockAsset  BlockKind = "asset"
	BlockPerson BlockKind = "person"
	BlockTag    BlockKind = "tag"
	BlockPlace  BlockKind = "place"
	BlockFolder BlockKind = "folder"
	BlockAlbum  BlockKind = "album"
	BlockEvent  BlockKind = "event" // target: the event's block tag (an event_tags row)
	BlockFace   BlockKind = "face"  // target: a face_blocks row, the face as it was found
)

// FaceLookalikeMin is the cosine similarity at which a face counts as a
// look-alike of a blocked face. On the face corpus (about 27 people, 920 faces) it
// covers 97.1% of the same person's other faces and 1.2% of other people's;
// the highest value that still covers 95% is 0.43, and decision 7 leans to
// hiding more. TestFaceLookalikeThresholdOnTheFaceCorpus asserts both rates.
const FaceLookalikeMin = 0.4

// BlockMode chooses whether only the label or also its covered media vanish.
type BlockMode string

// The two scopes supported by label blocks.
const (
	BlockLabel  BlockMode = "label"
	BlockPhotos BlockMode = "photos"
)

// Block is a user-created visibility rule. Path is set only for folders.
type Block struct {
	ID       int64     `json:"id"`
	Kind     BlockKind `json:"kind"`
	TargetID int64     `json:"targetId"`
	Path     string    `json:"path"`
	Mode     BlockMode `json:"mode"`
	Name     string    `json:"name"`
	// Count is how many photos and videos the rule covers now, in either
	// mode; Cover (0 = none) is the newest of them, with a thumbnail where one
	// has been made, and CoverThumb its thumbnail version.
	Count      int   `json:"count"`
	Cover      int64 `json:"cover"`
	CoverThumb int   `json:"coverThumb"`
	// Face is the face a person or face rule is shown by (ID 0 = none).
	Face Face `json:"face"`
}

// BlockChange identifies a saved block and whether it was just created.
type BlockChange struct {
	ID      int64 `json:"id"`
	Created bool  `json:"created"`
}

// DiskDeletion is what moving files to the Windows Recycle Bin did. A file
// the shell would not recycle (on a network share, or a drive without a
// Recycle Bin) is left exactly as it was: Lumen never deletes permanently.
type DiskDeletion struct {
	Recycled int      `json:"recycled"` // files now in the Recycle Bin
	Kept     []string `json:"kept"`     // files left in place because they could not be recycled
}

// TrashSelection is what Delete from disk asks the user to confirm: trashed
// assets and how many files they have on disk.
type TrashSelection struct {
	Assets []int64 `json:"assets"`
	Files  int     `json:"files"`
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
	// The Trash names each item by its file and says what deleting it from
	// disk takes: the first file's name, and the size and number of all its
	// files. Other views leave them empty.
	Name  string `json:"name,omitempty"`
	Size  int64  `json:"size,omitempty"`
	Files int    `json:"files,omitempty"`
}

// CoveredItem is a photo a block covers, as the Blocked screen's browser
// lists it: its grid cell and the other photo-mode blocks that also hide it.
type CoveredItem struct {
	GridItem
	HiddenBy []BlockRef `json:"hiddenBy"`
}

// BlockRef names a block.
type BlockRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Filter narrows the grid to a subset of the library.
type Filter string

// The grid filters.
const (
	FilterAll        Filter = ""
	FilterPhotos     Filter = "photos"
	FilterFavourites Filter = "favourites"
	FilterVideos     Filter = "videos"
	// FilterTrash is the app-level Trash: trashed assets that are still on
	// disk, blocked ones included (the plan shows them in Trash like any other).
	FilterTrash Filter = "trash"
)

// View is what the timeline grid shows: a filter, optionally narrowed to
// one tag or one person, or one album instead.
type View struct {
	Filter Filter `json:"filter"`
	Tag    int64  `json:"tag,omitempty"`    // 0 = any
	Person int64  `json:"person,omitempty"` // 0 = anyone
	Album  int64  `json:"album,omitempty"`  // 0 = any; excludes other view filters
	Place  int64  `json:"place,omitempty"`  // 0 = anywhere
	Event  int64  `json:"event,omitempty"`  // 0 = any event
}

// Event is a consecutive run of visible media, separated by time or distance.
type Event struct {
	ID           int64  `json:"id"` // oldest asset in the run
	Name         string `json:"name"`
	NameOverride string `json:"nameOverride,omitempty"` // empty when the date/place title is automatic
	StartAt      int64  `json:"startAt"`
	EndAt        int64  `json:"endAt"`
	Count        int    `json:"count"`
	Videos       int    `json:"videos,omitempty"` // included in Count
	Cover        int64  `json:"cover"`
	CoverThumb   int    `json:"coverThumb"`
	PlaceID      int64  `json:"placeId"`
	Pinned       bool   `json:"pinned"` // the user pinned it: it sorts first
	FirstID      int64  `json:"-"`      // boundary among equal timestamps; engine only
	LastID       int64  `json:"-"`
}

// Place is a city near one or more visible GPS-tagged assets.
type Place struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	Region     string  `json:"region"`
	Country    string  `json:"country"`
	Lat        float64 `json:"lat"`
	Lon        float64 `json:"lon"`
	Count      int     `json:"count"`
	Cover      int64   `json:"cover"`
	CoverThumb int     `json:"coverThumb"`
}

// AlbumRule is a conjunction of optional filters, saved for a smart album.
// Dates are inclusive camera-clock calendar days (YYYY-MM-DD).
type AlbumRule struct {
	Kind       Kind   `json:"kind"` // "" = photos and videos
	Favourites bool   `json:"favourites"`
	From       string `json:"from"`
	To         string `json:"to"`
	Tag        int64  `json:"tag"`
	Person     int64  `json:"person"`
}

// AlbumKind identifies how an album selects its members.
type AlbumKind string

// The supported album kinds.
const (
	AlbumManual AlbumKind = "manual"
	AlbumSmart  AlbumKind = "smart"
)

// Album is a manual collection or a saved rule with its current visible count and cover.
type Album struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	Kind       AlbumKind `json:"kind"`
	Rule       AlbumRule `json:"rule"`
	Count      int       `json:"count"`
	Cover      int64     `json:"cover"`
	CoverThumb int       `json:"coverThumb"`
}

// DuplicateItem is one indexed asset and all of its visible file copies.
type DuplicateItem struct {
	ID      int64    `json:"id"`
	Paths   []string `json:"paths"`
	Size    int64    `json:"size"` // bytes in one copy
	Width   int      `json:"width"`
	Height  int      `json:"height"`
	Thumb   int      `json:"thumb"`
	TakenAt int64    `json:"takenAt"` // wall-clock seconds, see WallClock
	// What the best-copy rule reads.
	Ext      string  `json:"ext"`      // lower case, with the dot
	Camera   bool    `json:"camera"`   // EXIF names a camera
	ExifDate bool    `json:"exifDate"` // the date came from EXIF
	GPS      bool    `json:"gps"`
	AddedAt  int64   `json:"addedAt"` // when Lumen first saw it, Unix seconds
	Mtimes   []int64 `json:"mtimes"`  // per path, Unix seconds
	// Best marks the item the rule keeps; in an exact group, Keep is its
	// path to keep.
	Best bool   `json:"best"`
	Keep string `json:"keep,omitempty"`
}

// DuplicateKind says why assets were grouped for review.
type DuplicateKind string

// The kinds of duplicate group.
const (
	DuplicateExact   DuplicateKind = "exact"   // one asset whose content is in several files
	DuplicateSimilar DuplicateKind = "similar" // photos whose perceptual hashes nearly match
)

// DuplicateGroup holds exact file copies or photos that look similar.
// Similarity is a suggestion for review, never a deletion decision.
type DuplicateGroup struct {
	Kind  DuplicateKind   `json:"kind"`
	Items []DuplicateItem `json:"items"`
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
	Count      int     `json:"count"`
	Cover      int64   `json:"cover"`      // asset id; 0 = none
	CoverThumb int     `json:"coverThumb"` // its thumbnail version
	Pinned     bool    `json:"pinned"`     // the user pinned it: it sorts first
	Original   string  `json:"original"`   // a renamed built-in's own name; "" otherwise
}

// AssetTag is a tag on one asset.
type AssetTag struct {
	ID     int64     `json:"id"`
	Name   string    `json:"name"`
	Kind   TagKind   `json:"kind"`
	Source TagSource `json:"source"`
	Score  float32   `json:"score"` // AI probability; 0 for user tags
}

// NamedTarget is a named entity considered when resolving a search query.
type NamedTarget struct {
	ID      int64
	Name    string
	Kind    BlockKind
	Blocked bool
}

// SearchResult is the answer to a natural-language search, best match first.
type SearchResult struct {
	Items    []GridItem `json:"items"`
	People   []string   `json:"people"`   // people named in the query: results show all of them
	Pending  int        `json:"pending"`  // photos not yet understood, so not searchable yet
	Complete bool       `json:"complete"` // false: more matched than are shown
	NoAI     bool       `json:"noAI"`     // the description could not be searched: the AI isn't available (see AI.Error)
	Blocked  bool       `json:"blocked"`  // a blocked named entity occurred in the query
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
	ID    int64  `json:"id"`
	Name  string `json:"name"`  // "" until the user names them
	Count int    `json:"count"` // visible photos they appear in
	Cover Face   `json:"cover"` // the face to show; ID 0 = none
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
	// OriginalDate and OriginalLocation are present only while corrected.
	// They follow the latest metadata read, so restoring never revives stale metadata.
	OriginalDate     *OriginalDate      `json:"originalDate,omitempty"`
	OriginalLocation *OriginalLocation  `json:"originalLocation,omitempty"`
	Adjustment       *DisplayAdjustment `json:"adjustment,omitempty"`
	ID               int64              `json:"id"`
	Kind             Kind               `json:"kind"`
	TakenAt          int64              `json:"takenAt"` // wall-clock seconds, see WallClock
	DateFrom         DateSource         `json:"dateFrom"`
	Width            int                `json:"width"`
	Height           int                `json:"height"`
	Camera           string             `json:"camera"`
	Lat              *float64           `json:"lat"`
	Lon              *float64           `json:"lon"`
	PlaceID          int64              `json:"placeId"` // resolved nearby city; 0 if unknown
	Favourite        bool               `json:"favourite"`
	Paths            []string           `json:"paths"`
	Size             int64              `json:"size"`
	Ext              string             `json:"ext"`
	Caps             MediaCaps          `json:"caps"`  // what can be done with it (see CapsOf)
	Video            *Video             `json:"video"` // nil for photos
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
	switch FamilyOf(ext) {
	case FamilyMP4:
		if !mp4Audio[audio] {
			return PlayConvert
		}
		switch video {
		case "h264", "vp9", "av1":
			return PlayDirect
		case "hevc":
			return PlayHEVC
		}
	case FamilyWebM:
		if webmVideo[video] && webmAudio[audio] {
			return PlayDirect
		}
	default: // no other container plays in the WebView
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
	Scanning  bool    `json:"scanning"`
	ScanPath  string  `json:"scanPath"`  // the folder being looked through, while scanning
	ScanSeen  int     `json:"scanSeen"`  // photos and videos found in it so far
	Total     int     `json:"total"`     // assets in the library
	Metadata  int     `json:"metadata"`  // assets still waiting for metadata
	Thumbnail int     `json:"thumbnail"` // assets still waiting for thumbnails
	Embedding int     `json:"embedding"` // photos still waiting for the AI
	Faces     int     `json:"faces"`     // photos still waiting for face recognition
	Videos    int     `json:"videos"`    // videos still waiting for the AI or face recognition
	OCR       int     `json:"ocr"`       // photos still waiting for their text to be read
	Working   Working `json:"working"`   // the stages at work right now; the rest wait their turn
	Errors    int     `json:"errors"`
	Skipped   int     `json:"skipped"`
	Paused    bool    `json:"paused"`
	OnBattery bool    `json:"onBattery"` // AI work is waiting for mains power
	// Waiting is why every stage waits for the PC, though not paused:
	// WaitNone when nothing holds them.
	Waiting WaitReason `json:"waiting"`
	AI      AI         `json:"ai"`
}

// WaitReason is what indexing gives way to, as the user's settings ask.
// Scanning goes on meanwhile, as it does while paused: it is cheap.
type WaitReason string

// The reasons, in the order they are checked.
const (
	WaitNone       WaitReason = ""
	WaitFullScreen WaitReason = "full_screen" // a game, full-screen video or presentation is in front
	WaitBusy       WaitReason = "busy"        // other programs keep the processor busy
	WaitIdle       WaitReason = "idle"        // indexing runs only while the user is away
)

// Working says which stages are at work right now. A stage with files left
// but not at work is waiting: for an earlier stage to finish them, for mains
// power, or to try its model again.
type Working struct {
	Metadata  bool `json:"metadata"`
	Thumbnail bool `json:"thumbnail"`
	Embedding bool `json:"embedding"` // on photos
	Faces     bool `json:"faces"`     // on photos
	Videos    bool `json:"videos"`    // the AI and face recognition, on videos' frames
	OCR       bool `json:"ocr"`
}

// FailureGroup is the files that couldn't be read for one reason: what the
// status's "couldn't be read" count is made of.
type FailureGroup struct {
	Reason FailReason   `json:"reason"`
	Count  int          `json:"count"`
	Files  []FailedFile `json:"files"` // the first few, as examples
}

// FailedFile is one file that couldn't be read.
type FailedFile struct {
	Path  string `json:"path"`
	Error string `json:"error"` // what the decoder said, with the path shortened to the file's name
}

// FailReason is why a file couldn't be read, in the user's terms.
type FailReason string

// The reasons.
const (
	FailDamaged FailReason = "damaged" // not a readable photo or video: damaged, or another kind of file with its extension
	FailSlow    FailReason = "slow"    // reading it took longer than a file may
	FailLocked  FailReason = "locked"  // another app holds it, or Windows doesn't allow reading it
	FailOther   FailReason = "other"
)

// failSigns are what the decoders' messages say for each reason (lower case).
var failSigns = []struct {
	reason FailReason
	signs  []string
}{
	{FailSlow, []string{"context deadline exceeded", "timed out"}},
	{FailLocked, []string{"being used by another process", "access is denied", "permission denied"}},
	{FailDamaged, []string{
		"invalid data found", "not a known file format", "end of file", "moov atom not found", "premature end",
		"corrupt", "truncated", "unsupported image format", "the video has no frames",
	}},
}

// FailReasonOf says why a file failed, from the error a stage stored.
func FailReasonOf(msg string) FailReason {
	msg = strings.ToLower(msg)
	for _, f := range failSigns {
		for _, s := range f.signs {
			if strings.Contains(msg, s) {
				return f.reason
			}
		}
	}
	return FailOther
}

// Changes says what changed in the library: the payload of library:changed,
// so each view reloads only for what it shows. While the AI works through a
// big library, tags and people change for hours after the timeline settles.
type Changes struct {
	Sources bool `json:"sources,omitempty"` // source availability or the source list changed
	Assets  bool `json:"assets"`            // assets came, went, were blocked or unblocked, or their dates or sizes changed
	Thumbs  bool `json:"thumbs"`            // only thumbnails (and the perceptual hashes made with them) changed
	Tags    bool `json:"tags"`              // the AI tagged assets
	People  bool `json:"people"`            // faces were found or grouped into people
}

// AI is the state of the on-device models.
type AI struct {
	Device     Device `json:"device"`     // while the image model is loaded, else ""
	Error      string `json:"error"`      // why search and Things can't run (models missing, runtime failed)
	FacesError string `json:"facesError"` // why people can't be recognised
}

// Device is where a model runs.
type Device string

// The devices.
const (
	DeviceGPU Device = "gpu" // DirectML: any DirectX 12 GPU
	DeviceCPU Device = "cpu"
)

// WallClock converts t to "wall-clock seconds": the local date and time as it
// read on the camera's clock, encoded as if it were UTC. EXIF dates carry no
// time zone, so storing every date this way keeps a photo taken at 23:30 on
// the 14th on the 14th, whatever zone the user is in now.
func WallClock(t time.Time) int64 {
	y, mo, d := t.Date()
	h, mi, s := t.Clock()
	return time.Date(y, mo, d, h, mi, s, 0, time.UTC).Unix()
}

// About is what Settings → About Lumen shows.
type About struct {
	Version string `json:"version"`
	// DataDir holds the library, its thumbnails, the log and crash reports.
	DataDir string `json:"dataDir"`
	// ReleasesURL is where new versions are published. Lumen itself never
	// visits it: the user opens it, in their browser, if they wish.
	ReleasesURL string `json:"releasesURL"`
	// LastRunCrashed says the last run ended in a crash, which left a report.
	LastRunCrashed bool `json:"lastRunCrashed"`
}

// UIError is a failure in the UI, for Lumen's log.
type UIError struct {
	Message string `json:"message"`
	Stack   string `json:"stack"`
	// Where is what caught it: the UI's own error handler, a listener, or a
	// view that logged a failure it worked around.
	Where string `json:"where"`
}

// Link is a page outside Lumen that the user can open in their browser.
type Link string

// The links.
const (
	LinkReleases Link = "releases" // new versions
	LinkGeoNames Link = "geonames" // where place names come from (CC BY 4.0 asks for the credit)
)
