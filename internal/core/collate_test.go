package core

import (
	"slices"
	"testing"
)

func TestCompareNamesSortsAccentedLettersWithTheirBaseLetters(t *testing.T) {
	want := []string{
		"Album 2", "Album 10", "Ångström", "emile", "Emile", "émile", "Émile", "Emma", "Ølstykke", "Omar", "Zoë",
		"Ζωή", "Иван",
	}
	got := slices.Clone(want)
	slices.Reverse(got)
	slices.SortFunc(got, CompareNames)
	if !slices.Equal(got, want) {
		t.Errorf("sorted names:\n got %q\nwant %q", got, want)
	}
}

func TestCompareNamesIsATotalOrder(t *testing.T) {
	// The collation calls the trips equal (one number, with leading zeros), so
	// only the fallback to bytes keeps their order from depending on the input.
	names := []string{"Trip 007", "Trip 7", "Trip 07", "Trip 8", "a", "A", ""}
	for _, a := range names {
		if CompareNames(a, a) != 0 {
			t.Errorf("CompareNames(%q, itself) != 0", a)
		}
		for _, b := range names {
			if a != b && CompareNames(a, b) == 0 {
				t.Errorf("CompareNames(%q, %q) = 0 for different names", a, b)
			}
			if CompareNames(a, b) != -CompareNames(b, a) {
				t.Errorf("CompareNames(%q, %q) and its reverse disagree", a, b)
			}
		}
	}
}
