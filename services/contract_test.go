package services

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"lumen/internal/core"
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
		"THUMB_SMALL":            core.ThumbSmall,
		"THUMB_LARGE":            core.ThumbLarge,
		"SPRITE_COLS":            core.SpriteCols,
		"SPRITE_ROWS":            core.SpriteRows,
		"TAG_CONFIDENCE_DEFAULT": core.TagConfidenceDefault,
		"TAG_CONFIDENCE_MIN":     core.TagConfidenceMin,
		"TAG_CONFIDENCE_MAX":     core.TagConfidenceMax,
		"MAX_NAME_LEN":           core.MaxNameLen,
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
