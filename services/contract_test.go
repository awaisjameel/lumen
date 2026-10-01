package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"

	"lumen/internal/core"
	"lumen/internal/search"
	"lumen/internal/store"
)

// The bridge (frontend/src/bridge/index.ts) mirrors what the generated
// bindings don't carry: constants, error codes and event names. A change on
// one side only fails here.
func TestBridgeMirrorsTheEngine(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "frontend", "src", "bridge", "index.ts"))
	if err != nil {
		t.Fatal(err)
	}
	bridge := string(src)

	constant := func(name string) float64 {
		t.Helper()
		m := regexp.MustCompile(`export const ` + name + ` = ([0-9.]+)`).FindStringSubmatch(bridge)
		if m == nil {
			t.Fatalf("bridge has no constant %s", name)
		}
		v, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for name, want := range map[string]float64{
		"THUMB_SMALL":               core.ThumbSmall,
		"THUMB_LARGE":               core.ThumbLarge,
		"SPRITE_COLS":               core.SpriteCols,
		"SPRITE_ROWS":               core.SpriteRows,
		"TAG_CONFIDENCE_DEFAULT":    core.TagConfidenceDefault,
		"TAG_CONFIDENCE_MIN":        core.TagConfidenceMin,
		"TAG_CONFIDENCE_MAX":        core.TagConfidenceMax,
		"EVENT_GAP_DEFAULT":         core.EventGapDefault,
		"EVENT_GAP_MIN":             core.EventGapMin,
		"EVENT_GAP_MAX":             core.EventGapMax,
		"MAX_NAME_LEN":              core.MaxNameLen,
		"PIN_MIN_LEN":               core.PINMinLen,
		"PIN_MAX_LEN":               core.PINMaxLen,
		"PIN_COOLDOWN_SECONDS":      float64(pinCooldown / time.Second),
		"BLOCK_UNLOCK_IDLE_MINUTES": float64(blockUnlockIdle / time.Minute),
		"THUMB_CACHE_DEFAULT_MB":    core.ThumbCacheDefaultMB,
		"THUMB_CACHE_MIN_MB":        core.ThumbCacheMinMB,
		"THUMB_CACHE_MAX_MB":        core.ThumbCacheMaxMB,
	} {
		if got := constant(name); got != want {
			t.Errorf("bridge %s = %v, engine has %v", name, got, want)
		}
	}

	block := regexp.MustCompile(`(?s)export const ErrorCode = \{(.*?)\} as const`).FindStringSubmatch(bridge)
	if block == nil {
		t.Fatal("bridge has no ErrorCode")
	}
	inBridge := map[string]bool{}
	for _, m := range regexp.MustCompile(`'([a-z_]+)'`).FindAllStringSubmatch(block[1], -1) {
		inBridge[m[1]] = true
	}
	inEngine := map[string]bool{}
	for _, c := range errorCodes {
		inEngine[string(c.code)] = true
		if !inBridge[string(c.code)] {
			t.Errorf("error code %q is not in the bridge's ErrorCode", c.code)
		}
	}
	for code := range inBridge {
		if !inEngine[code] {
			t.Errorf("the bridge's error code %q is not one the engine sends", code)
		}
	}

	for _, event := range []string{EventProgress, EventLibraryChanged} {
		if !regexp.MustCompile(`'` + regexp.QuoteMeta(event) + `' satisfies keyof Events.CustomEvents`).MatchString(bridge) {
			t.Errorf("the bridge does not listen for %q", event)
		}
	}
}

func TestClosedSearchHasALocalisedFailureCode(t *testing.T) {
	var failure Failure
	if err := json.Unmarshal(MarshalError(fmt.Errorf("search: %w", search.ErrClosed)), &failure); err != nil {
		t.Fatal(err)
	}
	if failure.Code != CodeNoAI {
		t.Fatalf("closed search code = %q, want %q", failure.Code, CodeNoAI)
	}
}

func TestAlbumValidationFailuresHaveSpecificCodes(t *testing.T) {
	for _, c := range []struct {
		err  error
		code ErrorCode
	}{
		{store.ErrAlbumMediaKind, CodeAlbumMediaKind},
		{store.ErrAlbumSelection, CodeAlbumSelection},
		{store.ErrAlbumDate, CodeAlbumDate},
		{store.ErrAlbumDateOrder, CodeAlbumDateOrder},
		{store.ErrAlbumSmartItems, CodeAlbumSmartItems},
		{store.ErrAlbumKind, CodeAlbumKind},
		{store.ErrAlbumReference, CodeAlbumReference},
		{store.ErrAlbumManualOnly, CodeAlbumManualOnly},
	} {
		var failure Failure
		if err := json.Unmarshal(MarshalError(fmt.Errorf("save failed: %w", c.err)), &failure); err != nil {
			t.Fatal(err)
		}
		if failure.Code != c.code {
			t.Errorf("%v: code %s, want %s", c.err, failure.Code, c.code)
		}
	}
}
