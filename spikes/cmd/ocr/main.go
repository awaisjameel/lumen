// Spike: PP-OCRv5 (mobile or server detector + English mobile recognizer) on
// screenshots: time per image, lines found, and sample text to eyeball.
package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	ort "github.com/yalue/onnxruntime_go"

	"lumen/spikes/corpus"
	"lumen/spikes/onnx"
	"lumen/spikes/vips"
)

const (
	models = ".cache/models/ocr"
	recH   = 48
)

var recBatch = 4

type box struct{ x0, y0, x1, y1 int } // in full-image pixels

type line struct {
	b    box
	text string
	conf float32
}

func main() {
	dir := flag.String("dir", "", "screenshot folder (required)")
	n := flag.Int("n", 50, "images")
	detModel := flag.String("det", "mobile-det.onnx", "detector (mobile-det.onnx or det.onnx = server)")
	long := flag.Int("long", 960, "detector input long side (PaddleOCR default 960)")
	ep := flag.String("ep", "cpu", "execution provider")
	show := flag.Int("show", 2, "print the text of this many images")
	flag.IntVar(&recBatch, "rb", 4, "recognizer batch size (lines)")
	flag.Parse()
	if *dir == "" {
		fmt.Println("-dir is required")
		os.Exit(2)
	}
	vips.Init()

	corpus.Folders = []string{*dir}
	paths := corpus.Images(*n, ".png", ".jpg", ".jpeg")
	corpus.Warm(paths)
	dict := loadDict(filepath.Join(models, "en-dict.txt"))

	det, err := onnx.Open(filepath.Join(models, *detModel), *ep, 0)
	check(err)
	rec, err := onnx.Open(filepath.Join(models, "en-rec.onnx"), *ep, 0)
	check(err)

	var tDec, tDet, tRec time.Duration
	lines, chars := 0, 0
	for i, p := range paths {
		t := time.Now()
		pix, w, h, err := vips.RGBFit(p, 2560)
		if err != nil {
			continue
		}
		tDec += time.Since(t)
		t = time.Now()
		boxes := detect(det, pix, w, h, *long)
		tDet += time.Since(t)
		t = time.Now()
		ls := recognize(rec, dict, pix, w, h, boxes)
		tRec += time.Since(t)
		lines += len(ls)
		for _, l := range ls {
			chars += len(l.text)
		}
		if i < *show {
			fmt.Printf("\n== %s (%dx%d, %d lines)\n", filepath.Base(p), w, h, len(ls))
			for _, l := range ls[:min(len(ls), 25)] {
				fmt.Printf("   %.2f  %s\n", l.conf, l.text)
			}
		}
	}
	k := float64(len(paths))
	fmt.Printf("\n%s · %s · long side %d · %d images: decode %.0f ms, detect %.0f ms, recognize %.0f ms per image · %.0f lines, %.0f chars per image\n",
		*detModel, *ep, *long, len(paths), ms(tDec)/k, ms(tDet)/k, ms(tRec)/k, float64(lines)/k, float64(chars)/k)
}

// detect runs the DB text detector and returns axis-aligned line boxes.
func detect(s *onnx.Session, pix []byte, w, h, long int) []box {
	scale := math.Min(1, float64(long)/float64(max(w, h)))
	dw := max(32, int(math.Round(float64(w)*scale/32))*32)
	dh := max(32, int(math.Round(float64(h)*scale/32))*32)
	sx, sy := float64(w)/float64(dw), float64(h)/float64(dh)
	plane := dw * dh
	data := make([]float32, 3*plane)
	mean := [3]float32{0.485, 0.456, 0.406}
	std := [3]float32{0.229, 0.224, 0.225}
	for y := range dh {
		yy := min(h-1, int(float64(y)*sy))
		for x := range dw {
			xx := min(w-1, int(float64(x)*sx))
			o := (yy*w + xx) * 3
			i := y*dw + x
			for c := range 3 { // BGR order, as PaddleOCR reads with OpenCV
				v := float32(pix[o+2-c]) / 255
				data[c*plane+i] = (v - mean[c]) / std[c]
			}
		}
	}
	in, err := ort.NewTensor(ort.NewShape(1, 3, int64(dh), int64(dw)), data)
	check(err)
	defer in.Destroy()
	outs, err := s.Run(in)
	check(err)
	defer onnx.Destroy(outs...)
	prob, _, err := onnx.Float32(outs[0])
	check(err)

	// Connected components of prob > 0.3; keep those whose mean prob >= 0.6.
	seen := make([]bool, plane)
	var boxes []box
	stack := make([]int, 0, 1024)
	for start := range plane {
		if seen[start] || prob[start] <= 0.3 {
			continue
		}
		x0, y0, x1, y1 := dw, dh, 0, 0
		var sum float64
		cnt := 0
		stack = append(stack[:0], start)
		seen[start] = true
		for len(stack) > 0 {
			i := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			x, y := i%dw, i/dw
			x0, y0, x1, y1 = min(x0, x), min(y0, y), max(x1, x), max(y1, y)
			sum += float64(prob[i])
			cnt++
			for _, j := range [4]int{i - 1, i + 1, i - dw, i + dw} {
				if j < 0 || j >= plane || seen[j] || prob[j] <= 0.3 || (j == i-1 && x == 0) || (j == i+1 && x == dw-1) {
					continue
				}
				seen[j] = true
				stack = append(stack, j)
			}
		}
		bw, bh := x1-x0+1, y1-y0+1
		if min(bw, bh) < 3 || sum/float64(cnt) < 0.6 {
			continue
		}
		// DB "unclip": grow by area * 1.5 / perimeter.
		d := float64(bw*bh) * 1.5 / float64(2*(bw+bh))
		boxes = append(boxes, box{
			x0: max(0, int((float64(x0)-d)*sx)), y0: max(0, int((float64(y0)-d)*sy)),
			x1: min(w, int((float64(x1+1)+d)*sx)), y1: min(h, int((float64(y1+1)+d)*sy)),
		})
	}
	return boxes
}

