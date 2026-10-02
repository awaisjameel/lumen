package xmp

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// golden compares got with testdata/name. Set LUMEN_UPDATE_GOLDEN=1 to
// rewrite the files after a deliberate change to the packet (and bump
// Version).
func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if os.Getenv("LUMEN_UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("packet differs from %s:\n%s", path, got)
	}
}

// wellFormed fails the test unless packet is XML a strict parser accepts.
func wellFormed(t *testing.T, packet []byte) {
	t.Helper()
	d := xml.NewDecoder(bytes.NewReader(packet))
	for {
		_, err := d.Token()
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			t.Fatalf("packet is not well-formed XML: %v\n%s", err, packet)
		}
	}
}

var full = Doc{
	Subjects:  []string{"Beaches", "Dogs", "Sunset"},
	Faces:     []Face{{Name: "Ali", X: 0.1, Y: 0.2, W: 0.2, H: 0.3}, {Name: "Sara", X: 0.6, Y: 0.25, W: 0.1, H: 0.15}},
	Favourite: true,
	Width:     4000, Height: 3000,
}

func TestPacketGoldenForAPhotoWithThingsPeopleAndAFavourite(t *testing.T) {
	got := Marshal(full)
	golden(t, "full.xmp", got)
	wellFormed(t, got)
}

func TestPacketGoldenForAPhotoWithNothingToSay(t *testing.T) {
	got := Marshal(Doc{})
	golden(t, "marker.xmp", got)
	wellFormed(t, got)
	if !(Doc{}).Empty() || (Doc{Favourite: true}).Empty() || (Doc{Subjects: []string{"x"}}).Empty() {
		t.Fatal("Empty must be true only for a Doc without content")
	}
}

func TestPacketGoldenForNamesWithAnAwkwardSpelling(t *testing.T) {
	got := Marshal(Doc{
		Subjects: []string{`Tom & Jerry <3 "quotes" 'single'`},
		Names:    []string{"Zoë Œuvre", "line\nbreak"},
	})
	golden(t, "escaped.xmp", got)
	wellFormed(t, got)
}

func TestPacketRatesOnlyAFavourite(t *testing.T) {
	if got := Marshal(Doc{Favourite: true}); !strings.Contains(string(got), `xmp:Rating="5"`) {
		t.Fatalf("a favourite has no rating of 5:\n%s", got)
	}
	if got := Marshal(Doc{Subjects: []string{"Dogs"}}); strings.Contains(string(got), "Rating") {
		t.Fatalf("a photo that is not a favourite is rated:\n%s", got)
	}
}

func TestPacketCarriesTheExporterVersion(t *testing.T) {
	if got := Marshal(Doc{}); !strings.Contains(string(got), `lumen:Exporter="`+Version+`"`) ||
		!strings.Contains(string(got), `xmlns:lumen="`+nsLumen+`"`) {
		t.Fatalf("the lumen: marker is missing:\n%s", got)
	}
}

func TestPacketIsTheSameWhateverOrderItsInputCameIn(t *testing.T) {
	shuffled := Doc{
		Subjects:  []string{"Sunset", "dogs", "Beaches", "Dogs", " ", "Beaches"},
		Faces:     []Face{full.Faces[1], full.Faces[0]},
		Favourite: true,
		Width:     4000, Height: 3000,
	}
	if !bytes.Equal(Marshal(shuffled), Marshal(full)) {
		t.Fatalf("input order changed the packet:\n%s\n----\n%s", Marshal(shuffled), Marshal(full))
	}
	if !bytes.Equal(Marshal(full), Marshal(full)) {
		t.Fatal("two calls differ")
	}
}

func TestRegionsAreCentreBasedNormalisedAndInsideThePhoto(t *testing.T) {
	got := string(Marshal(Doc{
		Faces: []Face{
			{Name: "Edge", X: 0.9, Y: -0.05, W: 0.2, H: 0.3}, // overhangs the right and top edges
			{Name: "Nobody", X: 0.4, Y: 0.4, W: 0, H: 0.1},   // no area: not a region
			{Name: "  ", X: 0.4, Y: 0.4, W: 0.1, H: 0.1},     // no name: not a region
		},
		Width: 100, Height: 50,
	}))
	want := `stArea:x="0.95" stArea:y="0.125" stArea:w="0.1" stArea:h="0.25" stArea:unit="` + areaUnit + `"`
	if !strings.Contains(got, want) {
		t.Fatalf("region is not the clamped centre-based box %s:\n%s", want, got)
	}
	if n := strings.Count(got, `mwg-rs:Type="Face"`); n != 1 {
		t.Fatalf("%d regions, want 1:\n%s", n, got)
	}
	if !strings.Contains(got, `stDim:w="100" stDim:h="50" stDim:unit="pixel"`) {
		t.Fatalf("regions do not say what size they apply to:\n%s", got)
	}
}

func TestFacesWithoutAKnownPhotoSizeAreNamedButHaveNoRegion(t *testing.T) {
	got := string(Marshal(Doc{Faces: []Face{{Name: "Ali", X: 0.1, Y: 0.1, W: 0.2, H: 0.2}}}))
	if strings.Contains(got, "mwg-rs:Regions") {
		t.Fatalf("regions written without the photo's size:\n%s", got)
	}
	if !strings.Contains(got, "<rdf:li>Ali</rdf:li>") {
		t.Fatalf("the person is not listed:\n%s", got)
	}
}

func TestPersonInImageListsEachNameOnceFromFacesAndFromNames(t *testing.T) {
	got := string(Marshal(Doc{
		Faces: []Face{{Name: "Ali", X: 0.1, Y: 0.1, W: 0.2, H: 0.2}, {Name: "ALI", X: 0.5, Y: 0.1, W: 0.2, H: 0.2}},
		Names: []string{"Sara", "Ali"},
		Width: 10, Height: 10,
	}))
	// Two spellings of one name are one person; the first in sort order stays.
	if !strings.Contains(got, "<rdf:li>ALI</rdf:li>") || !strings.Contains(got, "<rdf:li>Sara</rdf:li>") ||
		strings.Contains(got, "<rdf:li>Ali</rdf:li>") {
		t.Fatalf("PersonInImage is not Ali and Sara once each:\n%s", got)
	}
	if n := strings.Count(got, `mwg-rs:Type="Face"`); n != 2 {
		t.Fatalf("%d regions, want one per box:\n%s", n, got)
	}
}
