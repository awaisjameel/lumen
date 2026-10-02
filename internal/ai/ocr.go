package ai

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	ort "github.com/yalue/onnxruntime_go"
)

// OCRVersion is the version of what Lumen reads from a photo's text: the
// PP-OCRv5 mobile detector with the English mobile recogniser (deps.sh pins
// both), and the way this file uses them. A change re-runs the OCR stage.
const OCRVersion = "ppocrv5-mobile-en-1"

// The detector and recogniser settings are the ones Phase 0 measured
// (spikes/README.md, "OCR: PP-OCRv5") and PaddleOCR's own defaults.
const (
	ocrDetectSide = 960  // the detector's input long side; smaller images are not enlarged
	ocrBinarise   = 0.3  // a detector pixel counts as text above this probability
	ocrBoxScore   = 0.6  // a text region's mean probability must reach this
	ocrUnclip     = 1.5  // the detector draws text shrunk; boxes grow back by area * ocrUnclip / perimeter
	ocrMinBox     = 3    // detector pixels: a region thinner than this is noise
	ocrMaxLines   = 500  // the most regions read from one image (the largest), to bound the time
	ocrRecHeight  = 48   // the recogniser's input height
	ocrRecMinW    = 320  // and its least width, which a batch of short lines is padded to
	ocrRecMaxW    = 4096 // and the most: a longer line is squeezed to fit
	ocrRecBatch   = 4    // lines per recogniser run (4 was quickest on a CPU)
	ocrDropScore  = 0.5  // a line whose characters average less confidence than this is noise
)

// ErrBadOutput is what OCR.Read returns when a model gave output that can't
// be read (NaN, or a shape the model should not produce). Like ErrBadImage
// it is a problem of that image: the next one may read fine.
var ErrBadOutput = errors.New("ocr: the model gave no usable result")

// OCRLine is one line of text found in an image.
type OCRLine struct {
	Box  image.Rectangle // where it is, in the image's pixels
	Text string
	Conf float32 // the characters' mean confidence, 0..1
}

// OCR reads the text in images: PP-OCRv5's detector finds the lines, the
// English recogniser reads each. Both run on the CPU, with ONNX Runtime's
// worker threads not spinning (see openSession): it is background work on a
// few images in a library, where the GPU is the image model's. Safe for
// concurrent use.
type OCR struct {
	det, rec *session
	dict     []string // the recogniser's characters, after its blank (class 0)
	classes  int      // the recogniser's classes: blank, the dictionary, and a space
}

