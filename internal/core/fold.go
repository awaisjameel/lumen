package core

import (
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Fold returns the key Lumen compares names and paths by, case-insensitively:
// two strings have the same key exactly when strings.EqualFold says they are
// equal (Unicode simple case folding, so "Émile" matches "émile" and "ΣΊΣΥΦΟΣ"
// matches "σίσυφος"). SQLite's NOCASE and LIKE fold only A–Z, so every such
// comparison goes through this, in Go or as the SQL function fold().
func Fold(s string) string {
	ascii := true
	for i := range len(s) {
		if s[i] >= utf8.RuneSelf {
			ascii = false
			break
		}
	}
	if ascii {
		// U+212A KELVIN SIGN folds with k, and U+017F LONG S with s, but
		// neither is ASCII, so plain lower case is the key here.
		return strings.ToLower(s)
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		b.WriteRune(FoldRune(r))
	}
	return b.String()
}

// FoldRune picks one representative of r's case-folding orbit (the runes
// unicode.SimpleFold cycles through): its lower-case ASCII letter if it has
// one, so ASCII text keys the same on both paths of Fold, else the smallest.
func FoldRune(r rune) rune {
	best := r
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		if f < best {
			best = f
		}
	}
	if 'A' <= best && best <= 'Z' {
		best += 'a' - 'A'
	}
	return best
}

// IsWithin reports whether path is dir or inside it, comparing as Windows
// does: whatever the case (see Fold).
func IsWithin(path, dir string) bool {
	path, dir = Fold(filepath.Clean(path)), Fold(filepath.Clean(dir))
	if path == dir {
		return true
	}
	if !strings.HasSuffix(dir, string(filepath.Separator)) {
		dir += string(filepath.Separator)
	}
	return strings.HasPrefix(path, dir)
}
