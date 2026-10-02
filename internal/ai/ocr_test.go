package ai

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/inconsolata"
	"golang.org/x/image/math/fixed"

	"lumen/internal/testenv"
)

func openOCR(t *testing.T) *OCR {
	t.Helper()
	rt := testenv.Runtime(t)
	if err := Init(rt); err != nil {
		t.Fatal(err)
	}
	o, err := OpenOCR(filepath.Join(rt, "models", "ocr"), 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(o.Close)
	return o
}

// screenshotLine is one line of text drawn on a test screenshot.
type screenshotLine struct {
	x, y  int // the top left of the line, in pixels
	scale int // the 8 x 16 font's size multiple: 2 gives text 32 px high
	text  string
}

// drawScreenshot draws lines in Inconsolata's bitmap font, enlarged smoothly,
// on a plain background, like a phone's settings screen, and returns it as
// 8-bit RGB. (The outline fonts need golang.org/x/text, which the module does
// not use.)
func drawScreenshot(w, h int, bg, fg color.RGBA, lines []screenshotLine) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)
	face := inconsolata.Regular8x16
	for _, l := range lines {
		small := image.NewRGBA(image.Rect(0, 0, font.MeasureString(face, l.text).Ceil(), 16))
		draw.Draw(small, small.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)
		d := font.Drawer{Dst: small, Src: image.NewUniform(fg), Face: face, Dot: fixed.P(0, 13)}
		d.DrawString(l.text)
		to := image.Rect(l.x, l.y, l.x+small.Bounds().Dx()*l.scale, l.y+16*l.scale)
		xdraw.CatmullRom.Scale(img, to, small, small.Bounds(), draw.Over, nil)
	}
	pix := make([]byte, 0, w*h*3)
	for i := 0; i < len(img.Pix); i += 4 {
		pix = append(pix, img.Pix[i], img.Pix[i+1], img.Pix[i+2])
	}
	return pix
}

func texts(lines []OCRLine) []string {
	var all []string
	for _, l := range lines {
		all = append(all, l.Text)
	}
	return all
}

func words(lines []OCRLine) []string {
	var all []string
	for _, l := range lines {
		all = append(all, strings.Fields(strings.ToLower(l.Text))...)
	}
	return all
}

// The model test draws its screenshots, rather than keeping a PNG of one, so
// what it reads is known to the letter.
func TestOCRReadsTheWordsOfAScreenshot(t *testing.T) {
	o := openOCR(t)
	white, ink := color.RGBA{255, 255, 255, 255}, color.RGBA{20, 20, 20, 255}
	pix := drawScreenshot(1080, 900, white, ink, []screenshotLine{
		{60, 60, 4, "Wireless network"},
		{60, 170, 3, "Guest password: sunflower42"},
		{60, 260, 3, "Invoice total due on Friday"},
		{60, 350, 2, "Tap here to share the booking reference"},
		{60, 420, 2, "Receipt number 90417 for the repair"},
	})
	lines, err := o.Read(t.Context(), pix, 1080, 900)
	if err != nil {
		t.Fatal(err)
	}
	got := words(lines)
	for _, want := range []string{"wireless", "network", "password:", "sunflower42", "invoice", "friday", "booking", "reference", "receipt", "90417", "repair"} {
		if !slices.Contains(got, want) {
			t.Errorf("word %q not read; got lines %q", want, texts(lines))
		}
	}
	// The lines come back top to bottom.
	for i := 1; i < len(lines); i++ {
		if lines[i].Box.Min.Y < lines[i-1].Box.Min.Y-lines[i-1].Box.Dy() {
			t.Errorf("line %d (%q) is above line %d (%q)", i, lines[i].Text, i-1, lines[i-1].Text)
		}
	}
}

func TestOCRReadsLightTextOnADarkScreen(t *testing.T) {
	o := openOCR(t)
	pix := drawScreenshot(720, 400, color.RGBA{18, 18, 24, 255}, color.RGBA{230, 230, 235, 255}, []screenshotLine{
		{40, 80, 3, "Battery saver is on"},
		{40, 200, 3, "Storage almost full"},
	})
	lines, err := o.Read(t.Context(), pix, 720, 400)
	if err != nil {
		t.Fatal(err)
	}
	got := words(lines)
	for _, want := range []string{"battery", "saver", "storage", "almost", "full"} {
		if !slices.Contains(got, want) {
			t.Errorf("word %q not read; got %q", want, texts(lines))
		}
	}
}

func TestOCRFindsNothingInAnImageWithNoText(t *testing.T) {
	o := openOCR(t)
	pix := drawScreenshot(400, 300, color.RGBA{90, 140, 200, 255}, color.RGBA{90, 140, 200, 255}, nil)
	lines, err := o.Read(t.Context(), pix, 400, 300)
	if err != nil || len(lines) != 0 {
		t.Fatalf("a plain image gave %q, %v", texts(lines), err)
	}
}

func TestOCRStopsWhenItsContextEnds(t *testing.T) {
	o := openOCR(t)
	pix := drawScreenshot(600, 200, color.RGBA{255, 255, 255, 255}, color.RGBA{0, 0, 0, 255}, []screenshotLine{{20, 60, 3, "Cancelled read"}})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := o.Read(ctx, pix, 600, 200); !errors.Is(err, context.Canceled) {
		t.Fatalf("Read with a cancelled context: %v", err)
	}
}

