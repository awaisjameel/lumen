package core

// CompressionMode describes the preservation promise, never a quality level.
type CompressionMode string

// Compression modes all publish new copies. Restoration is offered from a verified run.
const (
	CompressCompatible CompressionMode = "compatible"
	CompressReversible CompressionMode = "reversible"
	CompressConvert    CompressionMode = "convert"
	CompressRestore    CompressionMode = "restore"
)

// CompressionEffort changes search time, with unchanged preservation checks.
type CompressionEffort string

// The bounded effort presets.
const (
	CompressFast     CompressionEffort = "fast"
	CompressBalanced CompressionEffort = "balanced"
	CompressMaximum  CompressionEffort = "maximum"
)

// CompressionOptions contains portable defaults only: no paths or automatic actions.
type CompressionOptions struct {
	Mode       CompressionMode   `json:"mode"`
	Effort     CompressionEffort `json:"effort"`
	MinSavings int               `json:"minSavings"` // percent, 0..90
	Threads    int               `json:"threads"`    // at most the shared indexing budget
}

// DefaultCompressionOptions favours compatibility and bounded work.
func DefaultCompressionOptions() CompressionOptions {
	return CompressionOptions{Mode: CompressCompatible, Effort: CompressBalanced, Threads: 1}
}

// Normalised makes stored defaults safe without enabling restoration or replacement.
func (o CompressionOptions) Normalised() CompressionOptions {
	switch o.Mode {
	case CompressCompatible, CompressReversible, CompressConvert:
	default:
		o.Mode = CompressCompatible
	}
	switch o.Effort {
	case CompressFast, CompressBalanced, CompressMaximum:
	default:
		o.Effort = CompressBalanced
	}
	o.MinSavings = min(max(o.MinSavings, 0), 90)
	o.Threads = min(max(o.Threads, 1), DefaultCPUThreads())
	return o
}

// CompressionState is a durable run or per-file state.
type CompressionState string

// The journal records every boundary that can be interrupted.
const (
	CompressionPlanned    CompressionState = "planned"
	CompressionEncoding   CompressionState = "encoding"
	CompressionVerifying  CompressionState = "verifying"
	CompressionReady      CompressionState = "ready"
	CompressionCommitting CompressionState = "committing"
	CompressionDone       CompressionState = "done"
	CompressionPaused     CompressionState = "paused"
	CompressionSkipped    CompressionState = "skipped"
	CompressionCancelled  CompressionState = "cancelled"
	CompressionFailed     CompressionState = "failed"
)

// CompressionInput explicitly chooses a duplicate path, or the first readable copy.
type CompressionInput struct {
	AssetID int64  `json:"assetId"`
	Path    string `json:"path"`
}

// CompressionJob is a measured candidate and its publication journal.
type CompressionJob struct {
	ID            int64            `json:"id"`
	RunID         int64            `json:"runId"`
	AssetID       int64            `json:"assetId"`
	From          string           `json:"from"`
	To            string           `json:"to"`
	Copies        int              `json:"copies"`
	Size          int64            `json:"size"`
	MTime         int64            `json:"mtime"`
	Digest        string           `json:"digest"`
	OutputSize    int64            `json:"outputSize"`
	OutputDigest  string           `json:"outputDigest"`
	State         CompressionState `json:"state"`
	Reason        string           `json:"reason"`
	Verification  string           `json:"verification"`
	Tool          string           `json:"tool"`
	Temp          string           `json:"-"`
	PublishTemp   string           `json:"-"`
	SidecarDigest string           `json:"-"`
	SidecarSize   int64            `json:"-"`
	RestoreDigest string           `json:"-"`
	RestoreSize   int64            `json:"-"`
	OutputAssetID int64            `json:"outputAssetId"`
}

// CompressionRun keeps local history and bounded progress, without codec percentages.
type CompressionRun struct {
	ID         int64              `json:"id"`
	CreatedAt  int64              `json:"createdAt"`
	Options    CompressionOptions `json:"options"`
	State      CompressionState   `json:"state"`
	Total      int                `json:"total"`
	Processed  int                `json:"processed"`
	Ready      int                `json:"ready"`
	Done       int                `json:"done"`
	Skipped    int                `json:"skipped"`
	Failed     int                `json:"failed"`
	Cancelled  int                `json:"cancelled"`
	SavedBytes int64              `json:"savedBytes"`
	AddedBytes int64              `json:"addedBytes"`
}

// CompressionPage bounds a public result list even for a large run.
type CompressionPage struct {
	Jobs   []CompressionJob `json:"jobs"`
	Offset int              `json:"offset"`
	Total  int              `json:"total"`
}

// CompressionCapability is the catalogue's conservative source eligibility.
type CompressionCapability struct {
	Family     Family `json:"family"`
	Compatible bool   `json:"compatible"`
	Reversible bool   `json:"reversible"`
	Convert    bool   `json:"convert"`
}

// CompressionCapabilities advertises only implemented, verified preservation paths.
func CompressionCapabilities() []CompressionCapability {
	return []CompressionCapability{{Family: FamilyJPEG, Compatible: true, Reversible: true}, {Family: FamilyPNG, Compatible: true, Convert: true}}
}