// OpenOCR loads the OCR models in dir (det.onnx, rec.onnx and rec.dict.txt).
// threads limits each model's CPU threads.
func OpenOCR(dir string, threads int) (*OCR, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "rec.dict.txt"))
	if err != nil {
		return nil, fmt.Errorf("OCR models missing: %w", err)
	}
	// One character per line (a line may itself be a space), then the
	// recogniser's own space class.
	dict := strings.Split(strings.TrimSuffix(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n"), "\n")
	if len(dict) < 2 {
		return nil, fmt.Errorf("OCR dictionary is empty (%d lines)", len(dict))
	}
	o := &OCR{dict: dict, classes: len(dict) + 2}
	if o.det, err = openSession(filepath.Join(dir, "det.onnx"), CPU, threads); err != nil {
		return nil, err
	}
	if o.rec, err = openSession(filepath.Join(dir, "rec.onnx"), CPU, threads); err != nil {
		o.det.close()
		return nil, err
	}
	// A blank line through the recogniser proves its classes are the
	// dictionary's before any photo is read with the wrong one.
	if _, _, err := o.recognise(make([]float32, 3*ocrRecHeight*ocrRecMinW), 1, ocrRecMinW); err != nil {
		o.Close()
		return nil, fmt.Errorf("check OCR models: %w", err)
	}
	return o, nil
}

// Close frees the models.
func (o *OCR) Close() {
	o.det.close()
	o.rec.close()
}

// Read finds and reads the lines of text in an 8-bit RGB image, top to
// bottom and, within a row, left to right. Its long edge sets what size of
// text is read: UI text of 12 px reads at 2560 px, not at 1024. It stops
// between recogniser runs when ctx ends.
func (o *OCR) Read(ctx context.Context, pix []byte, w, h int) ([]OCRLine, error) {
	if err := checkRGB(pix, w, h); err != nil {
		return nil, err
	}
	boxes, err := o.detect(pix, w, h)
	if err != nil {
		return nil, err
	}
	// Similar widths together keep a batch's padding small.
	ratio := func(b image.Rectangle) float64 { return float64(b.Dx()) / float64(b.Dy()) }
	slices.SortFunc(boxes, func(a, b image.Rectangle) int { return cmp.Compare(ratio(a), ratio(b)) })
	var lines []OCRLine
	for lo := 0; lo < len(boxes); lo += ocrRecBatch {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		batch := boxes[lo:min(lo+ocrRecBatch, len(boxes))]
		maxRatio := float64(ocrRecMinW) / ocrRecHeight
		for _, b := range batch {
			maxRatio = math.Max(maxRatio, ratio(b))
		}
		width := min(ocrRecMaxW, int(math.Ceil(ocrRecHeight*maxRatio)))
		data := make([]float32, len(batch)*3*ocrRecHeight*width) // zero is the padding
		for k, b := range batch {
			cropInto(data[k*3*ocrRecHeight*width:], width, pix, w, b)
		}
		text, conf, err := o.recognise(data, len(batch), width)
		if err != nil {
			return nil, err
		}
		for k, b := range batch {
			if text[k] != "" && conf[k] >= ocrDropScore {
				lines = append(lines, OCRLine{Box: b, Text: text[k], Conf: conf[k]})
			}
		}
	}
	return readingOrder(lines), nil
}

// detect runs the DB text detector and returns a box (in pixels of the
// image) round each region it found. They are axis-aligned: screenshots and
// scans of documents are upright.
func (o *OCR) detect(pix []byte, w, h int) ([]image.Rectangle, error) {
	scale := math.Min(1, float64(ocrDetectSide)/float64(max(w, h)))
	// Sides in multiples of 32, as the detector's strides need.
	dw := max(32, int(math.Round(float64(w)*scale/32))*32)
	dh := max(32, int(math.Round(float64(h)*scale/32))*32)
	small := resample(pix, w, h, dw, dh)
	plane := dw * dh
	data := make([]float32, 3*plane)
	mean := [3]float32{0.485, 0.456, 0.406}
	std := [3]float32{0.229, 0.224, 0.225}
	for i := range plane {
		for c := range 3 { // BGR, as PaddleOCR reads images with OpenCV
			data[c*plane+i] = (float32(small[3*i+2-c])/255 - mean[c]) / std[c]
		}
	}
	in, err := ort.NewTensor(ort.NewShape(1, 3, int64(dh), int64(dw)), data)
	if err != nil {
		return nil, err
	}
	defer destroy(in)
	outs, err := o.det.run(in)
	if err != nil {
		return nil, fmt.Errorf("run text detector: %w", err)
	}
	prob := outs[0]
	if len(prob) != plane {
		return nil, fmt.Errorf("%w: detector gave %d values for a %dx%d map", ErrBadOutput, len(prob), dw, dh)
	}
	for _, p := range prob {
		if !finite(float64(p)) {
			return nil, fmt.Errorf("%w: the detector's map has a non-finite value", ErrBadOutput)
		}
	}
	return textBoxes(prob, dw, dh, w, h), nil
}

// textBoxes finds the regions of a w x h probability map: the connected
// pixels above ocrBinarise whose mean passes ocrBoxScore, each grown by
// DB's "unclip" and scaled to the imgW x imgH image. At most ocrMaxLines, the
// largest.
func textBoxes(prob []float32, w, h, imgW, imgH int) []image.Rectangle {
	sx, sy := float64(imgW)/float64(w), float64(imgH)/float64(h)
	seen := make([]bool, len(prob))
	var boxes []image.Rectangle
	var stack []int
	for start := range prob {
		if seen[start] || prob[start] <= ocrBinarise {
			continue
		}
		x0, y0, x1, y1 := w, h, 0, 0
		var sum float64
		count := 0
		stack = append(stack[:0], start)
		seen[start] = true
		for len(stack) > 0 {
			i := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			x, y := i%w, i/w
			x0, y0, x1, y1 = min(x0, x), min(y0, y), max(x1, x), max(y1, y)
			sum += float64(prob[i])
			count++
			for _, n := range [4][3]int{{x - 1, y, i - 1}, {x + 1, y, i + 1}, {x, y - 1, i - w}, {x, y + 1, i + w}} {
				if n[0] < 0 || n[0] >= w || n[1] < 0 || n[1] >= h || seen[n[2]] || prob[n[2]] <= ocrBinarise {
					continue
				}
				seen[n[2]] = true
				stack = append(stack, n[2])
			}
		}
		bw, bh := x1-x0+1, y1-y0+1
		if min(bw, bh) < ocrMinBox || sum/float64(count) < ocrBoxScore {
			continue
		}
		grow := float64(bw*bh) * ocrUnclip / float64(2*(bw+bh))
		box := image.Rect(
			max(0, int((float64(x0)-grow)*sx)), max(0, int((float64(y0)-grow)*sy)),
			min(imgW, int((float64(x1+1)+grow)*sx)), min(imgH, int((float64(y1+1)+grow)*sy)))
		if !box.Empty() {
			boxes = append(boxes, box)
		}
	}
	if len(boxes) > ocrMaxLines {
		slices.SortStableFunc(boxes, func(a, b image.Rectangle) int { return cmp.Compare(b.Dx()*b.Dy(), a.Dx()*a.Dy()) })
		boxes = boxes[:ocrMaxLines]
	}
	return boxes
}

// cropInto writes the part of an RGB image inside box, scaled to the
// recogniser's height and to its own width at that height (at most width), as
// the planar BGR input the recogniser takes, into dst: one image of a batch
// that is width wide. Where the line is shorter than the batch's width, the
// rest of dst stays as it was (zero).
func cropInto(dst []float32, width int, pix []byte, w int, box image.Rectangle) {
	bw, bh := box.Dx(), box.Dy()
	crop := make([]byte, bw*bh*3)
	for y := range bh {
		copy(crop[y*bw*3:(y+1)*bw*3], pix[((box.Min.Y+y)*w+box.Min.X)*3:])
	}
	cw := max(1, min(width, int(math.Ceil(ocrRecHeight*float64(bw)/float64(bh)))))
	scaled := resample(crop, bw, bh, cw, ocrRecHeight)
	plane := ocrRecHeight * width
	for y := range ocrRecHeight {
		for x := range cw {
			o := (y*cw + x) * 3
			for c := range 3 { // BGR
				dst[c*plane+y*width+x] = float32(scaled[o+2-c])/127.5 - 1
			}
		}
	}
}

// recognise runs the recogniser on n crops of width pixels (data, planar BGR)
// and decodes each.
func (o *OCR) recognise(data []float32, n, width int) (text []string, conf []float32, err error) {
	in, err := ort.NewTensor(ort.NewShape(int64(n), 3, ocrRecHeight, int64(width)), data)
	if err != nil {
		return nil, nil, err
	}
	defer destroy(in)
	outs, err := o.rec.run(in)
	if err != nil {
		return nil, nil, fmt.Errorf("run text recogniser: %w", err)
	}
	return o.decode(outs[0], n)
}

// decode CTC-decodes the recogniser's output for n lines: per step the
// likeliest class, and the characters where that changes to one that isn't
// the blank, with their mean probability.
func (o *OCR) decode(probs []float32, n int) (text []string, conf []float32, err error) {
	if len(probs) == 0 || len(probs)%(n*o.classes) != 0 {
		return nil, nil, fmt.Errorf("%w: recogniser gave %d values for %d lines of %d classes", ErrBadOutput, len(probs), n, o.classes)
	}
	for _, p := range probs {
		if !finite(float64(p)) {
			return nil, nil, fmt.Errorf("%w: the recogniser's output has a non-finite value", ErrBadOutput)
		}
	}
	steps := len(probs) / (n * o.classes)
	text, conf = make([]string, n), make([]float32, n)
	for k := range n {
		var sb strings.Builder
		prev := 0
		var sum float64
		chars := 0
		for t := range steps {
			row := probs[(k*steps+t)*o.classes : (k*steps+t+1)*o.classes]
			best := 0
			for c := range row {
				if row[c] > row[best] {
					best = c
				}
			}
			if best != 0 && best != prev {
				if best-1 < len(o.dict) {
					sb.WriteString(o.dict[best-1])
				} else {
					sb.WriteByte(' ') // the class after the dictionary
				}
				sum += float64(row[best])
				chars++
			}
			prev = best
		}
		text[k] = strings.TrimSpace(sb.String())
		conf[k] = float32(sum / float64(max(1, chars)))
	}
	return text, conf, nil
}

// readingOrder sorts lines top to bottom and, within a row (lines whose
// vertical middles lie inside the first one's box), left to right.
func readingOrder(lines []OCRLine) []OCRLine {
	middle := func(l OCRLine) int { return (l.Box.Min.Y + l.Box.Max.Y) / 2 }
	slices.SortStableFunc(lines, func(a, b OCRLine) int { return cmp.Compare(middle(a), middle(b)) })
	for lo := 0; lo < len(lines); {
		hi := lo + 1
		for hi < len(lines) && middle(lines[hi]) < lines[lo].Box.Max.Y {
			hi++
		}
		slices.SortStableFunc(lines[lo:hi], func(a, b OCRLine) int { return cmp.Compare(a.Box.Min.X, b.Box.Min.X) })
		lo = hi
	}
	return lines
}