func TestOCRRefusesPixelsThatAreNotAnImage(t *testing.T) {
	o := &OCR{}
	if _, err := o.Read(t.Context(), make([]byte, 10), 4, 4); !errors.Is(err, ErrBadImage) {
		t.Fatalf("short pixels: %v, want ErrBadImage", err)
	}
	if _, err := o.Read(t.Context(), nil, 0, 0); !errors.Is(err, ErrBadImage) {
		t.Fatalf("no image: %v, want ErrBadImage", err)
	}
}

func TestOCRModelsAreOnlyLoadedFromTheirFolder(t *testing.T) {
	if _, err := OpenOCR(t.TempDir(), 1); err == nil {
		t.Fatal("OpenOCR loaded models from an empty folder")
	}
}

// A recogniser with a blank and three characters ("a", "b" and the space).
func decodeFixture() *OCR { return &OCR{dict: []string{"a", "b"}, classes: 4} }

// probRow is one step of recogniser output: probability 0.9 for class c.
func probRow(c int) []float32 {
	row := make([]float32, 4)
	for i := range row {
		row[i] = 0.1 / 3
	}
	row[c] = 0.9
	return row
}

func TestDecodeJoinsRepeatsAndDropsBlanks(t *testing.T) {
	var probs []float32
	// Class 1 twice is one "a", a blank then splits the next, "b" twice is one, and the space ends it.
	for _, c := range []int{1, 1, 0, 1, 2, 2, 3} {
		probs = append(probs, probRow(c)...)
	}
	text, conf, err := decodeFixture().decode(probs, 1)
	if err != nil || len(text) != 1 || text[0] != "aab" {
		t.Fatalf("decoded %q, %v; want \"aab\"", text, err)
	}
	if want := float32(0.9); math.Abs(float64(conf[0]-want)) > 1e-6 {
		t.Errorf("confidence = %v, want %v", conf[0], want)
	}
}

func TestDecodeRejectsOutputItCannotRead(t *testing.T) {
	o := decodeFixture()
	good := append(probRow(1), probRow(2)...)
	if _, _, err := o.decode(good, 1); err != nil {
		t.Fatalf("valid output rejected: %v", err)
	}
	for name, probs := range map[string][]float32{
		"empty":         nil,
		"wrong shape":   good[:7],
		"not per line":  good, // eight values are no whole number of steps for three lines
		"NaN":           {float32(math.NaN()), 0, 0, 0},
		"infinite":      {0, float32(math.Inf(1)), 0, 0},
		"negative infs": {0, 0, float32(math.Inf(-1)), 0},
	} {
		n := 1
		if name == "not per line" {
			n = 3
		}
		if _, _, err := o.decode(probs, n); !errors.Is(err, ErrBadOutput) {
			t.Errorf("%s: err = %v, want ErrBadOutput", name, err)
		}
	}
}

func TestTextBoxesKeepsSureRegionsAndGrowsThem(t *testing.T) {
	const w, h = 64, 32
	prob := make([]float32, w*h)
	fill := func(x0, y0, x1, y1 int, p float32) {
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				prob[y*w+x] = p
			}
		}
	}
	fill(10, 10, 30, 16, 0.9) // a sure line
	fill(40, 4, 60, 10, 0.4)  // above the threshold, but not sure enough
	fill(2, 25, 4, 27, 0.9)   // too thin to be text
	boxes := textBoxes(prob, w, h, 2*w, 2*h)
	if len(boxes) != 1 {
		t.Fatalf("boxes = %v, want one", boxes)
	}
	// The 20 x 6 region at (10, 10) grows by 120 * 1.5 / 52 = 3.46 each way,
	// and the map is half the image's size.
	if want := image.Rect(13, 13, 66, 38); boxes[0] != want {
		t.Errorf("box = %v, want %v", boxes[0], want)
	}
}

func TestTextBoxesKeepsTheLargestWhenThereAreTooMany(t *testing.T) {
	const w, h = 4000, 8
	prob := make([]float32, w*h)
	// 600 regions 4 px apart; every one is 3 x 3 except the last, which is
	// bigger and so must survive.
	for i := range 600 {
		side := 3
		if i == 599 {
			side = 4
		}
		for y := range side {
			for x := range side {
				prob[y*w+i*6+x] = 0.9
			}
		}
	}
	boxes := textBoxes(prob, w, h, w, h)
	if len(boxes) != ocrMaxLines {
		t.Fatalf("got %d boxes, want %d", len(boxes), ocrMaxLines)
	}
	if !slices.ContainsFunc(boxes, func(b image.Rectangle) bool { return b.Min.X >= 599*6-2 }) {
		t.Error("the largest region was dropped")
	}
}

func TestReadingOrderGroupsRowsAndReadsThemLeftToRight(t *testing.T) {
	line := func(text string, x0, y0, x1, y1 int) OCRLine {
		return OCRLine{Text: text, Box: image.Rect(x0, y0, x1, y1)}
	}
	got := readingOrder([]OCRLine{
		line("third", 10, 100, 80, 120),
		line("two", 200, 52, 260, 74), // a little lower than "one", the same row
		line("one", 10, 50, 80, 70),
		line("four", 150, 101, 220, 121),
	})
	var texts []string
	for _, l := range got {
		texts = append(texts, l.Text)
	}
	if want := []string{"one", "two", "third", "four"}; !slices.Equal(texts, want) {
		t.Errorf("reading order = %v, want %v", texts, want)
	}
}
