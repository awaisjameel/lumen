package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"lumen/internal/core"
	"lumen/internal/fileops"
	"lumen/internal/index"
	"lumen/internal/media"
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
		"RENDER_SIDE":               media.RenderSide,
		"RENDER_SIDE_MIN":           media.RenderSideMin,
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
		"AWAY_MINUTES":              float64(index.AwayAfter / time.Minute),
		"TEXT_SCALE_MIN":            core.TextScaleMin,
		"TEXT_SCALE_MAX":            core.TextScaleMax,
		"TEXT_SCALE_DEFAULT":        core.TextScaleDefault,
		"TILE_SIZE_MIN":             core.TileSizeMin,
		"TILE_SIZE_MAX":             core.TileSizeMax,
		"TILE_SIZE_DEFAULT":         core.TileSizeDefault,
		"SIDEBAR_WIDTH_MIN":         core.SidebarWidthMin,
		"SIDEBAR_WIDTH_MAX":         core.SidebarWidthMax,
		"SIDEBAR_WIDTH_DEFAULT":     core.SidebarWidthDefault,
		"INSPECTOR_WIDTH_MIN":       core.InspectorWidthMin,
		"INSPECTOR_WIDTH_MAX":       core.InspectorWidthMax,
		"INSPECTOR_WIDTH_DEFAULT":   core.InspectorWidthDefault,
		"SLIDESHOW_SECONDS_MIN":     core.SlideshowSecondsMin,
		"SLIDESHOW_SECONDS_MAX":     core.SlideshowSecondsMax,
		"SKIP_SECONDS_MIN":          core.SkipSecondsMin,
		"SKIP_SECONDS_MAX":          core.SkipSecondsMax,
		"PLAYBACK_SPEED_MIN":        core.PlaybackSpeedMin,
		"PLAYBACK_SPEED_MAX":        core.PlaybackSpeedMax,
		"RENDER_SIDE_STANDARD":      core.RenderSideStandard,
		"RENDER_SIDE_HIGH":          core.RenderSideHigh,
		"EXPORT_QUALITY_MIN":        core.ExportQualityMin,
		"EXPORT_QUALITY_MAX":        core.ExportQualityMax,
		"EXPORT_QUALITY_DEFAULT":    core.ExportQualityDefault,
		"MAX_EXPORT_PRESETS":        core.MaxExportPresets,
		"MAX_SHORTCUTS":             core.MaxShortcuts,
		"COMPRESSION_PAGE_SIZE":     core.CompressionPageSize,
		"COMPRESSION_SAVINGS_MAX":   core.CompressionMinSavingsMax,
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

	// The organise scheme's default, tokens, fallbacks and the reasons a file is
	// left out are spelt out in the bridge for the editor and the preview.
	quoted := func(pattern string) []string {
		t.Helper()
		m := regexp.MustCompile(pattern).FindStringSubmatch(bridge)
		if m == nil {
			t.Fatalf("the bridge has nothing matching %s", pattern)
		}
		var out []string
		for _, q := range regexp.MustCompile(`'([^']*)'`).FindAllStringSubmatch(m[1], -1) {
			out = append(out, q[1])
		}
		return out
	}
	if got := quoted(`export const ORGANISE_DEFAULT_TEMPLATE = ('[^']*')`); len(got) != 1 || got[0] != fileops.DefaultTemplate {
		t.Errorf("bridge ORGANISE_DEFAULT_TEMPLATE = %v, engine has %q", got, fileops.DefaultTemplate)
	}
	if got := quoted(`(?s)export const ORGANISE_TOKENS = \[(.*?)\] as const`); !slices.Equal(got, fileops.Tokens()) {
		t.Errorf("bridge ORGANISE_TOKENS = %v, engine has %v", got, fileops.Tokens())
	}
	if got := quoted(`(?s)export const ORGANISE_FALLBACKS = \{(.*?)\} as const`); !slices.Equal(got,
		[]string{fileops.FallbackEvent, fileops.FallbackPlace, fileops.FallbackCamera}) {
		t.Errorf("bridge ORGANISE_FALLBACKS = %v, engine has event, place and camera fallbacks", got)
	}
	reasons := quoted(`(?s)export const OrganiseReason = \{(.*?)\} as const`)
	wantReasons := []string{fileops.ReasonMissing, fileops.ReasonNotVisible, fileops.ReasonBlockedFolder, fileops.ReasonChanged,
		fileops.ReasonTaken, fileops.ReasonTooLong, fileops.ReasonNotAFolder, fileops.ReasonSidecarTaken,
		fileops.ReasonUnsupported, fileops.ReasonInterrupted}
	if !slices.Equal(reasons, wantReasons) {
		t.Errorf("bridge OrganiseReason = %v, engine has %v", reasons, wantReasons)
	}

	// The lists the settings offer, in the engine's order.
	names := func(list any) []string {
		var out []string
		v := reflect.ValueOf(list)
		for i := range v.Len() {
			out = append(out, v.Index(i).String())
		}
		return out
	}
	for name, want := range map[string][]string{
		"EXCLUDABLE_FAMILIES": names(core.ExcludableFamilies()),
		"START_VIEWS":         names(core.StartViews()),
		"PREF_CATEGORIES":     names(core.PrefCategories()),
	} {
		if got := quoted(`(?s)export const ` + name + ` = members\(\w+, \[(.*?)\]\)`); !slices.Equal(got, want) {
			t.Errorf("bridge %s = %v, engine has %v", name, got, want)
		}
	}
	// What a viewing copy and an export may be asked for are the renderer's bounds.
	if core.RenderSideHigh != media.RenderSide || core.ExportSideMax != media.RenderSide ||
		core.ExportSideMin != media.RenderSideMin {
		t.Errorf("core's render and export sides (%d, %d..%d) are not the renderer's (%d..%d)",
			core.RenderSideHigh, core.ExportSideMin, core.ExportSideMax, media.RenderSideMin, media.RenderSide)
	}

	for _, event := range []string{EventProgress, EventLibraryChanged, EventOrganiseProgress} {
		if !regexp.MustCompile(`'` + regexp.QuoteMeta(event) + `' satisfies keyof Events.CustomEvents`).MatchString(bridge) {
			t.Errorf("the bridge does not listen for %q", event)
		}
	}
}

func TestViewingFallbackMatchesTheEngineDefaults(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "frontend", "src", "shared", "viewing.ts"))
	if err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile(`(?s)return (\{.*?\})`).FindSubmatch(data)
	if len(block) != 2 {
		t.Fatal("missing viewing fallback")
	}
	// Enum members and mirrored constants are written by name in the fallback.
	text := string(block[1])
	for name, value := range map[string]string{
		"ViewerOpen.ViewerOpenFit":    strconv.Quote(string(core.ViewerOpenFit)),
		"ViewerWheel.ViewerWheelZoom": strconv.Quote(string(core.ViewerWheelZoom)),
		"RENDER_SIDE_HIGH":            strconv.Itoa(core.RenderSideHigh),
		"''":                          `""`,
	} {
		text = strings.ReplaceAll(text, name, value)
	}
	jsonData := regexp.MustCompile(`(\w+):`).ReplaceAll([]byte(text), []byte(`"$1":`))
	jsonData = regexp.MustCompile(`,\s*\}`).ReplaceAll(jsonData, []byte("}"))
	var got core.ViewerPreferences
	if err := json.Unmarshal(jsonData, &got); err != nil {
		t.Fatal(err)
	}
	if want := core.DefaultViewerPreferences(); !reflect.DeepEqual(got, want) {
		t.Fatalf("viewing fallback = %+v, engine = %+v", got, want)
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
