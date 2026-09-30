package core

import (
	"strings"
	"testing"
)

func TestFoldMatchesEqualFold(t *testing.T) {
	words := []string{
		"Photos", "PHOTOS", "photos", "Émile", "émile", "ÉMILE", "İbrahim", "ibrahim",
		"ΣΊΣΥΦΟΣ", "σίσυφος", "Straße", "STRASSE", "\u212Aelvin", "kelvin", "Иван", "иван", "",
	}
	for _, a := range words {
		for _, b := range words {
			if got, want := Fold(a) == Fold(b), strings.EqualFold(a, b); got != want {
				t.Errorf("Fold(%q) == Fold(%q) is %v; strings.EqualFold says %v", a, b, got, want)
			}
		}
	}
}

func TestIsWithin(t *testing.T) {
	cases := []struct {
		path, dir string
		want      bool
	}{
		{`C:\Photos`, `C:\Photos`, true},
		{`C:\Photos\2024\a.jpg`, `C:\Photos`, true},
		{`c:\photos\2024`, `C:\Photos\`, true},
		{`C:\ÉTÉ\a.jpg`, `c:\été`, true},
		{`C:\Photos2`, `C:\Photos`, false},
		{`C:\Pho`, `C:\Photos`, false},
		{`D:\Photos`, `C:\Photos`, false},
		{`D:\a.jpg`, `D:\`, true},
	}
	for _, c := range cases {
		if got := IsWithin(c.path, c.dir); got != c.want {
			t.Errorf("IsWithin(%q, %q) = %v, want %v", c.path, c.dir, got, c.want)
		}
	}
}
