package fileops

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// ErrInvalidTemplate means a folder scheme cannot be used: the wrapped text
// says which rule it breaks.
var ErrInvalidTemplate = errors.New("invalid folder scheme")

// DefaultTemplate is the scheme the Organise view starts with.
const DefaultTemplate = "{Year}/{Month}/{Event}/{Name}"

// The tokens a template may hold.
const (
	TokenYear      = "Year"      // 2024
	TokenMonth     = "Month"     // 08
	TokenMonthName = "MonthName" // August
	TokenDay       = "Day"       // 05
	TokenEvent     = "Event"     // the event's name, as the Events view shows it
	TokenPlace     = "Place"     // the nearest city
	TokenCamera    = "Camera"    // the camera model
	TokenName      = "Name"      // the file's own name without its extension
	TokenExt       = "Ext"       // the extension in lower case without the dot
)

// Fallbacks stand in for a token that has no value for a photo, so that a
// photo without a place still lands in a folder with a name.
const (
	FallbackEvent  = "Unknown event"
	FallbackPlace  = "Unknown place"
	FallbackCamera = "Unknown camera"
	fallbackExt    = "none"
	fallbackName   = "_"
)

// Limits that keep a path usable on Windows without long-path support: a
// whole path must stay under MAX_PATH, and no name may pass 255 characters.
const (
	maxTemplateLen = 200
	maxPathLen     = 259
	maxNameLen     = 255
	maxValueRunes  = 80  // a token's value, so one long event name cannot eat the path
	maxStemRunes   = 120 // a file's own name
)

// tokenNames are the tokens in the order the UI offers them.
var tokenNames = []string{TokenYear, TokenMonth, TokenMonthName, TokenDay, TokenEvent, TokenPlace, TokenCamera, TokenName, TokenExt}

var tokens = func() map[string]bool {
	m := make(map[string]bool, len(tokenNames))
	for _, name := range tokenNames {
		m[name] = true
	}
	return m
}()

// Tokens lists the tokens a template may hold, in the order the UI offers them.
func Tokens() []string { return slices.Clone(tokenNames) }

// reservedNames are the device names Windows will not let a file or folder
// have, with or without an extension.
var reservedNames = func() map[string]bool {
	m := map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true}
	for i := 1; i <= 9; i++ {
		m[fmt.Sprintf("COM%d", i)] = true
		m[fmt.Sprintf("LPT%d", i)] = true
	}
	return m
}()

// segment is a run of literal text, or one token.
type segment struct {
	text  string
	token bool
}

// Template is a validated folder scheme: folders and a file name, each made
// of literal text and tokens. The file's extension is added to the last
// component when it is rendered, so a template names where a file goes and
// what it is called, not what type it is.
type Template struct {
	components [][]segment
}

