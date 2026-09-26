// Spike: SigLIP 2 base on ~1,000 local images with ONNX Runtime on DirectML
// and CPU: preprocessing and inference throughput, fp32 vs fp16 vs int8
// agreement, text-query latency, and contact sheets of search results to
// judge quality by eye.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	ort "github.com/yalue/onnxruntime_go"

	"lumen/spikes/corpus"
	"lumen/spikes/onnx"
	"lumen/spikes/siglip"
	"lumen/spikes/vips"
)

const (
	models = ".cache/models/siglip2"
	side   = 224
)

var queries = []string{
	"people in an office",
	"a man sitting at a desk with a laptop",
	"a group photo",
	"a plant in a pot",
	"a screenshot of source code",
	"a spreadsheet",
	"a chat conversation on a phone",
	"a web page with a login form",
	"a map",
	"a bar chart",
	"a beach at sunset",
	"food on a plate",
}

func main() {
	n := flag.Int("n", 1000, "images")
	eps := flag.String("ep", "dml,cpu", "execution providers to benchmark")
	variants := flag.String("vision", "fp32,fp16,int8", "vision model variants")
	textVariant := flag.String("text", "int8", "text model variant")
	textCmp := flag.String("textcmp", "", "another text variant to compare embeddings with")
	batch := flag.Int("batch", 16, "images per inference batch")
	out := flag.String("out", "out/siglip", "output folder")
	flag.Parse()
	os.MkdirAll(*out, 0o755)
	vips.Init()
	vips.SetConcurrency(1)
	fmt.Println("ONNX Runtime", onnx.Version(), "· libvips", vips.Version())

	paths := corpus.Images(*n)
	corpus.Warm(paths)

	// 1. Preprocess: decode + squash to 224x224 RGB, in parallel.
	t0 := time.Now()
	pix := make([][]byte, len(paths))
	parallel(len(paths), runtime.NumCPU(), func(i int) {
		b, err := vips.RGB(paths[i], side, side)
		if err == nil {
			pix[i] = b
		}
	})
	var ok []int
	for i, p := range pix {
		if p != nil {
			ok = append(ok, i)
		}
	}
	d := time.Since(t0)
	fmt.Printf("preprocess (decode + resize to %d, %d threads): %d/%d images in %.1fs = %.0f images/s\n",
		side, runtime.NumCPU(), len(ok), len(paths), d.Seconds(), float64(len(ok))/d.Seconds())

	// 2. Vision tower throughput per provider and variant.
	embeds := map[string][][]float32{}
	for _, ep := range strings.Split(*eps, ",") {
		for _, v := range strings.Split(*variants, ",") {
			file := filepath.Join(models, visionFiles[v])
			key := ep + "/" + v
			t := time.Now()
			s, err := onnx.Open(file, ep, 0)
			if err != nil {
				fmt.Printf("%-9s open failed: %v\n", key, err)
				continue
			}
			load := time.Since(t)
			if len(embeds) == 0 {
				fmt.Print(s.Describe())
			}
			e, rate, err := visionRun(s, pix, ok, *batch)
			s.Destroy()
			if err != nil {
				fmt.Printf("%-9s run failed: %v\n", key, err)
				continue
			}
			embeds[key] = e
			fmt.Printf("%-9s load %4.1fs · %5.1f images/s (batch %d)\n", key, load.Seconds(), rate, *batch)
		}
	}

	// 3. How far quantized / GPU results drift from CPU fp32.
	ref := embeds["cpu/fp32"]
	if ref == nil {
		ref = embeds["dml/fp32"]
	}
	for key, e := range embeds {
		if ref != nil {
			mean, lo := meanCos(ref, e)
			fmt.Printf("agreement %-9s vs fp32: mean cosine %.4f, min %.4f\n", key, mean, lo)
		}
	}
	if ref == nil {
		for _, e := range embeds {
			ref = e
			break
		}
	}
	if ref == nil {
		return
	}

	// 4. Text tower: tokenizer + encoder latency per query, then search.
	t := time.Now()
	tok, err := siglip.LoadTokenizer(filepath.Join(models, "tokenizer.json"))
	check(err)
	fmt.Printf("tokenizer load %.1fs\n", time.Since(t).Seconds())
	textFile := filepath.Join(models, map[string]string{"fp32": "text_model.onnx", "fp16": "text_model_fp16.onnx", "int8": "text_model_int8.onnx"}[*textVariant])
	ts, err := onnx.Open(textFile, "cpu", 0)
	check(err)
	fmt.Print(ts.Describe())
	var lat []time.Duration
	for qi, q := range queries {
		t := time.Now()
		qv := textEmbed(ts, tok, q)
		hits := top(ref, qv, 8)
		lat = append(lat, time.Since(t))
		names := make([]string, len(hits))
		sheet := make([]string, len(hits))
		for i, h := range hits {
			sheet[i] = paths[ok[h.i]]
			names[i] = fmt.Sprintf("%s (%.3f)", filepath.Base(sheet[i]), h.s)
		}
		fmt.Printf("%q:\n    %s\n", q, strings.Join(names, ", "))
		contactSheet(filepath.Join(*out, fmt.Sprintf("q%02d.jpg", qi)), sheet)
	}
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	if *textCmp != "" {
		other, err := onnx.Open(filepath.Join(models, map[string]string{"fp32": "text_model.onnx", "fp16": "text_model_fp16.onnx", "int8": "text_model_int8.onnx"}[*textCmp]), "cpu", 0)
		check(err)
		var a, b [][]float32
		sameTop := 0
		for _, q := range queries {
			va, vb := textEmbed(ts, tok, q), textEmbed(other, tok, q)
			a, b = append(a, va), append(b, vb)
			if top(ref, va, 1)[0].i == top(ref, vb, 1)[0].i {
				sameTop++
			}
		}
		mean, lo := meanCos(a, b)
		fmt.Printf("text %s vs %s: mean cosine %.4f, min %.4f; same top hit for %d/%d queries\n", *textVariant, *textCmp, mean, lo, sameTop, len(queries))
	}
	fmt.Printf("query latency (tokenize + text encoder on CPU + scan of %d): median %d ms, max %d ms\n",
		len(ref), lat[len(lat)/2].Milliseconds(), lat[len(lat)-1].Milliseconds())
}

