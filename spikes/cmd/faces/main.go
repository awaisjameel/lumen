// Spike: YuNet face detection + SFace embeddings + clustering on real photos.
// Writes one contact sheet of aligned face crops per cluster so the grouping
// can be judged by eye. Everything stays in spikes/out (git-ignored).
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
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
	models  = ".cache/models/faces"
	detSide = 640 // YuNet 2023mar has a fixed 640x640 input
	crop    = 112
)

// ArcFace/SFace landmark template for a 112x112 crop: eyes, nose, mouth corners.
var template = [5][2]float64{{38.2946, 51.6963}, {73.5318, 51.5014}, {56.0252, 71.7366}, {41.5493, 92.3655}, {70.7299, 92.2041}}

type face struct {
	path       string
	x, y, w, h float64 // in the decoded (<= decodeEdge) image
	score      float32
	lm         [5][2]float64
	emb        []float32
	crop       *image.RGBA
}

func main() {
	dir := flag.String("dir", "", "photo folder (required)")
	n := flag.Int("n", 300, "max images")
	edge := flag.Int("edge", 1024, "decode long edge (the app would use its 1024 px preview)")
	minScore := flag.Float64("score", 0.8, "detection score threshold")
	minSize := flag.Float64("minsize", 36, "smallest face (px, in the decoded image) to embed")
	thresholds := flag.String("t", "0.30,0.36,0.42,0.50", "cosine thresholds to report (average linkage)")
	sheetAt := flag.Float64("sheet", 0.42, "threshold whose clusters get contact sheets")
	ep := flag.String("ep", "cpu", "execution provider for both models")
	out := flag.String("out", "out/faces", "output folder")
	flag.Parse()
	if *dir == "" {
		fmt.Println("-dir is required")
		os.Exit(2)
	}
	os.RemoveAll(*out)
	os.MkdirAll(*out, 0o755)
	vips.Init()

	corpus.Folders = []string{*dir}
	paths := corpus.Images(*n, ".jpg", ".jpeg", ".png")
	corpus.Warm(paths)

	det, err := onnx.Open(filepath.Join(models, "face_detection_yunet_2023mar.onnx"), *ep, 0)
	check(err)
	rec, err := onnx.Open(filepath.Join(models, "face_recognition_sface_2021dec.onnx"), *ep, 0)
	check(err)
	fmt.Print(rec.Describe()[:min(200, len(rec.Describe()))])

	var faces []*face
	var tDecode, tDetect, tEmbed time.Duration
	var small, total int
	for _, p := range paths {
		t := time.Now()
		pix, w, h, err := vips.RGBFit(p, *edge)
		if err != nil {
			continue
		}
		tDecode += time.Since(t)
		t = time.Now()
		found := detect(det, pix, w, h, float32(*minScore))
		tDetect += time.Since(t)
		t = time.Now()
		for _, f := range found {
			total++
			if math.Min(f.w, f.h) < *minSize {
				small++
				continue
			}
			f.path = p
			f.crop = align(pix, w, h, f.lm)
			f.emb = embed(rec, f.crop)
			faces = append(faces, f)
		}
		tEmbed += time.Since(t)
	}
	fmt.Printf("%d images · %d faces detected (score >= %.2f), %d too small (< %.0f px), %d embedded\n",
		len(paths), total, *minScore, small, *minSize, len(faces))
	fmt.Printf("decode %.0f ms/img · detect %.1f ms/img · align+embed %.1f ms/face (%s)\n",
		ms(tDecode)/float64(len(paths)), ms(tDetect)/float64(len(paths)), ms(tEmbed)/math.Max(1, float64(len(faces))), *ep)

	sims := similarity(faces)
	for _, s := range strings.Split(*thresholds, ",") {
		var th float64
		fmt.Sscan(s, &th)
		t := time.Now()
		cl := cluster(sims, th)
		multi, singles, biggest := 0, 0, 0
		for _, c := range cl {
			if len(c) > 1 {
				multi++
			} else {
				singles++
			}
			biggest = max(biggest, len(c))
		}
		fmt.Printf("threshold %.2f: %d clusters with 2+ faces, %d singletons, largest %d (%.0f ms)\n",
			th, multi, singles, biggest, ms(time.Since(t)))
		if math.Abs(th-*sheetAt) < 1e-9 {
			for i, c := range cl {
				if len(c) < 2 {
					continue
				}
				sheet(filepath.Join(*out, fmt.Sprintf("cluster%02d-%dfaces.jpg", i, len(c))), faces, c)
			}
		}
	}
}