// ParseTemplate checks a scheme and breaks it into components. It refuses an
// empty or absolute scheme, one that climbs out with "..", an unknown token,
// a character Windows does not allow in a name, a reserved device name, a
// name ending in a dot or space, and a scheme too long to leave room for a
// file name.
func ParseTemplate(text string) (Template, error) {
	bad := func(format string, args ...any) (Template, error) {
		return Template{}, fmt.Errorf("%w: %s", ErrInvalidTemplate, fmt.Sprintf(format, args...))
	}
	if strings.TrimSpace(text) == "" {
		return bad("it is empty")
	}
	if utf8.RuneCountInString(text) > maxTemplateLen {
		return bad("it is longer than %d characters", maxTemplateLen)
	}
	if strings.HasPrefix(text, "/") || strings.HasPrefix(text, `\`) || isDrivePrefix(text) {
		return bad("it must be relative to the source folder, not an absolute path")
	}
	var t Template
	for _, comp := range strings.Split(strings.ReplaceAll(text, `\`, "/"), "/") {
		segs, err := parseComponent(comp)
		if err != nil {
			return bad("%s", err)
		}
		t.components = append(t.components, segs)
	}
	// The file's own extension is added to the last name, so a scheme that
	// spells it out would give "IMG.jpg.jpg".
	last := t.components[len(t.components)-1]
	if n := len(last); n >= 2 && last[n-1] == (segment{text: TokenExt, token: true}) && !last[n-2].token &&
		strings.HasSuffix(last[n-2].text, ".") {
		return bad("the file's extension is added for you, so end the scheme without \".{%s}\"", TokenExt)
	}
	return t, nil
}

// isDrivePrefix reports whether text starts with a drive letter and a colon.
func isDrivePrefix(text string) bool {
	return len(text) >= 2 && text[1] == ':' && (text[0]|0x20 >= 'a' && text[0]|0x20 <= 'z')
}

// forbiddenRune reports whether Windows forbids r in a name (the separators
// are handled before names are looked at).
func forbiddenRune(r rune) bool {
	return strings.ContainsRune(`<>:"|?*`, r) || r < ' ' || r == 0x7f
}

// parseComponent breaks one folder or file name into literals and tokens and
// applies the rules that hold for what the user typed.
func parseComponent(comp string) ([]segment, error) {
	if strings.TrimSpace(comp) == "" {
		return nil, errors.New("a folder name between slashes is empty")
	}
	var segs []segment
	var literal strings.Builder
	flush := func() {
		if literal.Len() > 0 {
			segs = append(segs, segment{text: literal.String()})
			literal.Reset()
		}
	}
	for rest := comp; rest != ""; {
		r, size := utf8.DecodeRuneInString(rest)
		switch {
		case r == '{':
			end := strings.IndexByte(rest, '}')
			if end < 0 {
				return nil, errors.New("a { has no closing }")
			}
			name := rest[1:end]
			if !tokens[name] {
				return nil, fmt.Errorf("{%s} is not a token", name)
			}
			flush()
			segs = append(segs, segment{text: name, token: true})
			rest = rest[end+1:]
			continue
		case r == '}':
			return nil, errors.New("a } has no opening {")
		case forbiddenRune(r):
			return nil, fmt.Errorf("%q cannot be used in a folder or file name", r)
		}
		literal.WriteRune(r)
		rest = rest[size:]
	}
	flush()

	last := segs[len(segs)-1]
	// ".." would leave the source folder, and "." or "..." is no name at all;
	// say so before the more general rule about trailing dots.
	if !hasToken(segs) && strings.Trim(comp, ". ") == "" {
		return nil, fmt.Errorf("%q is not a folder name (\"..\" would leave the source folder)", comp)
	}
	if !last.token && (strings.HasSuffix(last.text, ".") || strings.HasSuffix(last.text, " ")) {
		return nil, fmt.Errorf("%q ends with a dot or a space, which Windows removes", comp)
	}
	if !hasToken(segs) && isReserved(comp) {
		return nil, fmt.Errorf("%q is a name Windows keeps for devices", comp)
	}
	return segs, nil
}

func hasToken(segs []segment) bool {
	return slices.ContainsFunc(segs, func(s segment) bool { return s.token })
}

// isReserved reports whether name is a device name, alone or before an
// extension ("con", "COM1.txt").
func isReserved(name string) bool {
	stem, _, _ := strings.Cut(name, ".")
	return reservedNames[strings.ToUpper(strings.TrimRight(stem, " "))]
}

// Values are what a template's tokens stand for, for one file.
type Values struct {
	Taken  time.Time // the photo's date, as the camera's clock read it
	Event  string    // "" for none: the fallback is used
	Place  string
	Camera string
	Stem   string // the file's name without its extension
	Ext    string // the file's extension with its dot, as it is on disk
}

// Rendered is where a file goes under a template.
type Rendered struct {
	Dirs []string // the folders below the source, outermost first
	Stem string   // the file's name before its extension
	Ext  string   // the extension with its dot
}

// Render gives the folders and the file name a file would have under the
// template. Token values are made safe for a Windows name, and a token
// without a value uses its fallback. The file keeps its own extension.
func (t Template) Render(v Values) Rendered {
	names := make([]string, len(t.components))
	for i, comp := range t.components {
		var b strings.Builder
		for _, s := range comp {
			if s.token {
				b.WriteString(t.value(s.text, v))
			} else {
				b.WriteString(s.text)
			}
		}
		names[i] = b.String()
	}
	last := len(names) - 1
	r := Rendered{Dirs: make([]string, last), Ext: v.Ext}
	for i, name := range names[:last] {
		r.Dirs[i] = safeName(name, "")
	}
	r.Stem = safeName(names[last], v.Ext)
	return r
}

func (Template) value(token string, v Values) string {
	switch token {
	case TokenYear:
		return fmt.Sprintf("%04d", v.Taken.Year())
	case TokenMonth:
		return fmt.Sprintf("%02d", int(v.Taken.Month()))
	case TokenMonthName:
		return v.Taken.Month().String()
	case TokenDay:
		return fmt.Sprintf("%02d", v.Taken.Day())
	case TokenEvent:
		return cleanValue(v.Event, maxValueRunes, FallbackEvent)
	case TokenPlace:
		return cleanValue(v.Place, maxValueRunes, FallbackPlace)
	case TokenCamera:
		return cleanValue(v.Camera, maxValueRunes, FallbackCamera)
	case TokenName:
		return cleanValue(v.Stem, maxStemRunes, fallbackName)
	default: // TokenExt: ParseTemplate admits no other token
		return cleanValue(strings.ToLower(strings.TrimPrefix(v.Ext, ".")), maxValueRunes, fallbackExt)
	}
}

// cleanValue makes text fit in a name: characters Windows forbids become
// "_", runs of white space shrink to one space, the ends lose the dots and
// spaces Windows would strip, and a long value is cut at limit characters. An
// empty result is fallback.
func cleanValue(text string, limit int, fallback string) string {
	text = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return ' ' // a tab or a line break is a gap, not a character to replace
		case r == '/' || r == '\\' || forbiddenRune(r):
			return '_'
		}
		return r
	}, text)
	text = strings.Join(strings.Fields(text), " ")
	if r := []rune(text); len(r) > limit {
		text = string(r[:limit])
	}
	if text = strings.Trim(text, ". "); text == "" {
		return fallback
	}
	return text
}

// safeName is the last line of defence for a rendered name that will be
// followed by ext: no trailing dots or spaces, no device name, not empty, and
// short enough that the name with its extension and a collision suffix fits.
func safeName(name, ext string) string {
	if room := maxNameLen - len(ext) - len(collisionSuffix(maxCollisions)); len([]rune(name)) > room {
		name = string([]rune(name)[:room])
	}
	name = strings.TrimRight(name, ". ")
	if name == "" {
		return fallbackName
	}
	if isReserved(name + ext) {
		return "_" + name
	}
	return name
}