// The *s variants are the same models with the input fixed to 16x3x224x224,
// which lets DirectML compile the whole graph.
var visionFiles = map[string]string{
	"fp32": "vision_model.onnx", "fp16": "vision_model_fp16.onnx", "int8": "vision_model_int8.onnx",
	"fp32s": "vision_model_b16.onnx", "fp16s": "vision_model_fp16_b16.onnx", "int8s": "vision_model_int8_b16.onnx",
}

// visionRun embeds every ok image and returns unit vectors in ok order.
func visionRun(s *onnx.Session, pix [][]byte, ok []int, batch int) ([][]float32, float64, error) {
	out := make([][]float32, 0, len(ok))
	var busy time.Duration
	for lo := 0; lo < len(ok); lo += batch {
		idx := ok[lo:min(lo+batch, len(ok))]
		data := make([]float32, batch*3*side*side) // last batch zero-padded
		for b, i := range idx {
			p := pix[i]
			plane := side * side
			base := b * 3 * plane
			for px := range plane {
				for c := range 3 {
					data[base+c*plane+px] = float32(p[px*3+c])/127.5 - 1 // (x/255-0.5)/0.5
				}
			}
		}
		in, err := ort.NewTensor(ort.NewShape(int64(batch), 3, side, side), data)
		if err != nil {
			return nil, 0, err
		}
		t := time.Now()
		outs, err := s.Run(in)
		if lo > 0 { // first batch includes one-off graph setup
			busy += time.Since(t)
		}
		in.Destroy()
		if err != nil {
			return nil, 0, err
		}
		v, err := pooled(s, outs)
		onnx.Destroy(outs...)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, v[:len(idx)]...)
	}
	return out, float64(len(ok)-min(batch, len(ok))) / busy.Seconds(), nil
}

// pooled picks the pooler_output and splits it into unit vectors.
func pooled(s *onnx.Session, outs []ort.Value) ([][]float32, error) {
	k := len(outs) - 1
	for i, o := range s.Out {
		if o.Name == "pooler_output" || o.Name == "image_embeds" || o.Name == "text_embeds" {
			k = i
		}
	}
	data, shape, err := onnx.Float32(outs[k])
	if err != nil {
		return nil, err
	}
	dim := int(shape[len(shape)-1])
	var vs [][]float32
	for i := 0; i+dim <= len(data); i += dim {
		vs = append(vs, unit(data[i:i+dim]))
	}
	return vs, nil
}

func textEmbed(s *onnx.Session, tok *siglip.Tokenizer, q string) []float32 {
	ids := tok.Encode(q)
	in, err := ort.NewTensor(ort.NewShape(1, int64(len(ids))), ids)
	check(err)
	defer in.Destroy()
	outs, err := s.Run(in)
	check(err)
	defer onnx.Destroy(outs...)
	v, err := pooled(s, outs)
	check(err)
	return v[0]
}

type hit struct {
	i int
	s float32
}

func top(vs [][]float32, q []float32, k int) []hit {
	hs := make([]hit, len(vs))
	for i, v := range vs {
		hs[i] = hit{i, dot(v, q)}
	}
	sort.Slice(hs, func(a, b int) bool { return hs[a].s > hs[b].s })
	return hs[:min(k, len(hs))]
}

func meanCos(a, b [][]float32) (float64, float64) {
	sum, lo := 0.0, 1.0
	for i := range min(len(a), len(b)) {
		c := float64(dot(a[i], b[i]))
		sum += c
		lo = math.Min(lo, c)
	}
	return sum / float64(min(len(a), len(b))), lo
}

func dot(a, b []float32) float32 {
	var s float32
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}

func unit(v []float32) []float32 {
	var n float64
	for _, x := range v {
		n += float64(x) * float64(x)
	}
	n = math.Sqrt(n)
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = float32(float64(x) / n)
	}
	return out
}

// contactSheet tiles 160 px thumbnails of paths into one JPEG.
func contactSheet(dst string, paths []string) {
	const cell = 160
	sheet := image.NewRGBA(image.Rect(0, 0, cell*len(paths), cell))
	draw.Draw(sheet, sheet.Bounds(), image.Black, image.Point{}, draw.Src)
	for i, p := range paths {
		_, small, _, _, err := vips.Thumbs(p, cell, cell)
		if err != nil {
			continue
		}
		img, err := jpeg.Decode(bytes.NewReader(small))
		if err != nil {
			continue
		}
		b := img.Bounds()
		off := image.Pt(i*cell+(cell-b.Dx())/2, (cell-b.Dy())/2)
		draw.Draw(sheet, b.Add(off).Sub(b.Min), img, b.Min, draw.Src)
	}
	f, err := os.Create(dst)
	check(err)
	defer f.Close()
	jpeg.Encode(f, sheet, &jpeg.Options{Quality: 85})
}

func parallel(n, workers int, fn func(int)) {
	ch := make(chan int)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for i := range ch {
				fn(i)
			}
		})
	}
	for i := range n {
		ch <- i
	}
	close(ch)
	wg.Wait()
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}