// detect runs YuNet on the image letterboxed into 640x640.
func detect(s *onnx.Session, pix []byte, w, h int, minScore float32) []*face {
	scale := float64(detSide) / float64(max(w, h))
	dw, dh := int(float64(w)*scale), int(float64(h)*scale)
	plane := detSide * detSide
	data := make([]float32, 3*plane) // BGR, 0..255, zero padding
	for y := range dh {
		sy := min(h-1, int(float64(y)/scale))
		for x := range dw {
			sx := min(w-1, int(float64(x)/scale))
			o := (sy*w + sx) * 3
			i := y*detSide + x
			data[i] = float32(pix[o+2])
			data[plane+i] = float32(pix[o+1])
			data[2*plane+i] = float32(pix[o])
		}
	}
	in, err := ort.NewTensor(ort.NewShape(1, 3, detSide, detSide), data)
	check(err)
	defer in.Destroy()
	outs, err := s.Run(in)
	check(err)
	defer onnx.Destroy(outs...)
	get := func(name string) []float32 {
		for i, o := range s.Out {
			if o.Name == name {
				v, _, err := onnx.Float32(outs[i])
				check(err)
				return v
			}
		}
		panic("missing output " + name)
	}
	var found []*face
	for _, stride := range []int{8, 16, 32} {
		cls, obj := get(fmt.Sprint("cls_", stride)), get(fmt.Sprint("obj_", stride))
		box, kps := get(fmt.Sprint("bbox_", stride)), get(fmt.Sprint("kps_", stride))
		cols := detSide / stride
		for idx := range cls {
			sc := float32(math.Sqrt(float64(clamp01(cls[idx]) * clamp01(obj[idx]))))
			if sc < minScore {
				continue
			}
			r, c := idx/cols, idx%cols
			st := float64(stride)
			cx := (float64(c) + float64(box[idx*4])) * st
			cy := (float64(r) + float64(box[idx*4+1])) * st
			bw := math.Exp(float64(box[idx*4+2])) * st
			bh := math.Exp(float64(box[idx*4+3])) * st
			f := &face{x: (cx - bw/2) / scale, y: (cy - bh/2) / scale, w: bw / scale, h: bh / scale, score: sc}
			for k := range 5 {
				f.lm[k][0] = (float64(kps[idx*10+2*k]) + float64(c)) * st / scale
				f.lm[k][1] = (float64(kps[idx*10+2*k+1]) + float64(r)) * st / scale
			}
			found = append(found, f)
		}
	}
	return nms(found, 0.3)
}

func nms(fs []*face, iouMax float64) []*face {
	sort.Slice(fs, func(i, j int) bool { return fs[i].score > fs[j].score })
	var keep []*face
	for _, f := range fs {
		ok := true
		for _, k := range keep {
			if iou(f, k) > iouMax {
				ok = false
				break
			}
		}
		if ok {
			keep = append(keep, f)
		}
	}
	return keep
}

func iou(a, b *face) float64 {
	x1, y1 := math.Max(a.x, b.x), math.Max(a.y, b.y)
	x2, y2 := math.Min(a.x+a.w, b.x+b.w), math.Min(a.y+a.h, b.y+b.h)
	inter := math.Max(0, x2-x1) * math.Max(0, y2-y1)
	return inter / (a.w*a.h + b.w*b.h - inter)
}

// align warps the face so its landmarks land on the template (least-squares
// similarity transform, bilinear sampling).
func align(pix []byte, w, h int, lm [5][2]float64) *image.RGBA {
	var mx, my, tx, ty float64
	for i := range 5 {
		mx += lm[i][0] / 5
		my += lm[i][1] / 5
		tx += template[i][0] / 5
		ty += template[i][1] / 5
	}
	var num1, num2, den float64
	for i := range 5 {
		px, py := lm[i][0]-mx, lm[i][1]-my
		qx, qy := template[i][0]-tx, template[i][1]-ty
		num1 += px*qx + py*qy
		num2 += px*qy - py*qx
		den += px*px + py*py
	}
	a, b := num1/den, num2/den // dst = [a -b; b a] * src + t
	inv := 1 / (a*a + b*b)
	dst := image.NewRGBA(image.Rect(0, 0, crop, crop))
	for v := range crop {
		for u := range crop {
			dx, dy := float64(u)-tx, float64(v)-ty
			sx := inv*(a*dx+b*dy) + mx
			sy := inv*(-b*dx+a*dy) + my
			r, g, bl := bilinear(pix, w, h, sx, sy)
			dst.SetRGBA(u, v, color.RGBA{r, g, bl, 255})
		}
	}
	return dst
}

