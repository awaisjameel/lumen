// Package xmp writes the XMP packet that goes in a photo's sidecar file
// (IMG_1.jpg.xmp): its Things as dc:subject, the people in it as
// Iptc4xmpExt:PersonInImage and MWG face regions, a favourite as
// xmp:Rating 5, and a lumen: marker that says which Lumen wrote it. It is
// plain Go with no dependency, and its output depends on nothing but its
// input, so the same photo always produces the same bytes: the exporter
// compares hashes to know whether a sidecar is up to date.
//
// The package decides nothing about what may be exported. Blocked labels and
// photos are filtered out by the store before a Doc is made; whatever is in
// a Doc is written.
package xmp

import (
	"bytes"
	"cmp"
	"encoding/xml"
	"math"
	"slices"
	"strconv"
	"strings"

	"lumen/internal/core"
)

// Version is the exporter version the lumen: marker carries. Change it when
// the packet's layout changes: every sidecar Lumen wrote then reads as out of
// date and is rewritten.
const Version = "1"

// bom is U+FEFF in UTF-8, the begin attribute of a packet: it tells a reader
// the packet is UTF-8.
const bom = "\xEF\xBB\xBF"

// nsLumen names the lumen: namespace. Like every XMP namespace it is only an
// identifier, never fetched; the .invalid domain can never resolve.
const nsLumen = "http://ns.lumen.invalid/xmp/1.0/"

// areaUnit is the unit of a region's box: fractions of the photo. The MWG
// specification spells it with a z.
const areaUnit = "normalized" //nolint:misspell // the specification's value, not prose

// Face is one named face. The box is as the store has it: the top-left
// corner, width and height, as fractions of the upright photo.
type Face struct {
	Name       string
	X, Y, W, H float64
}

// Doc is what a sidecar says about one photo. The zero Doc says nothing but
// the marker.
type Doc struct {
	Subjects  []string // the Things on the photo
	Faces     []Face   // named people's faces; their names go to PersonInImage too
	Names     []string // named people with no box to give (in a video), written to PersonInImage only
	Favourite bool     // written as xmp:Rating 5; there is no rating otherwise
	// Width and Height are the upright photo's pixels, which the face regions
	// are stated against (MWG requires them). Regions are left out without them.
	Width, Height int
}

// Empty reports whether the Doc has nothing to say: a sidecar with only the
// marker is not worth creating beside a photo.
func (d Doc) Empty() bool {
	return len(d.Subjects) == 0 && len(d.Faces) == 0 && len(d.Names) == 0 && !d.Favourite
}

// Marshal returns the packet for d: UTF-8 XML, escaped, with its lists sorted
// and without repeats, so the output is deterministic.
func Marshal(d Doc) []byte {
	var b bytes.Buffer
	b.WriteString("<?xpacket begin=\"" + bom + "\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>\n")
	b.WriteString("<x:xmpmeta xmlns:x=\"adobe:ns:meta/\" x:xmptk=\"Lumen\">\n")
	b.WriteString(" <rdf:RDF xmlns:rdf=\"http://www.w3.org/1999/02/22-rdf-syntax-ns#\">\n")
	b.WriteString("  <rdf:Description rdf:about=\"\"\n")
	b.WriteString("    xmlns:dc=\"http://purl.org/dc/elements/1.1/\"\n")
	b.WriteString("    xmlns:xmp=\"http://ns.adobe.com/xap/1.0/\"\n")
	b.WriteString("    xmlns:Iptc4xmpExt=\"http://iptc.org/std/Iptc4xmpExt/2008-02-29/\"\n")
	b.WriteString("    xmlns:mwg-rs=\"http://www.metadataworkinggroup.com/schemas/regions/\"\n")
	b.WriteString("    xmlns:stDim=\"http://ns.adobe.com/xap/1.0/sType/Dimensions#\"\n")
	b.WriteString("    xmlns:stArea=\"http://ns.adobe.com/xmp/sType/Area#\"\n")
	b.WriteString("    xmlns:lumen=\"" + nsLumen + "\"\n")
	if d.Favourite {
		b.WriteString("    xmp:Rating=\"5\"\n")
	}
	b.WriteString("    lumen:Exporter=\"" + Version + "\">\n")

	if subjects := unique(d.Subjects); len(subjects) > 0 {
		bag(&b, "dc:subject", subjects)
	}
	if people := unique(slices.Concat(d.Names, faceNames(d.Faces))); len(people) > 0 {
		bag(&b, "Iptc4xmpExt:PersonInImage", people)
	}
	if regions := regionsOf(d); len(regions) > 0 {
		writeRegions(&b, d, regions)
	}

	b.WriteString("  </rdf:Description>\n")
	b.WriteString(" </rdf:RDF>\n")
	b.WriteString("</x:xmpmeta>\n")
	b.WriteString("<?xpacket end=\"w\"?>\n")
	return b.Bytes()
}

