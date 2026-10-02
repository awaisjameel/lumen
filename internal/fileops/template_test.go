package fileops

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestTemplatesTheUserMayWrite(t *testing.T) {
	for _, text := range []string{
		DefaultTemplate,
		"{Year}",
		`{Year}\{Month}\{Name}`,
		"Photos {Year}/{MonthName}/{Day} {Name}",
		"{Ext}/{Year}/{Name}",
		"{Year}-{Month}-{Day} {Name}",
		"{Camera}/{Place}/{Name}",
		"con{Year}/{Name}", // a token makes the name safe: the value is checked when it is rendered
		"{Name}.{Year}",
		"Mum's photos (2024)/{Name}",
	} {
		if _, err := ParseTemplate(text); err != nil {
			t.Errorf("%q was refused: %v", text, err)
		}
	}
}

func TestTemplatesThatCannotWorkAreRefusedWithTheRuleTheyBreak(t *testing.T) {
	for _, c := range []struct{ name, text string }{
		{"empty", ""},
		{"only spaces", "   "},
		{"absolute with a slash", "/Photos/{Name}"},
		{"absolute with a backslash", `\Photos\{Name}`},
		{"a drive letter", `D:\Photos\{Name}`},
		{"a drive letter with a slash", "c:/{Name}"},
		{"up a folder", "../{Name}"},
		{"up a folder in the middle", "a/../{Name}"},
		{"up a folder with a backslash", `{Year}\..\..\{Name}`},
		{"the folder itself", "./{Name}"},
		{"only dots", "{Year}/.../{Name}"},
		{"an unknown token", "{Year}/{Nonsense}"},
		{"a token in the wrong case", "{year}/{Name}"},
		{"an unclosed brace", "{Year/{Name}"},
		{"a stray closing brace", "Year}/{Name}"},
		{"a colon", "{Year}/a:b"},
		{"a question mark", "{Year}/what?/{Name}"},
		{"an asterisk", "{Year}/*/{Name}"},
		{"a quote", `{Year}/"x"/{Name}`},
		{"angle brackets", "<{Year}>/{Name}"},
		{"a pipe", "{Year}|{Month}/{Name}"},
		{"a control character", "{Year}/a\tb/{Name}"},
		{"a device name", "{Year}/CON/{Name}"},
		{"a device name with an extension", "{Year}/nul.txt/{Name}"},
		{"a device name in lower case", "com1/{Name}"},
		{"a name ending in a dot", "{Year}/Trip./{Name}"},
		{"a name ending in a space", "{Year}/Trip /{Name}"},
		{"an empty folder name", "{Year}//{Name}"},
		{"a trailing slash", "{Year}/{Name}/"},
		{"the extension spelt out", "{Year}/{Name}.{Ext}"},
		{"too long", strings.Repeat("a", maxTemplateLen+1)},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseTemplate(c.text)
			if !errors.Is(err, ErrInvalidTemplate) {
				t.Fatalf("%q gave %v, want ErrInvalidTemplate", c.text, err)
			}
		})
	}
}

func TestTemplateTokensStandForTheirValues(t *testing.T) {
	tmpl, err := ParseTemplate("{Year}/{Month}/{MonthName}/{Day}/{Event}/{Place}/{Camera}/{Ext}/{Name}")
	if err != nil {
		t.Fatal(err)
	}
	r := tmpl.Render(Values{Taken: time.Date(2024, time.August, 5, 23, 59, 0, 0, time.UTC), Event: "Beach day", Place: "Karachi",
		Camera: "Pixel 8", Stem: "IMG_0042", Ext: ".JPG"})
	want := []string{"2024", "08", "August", "05", "Beach day", "Karachi", "Pixel 8", "jpg"}
	if !equalStrings(r.Dirs, want) || r.Stem != "IMG_0042" || r.Ext != ".JPG" {
		t.Fatalf("rendered %+v, want folders %q and the file IMG_0042.JPG", r, want)
	}
}