func bilinear(pix []byte, w, h int, x, y float64) (uint8, uint8, uint8) {
	if x < 0 || y < 0 || x > float64(w-1) || y > float64(h-1) {
		return 0, 0, 0
	}
	x0, y0 := int(x), int(y)
	x1, y1 := min(x0+1, w-1), min(y0+1, h-1)
	fx, fy := x-float64(x0), y-float64(y0)
	var out [3]uint8
	for c := range 3 {
		p := func(xx, yy int) float64 { return float64(pix[(yy*w+xx)*3+c]) }
		v := p(x0, y0)*(1-fx)*(1-fy) + p(x1, y0)*fx*(1-fy) + p(x0, y1)*(1-fx)*fy + p(x1, y1)*fx*fy
		out[c] = uint8(math.Round(v))
	}
	return out[0], out[1], out[2]
}

// embed runs SFace: RGB, 0..255, NCHW, 1x3x112x112 -> 128-d, normalized.
func embed(s *onnx.Session, img *image.RGBA) []float32 {
	plane := crop * crop
	data := make([]float32, 3*plane)
	for i := range plane {
		for c := range 3 {
			data[c*plane+i] = float32(img.Pix[i*4+c])
		}
	}
	in, err := ort.NewTensor(ort.NewShape(1, 3, crop, crop), data)
	check(err)
	defer in.Destroy()
	outs, err := s.Run(in)
	check(err)
	defer onnx.Destroy(outs...)
	v, _, err := onnx.Float32(outs[0])
	check(err)
	var n float64
	for _, x := range v {
		n += float64(x * x)
	}
	n = math.Sqrt(n)
	for i := range v {
		v[i] = float32(float64(v[i]) / n)
	}
	return v
}

func similarity(fs []*face) [][]float64 {
	s := make([][]float64, len(fs))
	for i := range fs {
		s[i] = make([]float64, len(fs))
		for j := range fs {
			var d float64
			for k := range fs[i].emb {
				d += float64(fs[i].emb[k] * fs[j].emb[k])
			}
			s[i][j] = d
		}
	}
	return s
}

// cluster is average-linkage agglomerative clustering: keep merging the two
// most similar clusters while their mean pairwise cosine is >= th.
func cluster(sim [][]float64, th float64) [][]int {
	cl := make([][]int, len(sim))
	for i := range cl {
		cl[i] = []int{i}
	}
	avg := func(a, b []int) float64 {
		s := 0.0
		for _, i := range a {
			for _, j := range b {
				s += sim[i][j]
			}
		}
		return s / float64(len(a)*len(b))
	}
	for {
		best, bi, bj := th, -1, -1
		for i := range cl {
			for j := i + 1; j < len(cl); j++ {
				if s := avg(cl[i], cl[j]); s >= best {
					best, bi, bj = s, i, j
				}
			}
		}
		if bi < 0 {
			break
		}
		cl[bi] = append(cl[bi], cl[bj]...)
		cl = append(cl[:bj], cl[bj+1:]...)
	}
	sort.Slice(cl, func(i, j int) bool { return len(cl[i]) > len(cl[j]) })
	return cl
}

func sheet(dst string, fs []*face, idx []int) {
	const cols = 8
	idx = idx[:min(len(idx), 32)]
	rows := (len(idx) + cols - 1) / cols
	img := image.NewRGBA(image.Rect(0, 0, cols*crop, rows*crop))
	for k, i := range idx {
		r := image.Rect((k%cols)*crop, (k/cols)*crop, (k%cols+1)*crop, (k/cols+1)*crop)
		draw.Draw(img, r, fs[i].crop, image.Point{}, draw.Src)
	}
	f, err := os.Create(dst)
	check(err)
	defer f.Close()
	jpeg.Encode(f, img, &jpeg.Options{Quality: 85})
}

func clamp01(x float32) float32 { return max(0, min(1, x)) }

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func check(err error) {
	if err != nil {
		panic(err)
	}
}