// recognize crops each box to height 48 and CTC-decodes the recognizer output,
// batching boxes of similar aspect ratio.
func recognize(s *onnx.Session, dict []string, pix []byte, w, h int, boxes []box) []line {
	ratio := func(b box) float64 { return float64(b.x1-b.x0) / float64(max(1, b.y1-b.y0)) }
	sort.Slice(boxes, func(i, j int) bool { return ratio(boxes[i]) < ratio(boxes[j]) })
	var out []line
	for lo := 0; lo < len(boxes); lo += recBatch {
		bs := boxes[lo:min(lo+recBatch, len(boxes))]
		maxR := 320.0 / recH
		for _, b := range bs {
			maxR = math.Max(maxR, ratio(b))
		}
		W := int(math.Ceil(recH * maxR))
		plane := recH * W
		data := make([]float32, len(bs)*3*plane) // zero = padding
		for k, b := range bs {
			cw := min(W, int(math.Ceil(recH*ratio(b))))
			bw, bh := float64(b.x1-b.x0), float64(b.y1-b.y0)
			for y := range recH {
				yy := min(h-1, b.y0+int(float64(y)*bh/recH))
				for x := range cw {
					xx := min(w-1, b.x0+int(float64(x)*bw/float64(cw)))
					o := (yy*w + xx) * 3
					for c := range 3 {
						data[k*3*plane+c*plane+y*W+x] = float32(pix[o+2-c])/127.5 - 1
					}
				}
			}
		}
		in, err := ort.NewTensor(ort.NewShape(int64(len(bs)), 3, recH, int64(W)), data)
		check(err)
		outs, err := s.Run(in)
		in.Destroy()
		check(err)
		probs, shape, err := onnx.Float32(outs[0])
		onnx.Destroy(outs...)
		check(err)
		T, C := int(shape[1]), int(shape[2])
		for k, b := range bs {
			var sb strings.Builder
			prev := 0
			var conf float64
			nc := 0
			for t := range T {
				row := probs[(k*T+t)*C : (k*T+t+1)*C]
				best := 0
				for c := range row {
					if row[c] > row[best] {
						best = c
					}
				}
				if best != 0 && best != prev {
					if best-1 < len(dict) {
						sb.WriteString(dict[best-1])
					} else {
						sb.WriteByte(' ')
					}
					conf += float64(row[best])
					nc++
				}
				prev = best
			}
			if txt := strings.TrimSpace(sb.String()); txt != "" {
				out = append(out, line{b: b, text: txt, conf: float32(conf / float64(max(1, nc)))})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if abs(out[i].b.y0-out[j].b.y0) > 8 {
			return out[i].b.y0 < out[j].b.y0
		}
		return out[i].b.x0 < out[j].b.x0
	})
	return out
}

func loadDict(path string) []string {
	b, err := os.ReadFile(path)
	check(err)
	return strings.Split(strings.TrimRight(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n"), "\n")
}

func abs(x int) int { return max(x, -x) }

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func check(err error) {
	if err != nil {
		panic(err)
	}
}