func TestEmptyTokenValuesTakeTheirFixedFallback(t *testing.T) {
	tmpl, err := ParseTemplate("{Event}/{Place}/{Camera}/{Name}")
	if err != nil {
		t.Fatal(err)
	}
	r := tmpl.Render(Values{Taken: day(2024, time.May, 1), Event: "  ", Place: "...", Stem: "x", Ext: ".jpg"})
	if want := []string{FallbackEvent, FallbackPlace, FallbackCamera}; !equalStrings(r.Dirs, want) {
		t.Fatalf("folders = %q, want %q", r.Dirs, want)
	}
}

func TestRenderedNamesAreAlwaysNamesWindowsAccepts(t *testing.T) {
	tmpl, err := ParseTemplate("{Event}/{Place}/{Name}")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ in, want string }{
		{`a<b>c:d"e|f?g*h`, "a_b_c_d_e_f_g_h"},
		{`up/down\across`, "up_down_across"},
		{"tab\tand\nnewline", "tab and newline"},
		{"  spaced   out  ", "spaced out"},
		{"ends with dots...", "ends with dots"},
		{"CON", "_CON"},
		{"aux", "_aux"},
		{"LPT1", "_LPT1"},
		{"com10", "com10"}, // only COM1 to COM9 are devices
		{"Zoë's café", "Zoë's café"},
	} {
		r := tmpl.Render(Values{Taken: day(2024, time.May, 1), Event: c.in, Place: "p", Stem: "x", Ext: ".jpg"})
		if r.Dirs[0] != c.want {
			t.Errorf("event %q rendered as %q, want %q", c.in, r.Dirs[0], c.want)
		}
	}
}

func TestALongValueIsCutAndAFileNameKeepsItsExtensionAndRoomForASuffix(t *testing.T) {
	tmpl, err := ParseTemplate("{Event}/{Name}")
	if err != nil {
		t.Fatal(err)
	}
	r := tmpl.Render(Values{Taken: day(2024, time.May, 1), Event: strings.Repeat("é", 500), Stem: strings.Repeat("n", 500), Ext: ".jpeg"})
	if utf8.RuneCountInString(r.Dirs[0]) != maxValueRunes {
		t.Errorf("the event folder has %d characters, want %d", utf8.RuneCountInString(r.Dirs[0]), maxValueRunes)
	}
	if n := utf8.RuneCountInString(r.Stem) + len(r.Ext) + len(collisionSuffix(maxCollisions)); n > maxNameLen {
		t.Errorf("the file name can reach %d characters, more than Windows's %d", n, maxNameLen)
	}
}

func TestADeviceNameWithTheFilesExtensionIsStillADeviceName(t *testing.T) {
	tmpl, err := ParseTemplate("{Name}")
	if err != nil {
		t.Fatal(err)
	}
	r := tmpl.Render(Values{Taken: day(2024, time.May, 1), Stem: "nul", Ext: ".jpg"})
	if r.Stem != "_nul" {
		t.Fatalf("stem = %q, want _nul (nul.jpg is a device name)", r.Stem)
	}
}

func TestACollisionSuffixCountsFromTwo(t *testing.T) {
	for n, want := range map[int]string{1: "", 2: " (2)", 10: " (10)"} {
		if got := collisionSuffix(n); got != want {
			t.Errorf("suffix %d = %q, want %q", n, got, want)
		}
	}
}

func TestASchemeThatClimbsOutOfTheSourceFolderSaysSo(t *testing.T) {
	for _, text := range []string{"../{Name}", "{Year}/../../{Name}", "{Year}/./{Name}"} {
		_, err := ParseTemplate(text)
		if err == nil || !strings.Contains(err.Error(), "source folder") {
			t.Errorf("%q gave %v, want an explanation that it leaves the source folder", text, err)
		}
	}
}
