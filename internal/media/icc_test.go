package media

import (
	"encoding/binary"
	"testing"
	"unicode/utf16"
)

// iccWith is a profile whose only tag is the description given.
func iccWith(tag []byte) []byte {
	const table = 128 + 4 + 12
	icc := make([]byte, table+len(tag))
	binary.BigEndian.PutUint32(icc[128:], 1)
	copy(icc[132:], "desc")
	binary.BigEndian.PutUint32(icc[136:], table)
	binary.BigEndian.PutUint32(icc[140:], uint32(len(tag)))
	copy(icc[table:], tag)
	return icc
}

func asciiDescription(text string) []byte {
	tag := make([]byte, 12+len(text)+1) // the text ends in a NUL
	copy(tag, "desc")
	binary.BigEndian.PutUint32(tag[8:], uint32(len(text)+1))
	copy(tag[12:], text)
	return tag
}

func localisedDescription(text string) []byte {
	units := utf16.Encode([]rune(text))
	tag := make([]byte, 28+len(units)*2)
	copy(tag, "mluc")
	binary.BigEndian.PutUint32(tag[8:], 1)
	binary.BigEndian.PutUint32(tag[12:], 12)
	copy(tag[16:], "enUS")
	binary.BigEndian.PutUint32(tag[20:], uint32(len(units)*2))
	binary.BigEndian.PutUint32(tag[24:], 28)
	for i, u := range units {
		binary.BigEndian.PutUint16(tag[28+i*2:], u)
	}
	return tag
}

func TestAProfileIsNamedAsItDescribesItselfInEitherVersion(t *testing.T) {
	for _, c := range []struct {
		what string
		icc  []byte
		want string
	}{
		{"version 2", iccWith(asciiDescription("sRGB IEC61966-2.1")), "sRGB IEC61966-2.1"},
		{"version 4", iccWith(localisedDescription("Display P3")), "Display P3"},
		{"control characters", iccWith(asciiDescription("Adobe\x07 RGB\n")), "Adobe RGB"},
		{"no description", make([]byte, 200), ""},
		{"too short", []byte("icc"), ""},
	} {
		if got := profileName(c.icc); got != c.want {
			t.Errorf("%s: name %q, want %q", c.what, got, c.want)
		}
	}
}

func TestADamagedProfileHasNoNameAndIsNotReadPastItsEnd(t *testing.T) {
	whole := iccWith(asciiDescription("Display P3"))
	for n := range whole {
		if got := profileName(whole[:n]); got != "" {
			t.Fatalf("a profile cut to %d bytes is named %q", n, got)
		}
	}
	lying := iccWith(asciiDescription("Display P3"))
	binary.BigEndian.PutUint32(lying[len(lying)-11-4:], 1<<31) // a text length far past the tag
	if got := profileName(lying); got != "" {
		t.Fatalf("a description longer than its tag is named %q", got)
	}
	many := make([]byte, 200)
	binary.BigEndian.PutUint32(many[128:], 1<<31) // more tags than the profile could hold
	if got := profileName(many); got != "" {
		t.Fatalf("a profile claiming 2^31 tags is named %q", got)
	}
}
