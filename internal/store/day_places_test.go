package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"lumen/internal/core"
)

// placedLibrary is six photos over two days, in Paris (1) and Lahore (2).
// 3 October: one in Paris, two in Lahore, one nowhere known. 2 October: one
// in each, Paris the newer.
func placedLibrary(t *testing.T) (*Store, context.Context) {
	t.Helper()
	st, ctx := openTest(t), context.Background()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	var items []IngestItem
	for i := range 6 {
		items = append(items, item(src.ID, fmt.Sprintf(`C:\Photos\%d.jpg`, i), fmt.Sprintf("h%d", i)))
	}
	check(t, errOf(st.Ingest(ctx, items)))
	_, err = st.w.ExecContext(ctx, `INSERT INTO places(id, name, region, country, lat, lon)
		VALUES (1, 'Paris', '', 'France', 48.9, 2.4), (2, 'Lahore', 'Punjab', 'Pakistan', 31.5, 74.3)`)
	check(t, err)
	third := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC).Unix()
	second := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC).Unix()
	for id, at := range map[int64]struct {
		takenAt int64
		place   any
	}{
		1: {third + 30, 1}, 2: {third + 20, 2}, 3: {third + 10, 2}, 4: {third, nil},
		5: {second + 10, 1}, 6: {second, 2},
	} {
		_, err = st.w.ExecContext(ctx, "UPDATE assets SET taken_at = ?, place_id = ? WHERE id = ?", at.takenAt, at.place, id)
		check(t, err)
	}
	return st, ctx
}

func TestADayIsNamedByWhereMostOfItsPhotosWereTakenUnlessThatNameIsBlocked(t *testing.T) {
	st, ctx := placedLibrary(t)
	places := func() []string {
		t.Helper()
		days, err := st.DayItems(ctx, "2026-10-02", "2026-10-03", core.View{Filter: core.FilterAll}, 0)
		check(t, err)
		var named []string
		for _, d := range days {
			named = append(named, d.Day+" "+d.Place)
		}
		return named
	}
	if got, want := fmt.Sprint(places()), "[2026-10-03 Lahore 2026-10-02 Paris]"; got != want {
		t.Fatalf("days are named %s, want %s", got, want)
	}
	_, err := st.SetBlock(ctx, core.BlockPlace, 2, "", core.BlockLabel)
	check(t, err)
	if got, want := fmt.Sprint(places()), "[2026-10-03  2026-10-02 Paris]"; got != want {
		t.Fatalf("with Lahore's name blocked, days are named %s, want %s", got, want)
	}
}

func TestACollectionsTopPlacesAreWhereMostOfItsItemsWereTakenUnlessTheirNameIsBlocked(t *testing.T) {
	st, ctx := placedLibrary(t)
	top := func(n int) string {
		t.Helper()
		places, err := st.TopPlaces(ctx, core.View{Filter: core.FilterAll}, 0, n)
		check(t, err)
		return fmt.Sprint(places)
	}
	if got, want := top(3), "[{2 Lahore} {1 Paris}]"; got != want {
		t.Fatalf("top places are %s, want %s", got, want)
	}
	if got, want := top(1), "[{2 Lahore}]"; got != want {
		t.Fatalf("the top place is %s, want %s", got, want)
	}
	_, err := st.SetBlock(ctx, core.BlockPlace, 2, "", core.BlockLabel)
	check(t, err)
	if got, want := top(3), "[{1 Paris}]"; got != want {
		t.Fatalf("with Lahore's name blocked, top places are %s, want %s", got, want)
	}
}