// bag writes an unordered list property.
func bag(b *bytes.Buffer, name string, items []string) {
	b.WriteString("   <" + name + ">\n    <rdf:Bag>\n")
	for _, it := range items {
		b.WriteString("     <rdf:li>")
		escape(b, it)
		b.WriteString("</rdf:li>\n")
	}
	b.WriteString("    </rdf:Bag>\n   </" + name + ">\n")
}

// writeRegions writes the MWG region list: one Face region per box, centre
// based and normalised, as the specification has them.
func writeRegions(b *bytes.Buffer, d Doc, regions []Face) {
	b.WriteString("   <mwg-rs:Regions rdf:parseType=\"Resource\">\n")
	b.WriteString("    <mwg-rs:AppliedToDimensions stDim:w=\"" + strconv.Itoa(d.Width) +
		"\" stDim:h=\"" + strconv.Itoa(d.Height) + "\" stDim:unit=\"pixel\"/>\n")
	b.WriteString("    <mwg-rs:RegionList>\n     <rdf:Bag>\n")
	for _, r := range regions {
		b.WriteString("      <rdf:li>\n       <rdf:Description mwg-rs:Name=\"")
		escape(b, r.Name)
		b.WriteString("\" mwg-rs:Type=\"Face\">\n")
		b.WriteString("        <mwg-rs:Area stArea:x=\"" + number(r.X+r.W/2) + "\" stArea:y=\"" + number(r.Y+r.H/2) +
			"\" stArea:w=\"" + number(r.W) + "\" stArea:h=\"" + number(r.H) + "\" stArea:unit=\"" + areaUnit + "\"/>\n")
		b.WriteString("       </rdf:Description>\n      </rdf:li>\n")
	}
	b.WriteString("     </rdf:Bag>\n    </mwg-rs:RegionList>\n   </mwg-rs:Regions>\n")
}

// regionsOf is the faces that get a region: named, with a box inside the
// photo, and only when the photo's size is known. They are in reading order,
// so the output does not depend on the order the store gave them in.
func regionsOf(d Doc) []Face {
	if d.Width <= 0 || d.Height <= 0 {
		return nil
	}
	var out []Face
	for _, f := range d.Faces {
		f.Name = strings.TrimSpace(f.Name)
		if f.Name == "" || !validBox(f) {
			continue
		}
		if f = clampBox(f); f.W > 0 && f.H > 0 {
			out = append(out, f)
		}
	}
	slices.SortStableFunc(out, func(a, b Face) int {
		return cmp.Or(cmp.Compare(a.X, b.X), cmp.Compare(a.Y, b.Y), strings.Compare(a.Name, b.Name))
	})
	return out
}

// validBox reports whether f is a finite box.
func validBox(f Face) bool {
	for _, v := range []float64{f.X, f.Y, f.W, f.H} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

// clampBox keeps the box inside the photo, as a detector's can overhang an
// edge by a pixel or two.
func clampBox(f Face) Face {
	x0, y0 := min(max(f.X, 0), 1), min(max(f.Y, 0), 1)
	x1, y1 := min(max(f.X+f.W, 0), 1), min(max(f.Y+f.H, 0), 1)
	f.X, f.Y, f.W, f.H = x0, y0, x1-x0, y1-y0
	return f
}

// faceNames lists the names of faces.
func faceNames(faces []Face) []string {
	names := make([]string, len(faces))
	for i, f := range faces {
		names[i] = f.Name
	}
	return names
}

// unique returns the non-blank entries of in, sorted, one for each spelling
// that core.Fold treats as the same (the first in sort order is kept).
func unique(in []string) []string {
	sorted := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			sorted = append(sorted, s)
		}
	}
	slices.Sort(sorted)
	seen := make(map[string]bool, len(sorted))
	out := sorted[:0]
	for _, s := range sorted {
		if k := core.Fold(s); !seen[k] {
			seen[k] = true
			out = append(out, s)
		}
	}
	return out
}

// number formats a fraction to six places, without trailing zeros, so the
// same box always gives the same text.
func number(v float64) string {
	return strconv.FormatFloat(math.Round(v*1e6)/1e6, 'f', -1, 64)
}

// escape writes s as XML text or attribute content. Characters XML cannot
// carry become U+FFFD; quotes and line breaks become character references.
func escape(b *bytes.Buffer, s string) {
	_ = xml.EscapeText(b, []byte(s)) // writing to a bytes.Buffer cannot fail
}
