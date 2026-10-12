// Package update owns installation policy, authenticated delivery and runtime
// activation. It never reads photo records or executes SQL.
package update

import (
	"encoding/hex"
	"errors"
	"path"
	"strconv"
	"strings"

	"lumen/internal/core"
)

// Errors identify failures that the service and UI can explain consistently.
var (
	ErrUnavailable = errors.New("trusted updates are not configured for this installation")
	ErrBusy        = errors.New("another update operation is active")
	ErrInvalid     = errors.New("invalid update metadata or payload")
	ErrState       = errors.New("the update cannot do that now")
	ErrSpace       = errors.New("insufficient update staging space")
	ErrPublisher   = errors.New("update publisher verification failed")
)

// File binds one regular runtime file to authenticated bytes.
type File struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Release is TUF target custom metadata; all eligibility fields are signed.
type Release struct {
	ArtifactURL string             `json:"artifactURL"`
	Product     string             `json:"product"`
	Channel     core.UpdateChannel `json:"channel"`
	Platform    string             `json:"platform"`
	Arch        string             `json:"arch"`
	Version     string             `json:"version"`
	MinUpdater  int                `json:"minUpdater"`
	MinWindows  uint32             `json:"minWindows"`
	ReadMin     int                `json:"readMin"`
	ReadMax     int                `json:"readMax"`
	WriteSchema int                `json:"writeSchema"`
	Notes       string             `json:"notes"`
	Files       []File             `json:"files"`
}

// Candidate adds the TUF-authenticated target identity and archive digest.
type Candidate struct {
	Release Release `json:"release"`
	Target  string  `json:"target"`
	Length  int64   `json:"length"`
	SHA256  string  `json:"sha256"`
	URL     string  `json:"url"`
}

const (
	product        = "io.lumen.app"
	updaterVersion = 1
	maxPayload     = int64(4 << 30)
	maxExpanded    = int64(8 << 30)
	maxFiles       = 4096
)

func validHash(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 32 && s == strings.ToLower(s)
}

// safePath uses Windows rules even when a publisher runs on another platform.
func safePath(s string) bool {
	if s == "" || len(s) > 220 || path.Clean(s) != s || strings.HasPrefix(s, "/") || strings.ContainsAny(s, "\\:*?\"<>|\x00\r\n") {
		return false
	}
	for _, part := range strings.Split(s, "/") {
		if part == "." || part == ".." || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return false
		}
		base := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || base == "CONIN$" || base == "CONOUT$" {
			return false
		}
		if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && strings.ContainsRune("123456789¹²³", rune(base[3])) {
			return false
		}
	}
	return true
}

func version(s string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || strconv.Itoa(n) != p {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

func newer(a, b string) bool {
	x, ok := version(a)
	if !ok {
		return false
	}
	y, ok := version(b)
	if !ok {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return x[i] > y[i]
		}
	}
	return false
}

func (r Release) validate() (int64, error) {
	_, valid := version(r.Version)
	if !valid || r.Product != product || r.Platform != "windows" || r.Arch != "amd64" || (r.Channel != core.UpdateStable && r.Channel != core.UpdatePreview) || r.MinUpdater < 1 || r.MinUpdater > updaterVersion || r.MinWindows < 10240 || r.ReadMin < 1 || r.ReadMax < r.ReadMin || r.WriteSchema < r.ReadMin || r.WriteSchema > r.ReadMax || len(r.Notes) > 32<<10 || len(r.Files) == 0 || len(r.Files) > maxFiles {
		return 0, ErrInvalid
	}
	seen := make(map[string]bool)
	var total int64
	for _, f := range r.Files {
		key := core.Fold(f.Path)
		if !safePath(f.Path) || seen[key] || f.Size < 0 || f.Size > maxExpanded-total || !validHash(f.SHA256) {
			return 0, ErrInvalid
		}
		seen[key] = true
		total += f.Size
	}
	for key := range seen {
		for parent := path.Dir(key); parent != "."; parent = path.Dir(parent) {
			if seen[parent] {
				return 0, ErrInvalid
			}
		}
	}
	if !seen["lumen-app.exe"] || !seen["third_party_notices.txt"] {
		return 0, ErrInvalid
	}
	return total, nil
}

// ValidateRelease applies the application's signed identity and inventory rules.
func ValidateRelease(r Release) error { _, err := r.validate(); return err }
