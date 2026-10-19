package store

import (
	"fmt"
	"slices"
	"testing"

	"lumen/internal/core"
)

func TestRecentSearchesKeepTheNewestOfEachOnlyAndForgetOnClear(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	for _, q := range []string{"beach", "  Sunset ", "BEACH"} {
		_, err := st.RememberSearch(ctx, q)
		check(t, err)
	}
	got, err := st.RecentSearches(ctx)
	check(t, err)
	if want := []string{"BEACH", "Sunset"}; !slices.Equal(got, want) {
		t.Fatalf("recent = %q, want %q", got, want)
	}
	for i := range core.RecentSearchesMax + 3 {
		_, err := st.RememberSearch(ctx, fmt.Sprint("query ", i))
		check(t, err)
	}
	got, err = st.RecentSearches(ctx)
	check(t, err)
	if len(got) != core.RecentSearchesMax || got[0] != fmt.Sprint("query ", core.RecentSearchesMax+2) {
		t.Fatalf("recent = %q, want the newest %d", got, core.RecentSearchesMax)
	}
	check(t, st.ClearRecentSearches(ctx))
	got, err = st.RecentSearches(ctx)
	check(t, err)
	if got == nil || len(got) != 0 {
		t.Fatalf("after clearing, recent = %#v, want an empty list", got)
	}
}
