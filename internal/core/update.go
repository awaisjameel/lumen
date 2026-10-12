package core

// UpdateChannel is authenticated release policy, independent of the library.
type UpdateChannel string

// The supported release channels.
const (
	UpdateStable  UpdateChannel = "stable"
	UpdatePreview UpdateChannel = "preview"
)

// UpdatePhase describes what the installation coordinator has actually done.
type UpdatePhase string

// Update phases are persisted before their corresponding side effects.
const (
	UpdateIdle           UpdatePhase = "idle"
	UpdateChecking       UpdatePhase = "checking"
	UpdateAvailable      UpdatePhase = "available"
	UpdateDownloading    UpdatePhase = "downloading"
	UpdateVerifying      UpdatePhase = "verifying"
	UpdateStaging        UpdatePhase = "staging"
	UpdateReady          UpdatePhase = "ready"
	UpdateQuiescing      UpdatePhase = "quiescing"
	UpdateApplying       UpdatePhase = "applying"
	UpdateAwaitingHealth UpdatePhase = "awaiting-health"
	UpdateComplete       UpdatePhase = "complete"
	UpdateRecovering     UpdatePhase = "recovering"
	UpdateFailed         UpdatePhase = "failed"
	UpdateCancelled      UpdatePhase = "cancelled"
)

// UpdatePolicy belongs to the installation, never a portable appearance preset.
type UpdatePolicy struct {
	Automatic    bool          `json:"automatic"`
	Channel      UpdateChannel `json:"channel"`
	DeferBattery bool          `json:"deferBattery"`
	BandwidthKB  int           `json:"bandwidthKB"`
}

// UpdateOffer exposes signed release text as plain text, never remote HTML.
type UpdateOffer struct {
	Version       string `json:"version"`
	Notes         string `json:"notes"`
	DownloadBytes int64  `json:"downloadBytes"`
	ExpandedBytes int64  `json:"expandedBytes"`
}

// UpdateStatus is a generation-tagged installation snapshot.
type UpdateStatus struct {
	Generation       uint64       `json:"generation"`
	Policy           UpdatePolicy `json:"policy"`
	Phase            UpdatePhase  `json:"phase"`
	InstalledVersion string       `json:"installedVersion"`
	Configured       bool         `json:"configured"`
	Managed          bool         `json:"managed"`
	Coordinator      bool         `json:"coordinator"`
	LastCheck        int64        `json:"lastCheck"`
	Offer            UpdateOffer  `json:"offer"`
	Bytes            int64        `json:"bytes"`
	Error            string       `json:"error"`
	Dismissed        string       `json:"dismissed"`
}
