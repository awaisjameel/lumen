package media

import (
	"encoding/binary"
	"strings"
	"unicode"
	"unicode/utf16"
)

// profileName is what an ICC colour profile calls itself ("Display P3"),
// read from its description tag; "" when it has none that can be read.
func profileName(icc []byte) string {
	const header, entry = 128, 12
	if len(icc) < header+4 {
		return ""
	}
	count := int(binary.BigEndian.Uint32(icc[header:]))
	for i := range min(count, (len(icc)-header-4)/entry) {
		e := icc[header+4+i*entry:]
		if string(e[:4]) != "desc" {
			continue
		}
		offset, size := uint64(binary.BigEndian.Uint32(e[4:])), uint64(binary.BigEndian.Uint32(e[8:]))
		if offset+size > uint64(len(icc)) {
			return ""
		}
		return describedBy(icc[offset : offset+size])
	}
	return ""
}

// describedBy reads a description tag in either of its forms: version 2's
// ASCII text, or version 4's localised UTF-16 strings (the first is taken).
func describedBy(tag []byte) string {
	if len(tag) < 12 {
		return ""
	}
	var name string
	switch string(tag[:4]) {
	case "desc":
		n := uint64(binary.BigEndian.Uint32(tag[8:]))
		if 12+n > uint64(len(tag)) {
			return ""
		}
		name = string(tag[12 : 12+n])
	case "mluc":
		if binary.BigEndian.Uint32(tag[8:]) == 0 || len(tag) < 28 {
			return ""
		}
		n, offset := uint64(binary.BigEndian.Uint32(tag[20:])), uint64(binary.BigEndian.Uint32(tag[24:]))
		if offset+n > uint64(len(tag)) {
			return ""
		}
		units := make([]uint16, n/2)
		for i := range units {
			units[i] = binary.BigEndian.Uint16(tag[offset+uint64(i)*2:])
		}
		name = string(utf16.Decode(units))
	}
	// The name is shown as it is: nothing a profile holds may reach the UI
	// but printable text of a sensible length.
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if !unicode.IsPrint(r) {
			return -1
		}
		return r
	}, name))
	if runes := []rune(name); len(runes) > 80 {
		name = string(runes[:80])
	}
	return name
}
