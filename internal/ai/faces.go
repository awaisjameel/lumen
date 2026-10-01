package ai

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

// FaceModel describes the prepared face models (faces.json, written by
// scripts/prepare_models.py).
type FaceModel struct {
	Dir        string `json:"-"`
	Version    string `json:"version"`    // stored with every face; a change re-runs the stage
	DetectSize int    `json:"detectSize"` // YuNet's square input side
	CropSize   int    `json:"cropSize"`   // SFace's aligned face side
	Dim        int    `json:"dim"`        // identity vector size
}

// LoadFaceModel reads the face model description in dir.
func LoadFaceModel(dir string) (FaceModel, error) {
	m := FaceModel{Dir: dir}
	raw, err := os.ReadFile(filepath.Join(dir, "faces.json"))
	if err != nil {
		return m, fmt.Errorf("face models missing: %w", err)
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return m, fmt.Errorf("read faces.json: %w", err)
	}
	if m.Version == "" || m.DetectSize <= 0 || m.CropSize <= 0 || m.Dim <= 0 {
		return m, fmt.Errorf("faces.json is incomplete: %+v", m)
	}
	return m, nil
}

// Face is one face found in an image, in that image's pixels.
type Face struct {
	X, Y, W, H float64 // bounding box
	Score      float32 // detection confidence, 0..1
	// Landmarks: the two eyes, the nose tip and the two mouth corners, in
	// the order SFace's alignment template expects.
	Landmarks [5][2]float64
}

// Yaw is how far the face is turned: the nose tip's distance from between
// the eyes along the line through them, in eye distances, so a tilted face
// that looks at the camera is not taken for a turned one. About 0 facing the camera, 0.3 at
// three-quarters, above 0.5 in profile, where recognition is unreliable.
func (f Face) Yaw() float32 {
	l := f.Landmarks
	// The nose's offset from between the eyes, along the line through the
	// eyes: a face that is only tilted (rolled) has none.
	ex, ey := l[1][0]-l[0][0], l[1][1]-l[0][1]
	dx, dy := l[2][0]-(l[0][0]+l[1][0])/2, l[2][1]-(l[0][1]+l[1][1])/2
	eyes2 := ex*ex + ey*ey
	if eyes2 == 0 {
		return 1
	}
	return float32(math.Abs(dx*ex+dy*ey) / eyes2)
}

// Faces finds and recognises faces. YuNet detects them with five landmarks,
// the landmarks align each face to SFace's template, and SFace turns it into
// an identity vector: two photos of one person give vectors with a high
// cosine similarity. Both models run on the GPU while the image model leaves
// it free (see gpu), and on the CPU otherwise or without one: the two find
// the same faces with the same vectors (1,000 faces measured: the same
// boxes, cosines above 0.99999), so any mix clusters together. Safe for
// concurrent use.
type Faces struct {
	m FaceModel
	// mu is held for reading while a model runs, and for writing to drop
	// the GPU's.
	mu         sync.RWMutex
	det, rec   *session // on the CPU
	gdet, grec *session // on the GPU; nil without one, or after it failed
	// turn queues face runs for the GPU, so while the image model leaves it
	// free they all run there, one after another, rather than on the CPU.
	turn sync.Mutex
}

// yunetStrides are the detector's three feature map strides; each has
// cls/obj/bbox/kps outputs.
var yunetStrides = [...]int{8, 16, 32}

// OpenFaces loads both face models on the CPU and, with device GPU, on the
// GPU too where one is usable. threads limits each model's CPU threads.
func OpenFaces(m FaceModel, device Device, threads int) (*Faces, error) {
	f := &Faces{m: m}
	var err error
	if f.det, f.rec, err = openFaceModels(m, CPU, threads); err != nil {
		return nil, err
	}
	if device == GPU {
		if f.gdet, f.grec, err = openFaceModels(m, GPU, 0); err != nil {
			slog.Warn("face models can't use the GPU; using the CPU", "err", err)
		}
	}
	return f, nil
}

func openFaceModels(m FaceModel, device Device, threads int) (det, rec *session, err error) {
	var outputs []string
	for _, kind := range []string{"cls", "obj", "bbox", "kps"} {
		for _, s := range yunetStrides {
			outputs = append(outputs, fmt.Sprintf("%s_%d", kind, s))
		}
	}
	det, err = openSession(filepath.Join(m.Dir, "yunet.onnx"), device, threads, outputs...)
	if err != nil {
		return nil, nil, err
	}
	rec, err = openSession(filepath.Join(m.Dir, "sface.onnx"), device, threads)
	if err != nil {
		det.close()
		return nil, nil, err
	}
	return det, rec, nil
}

// Device reports where the models run while the GPU is free: the GPU, or
// the CPU without one (or after it failed).
func (f *Faces) Device() Device {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.gdet != nil {
		return GPU
	}
	return CPU
}

// run runs the detector (or, with rec, the recogniser): on the GPU if the
// image model leaves it free, else on the CPU. A GPU that fails (a driver
// update, a removed device) is dropped, and the CPU runs everything.
func (f *Faces) run(rec bool, in ort.Value) ([][]float32, error) {
	f.mu.RLock()
	c, g := f.det, f.gdet
	if rec {
		c, g = f.rec, f.grec
	}
	if g != nil {
		f.turn.Lock()
		out, ran, err := g.tryRun(in)
		f.turn.Unlock()
		if ran {
			f.mu.RUnlock()
			if err == nil {
				return out, nil
			}
			f.dropGPU(g, err)
			return f.run(rec, in)
		}
	}
	defer f.mu.RUnlock()
	return c.run(in)
}

// dropGPU frees the GPU's models after a run failed on failed, unless
// another run already did.
func (f *Faces) dropGPU(failed *session, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if failed != f.gdet && failed != f.grec {
		return
	}
	slog.Warn("face models failed on the GPU; using the CPU", "err", err)
	f.gdet.close()
	f.grec.close()
	f.gdet, f.grec = nil, nil
}

// Close frees the models.
func (f *Faces) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range []*session{f.det, f.rec, f.gdet, f.grec} {
		s.close()
	}
}

// Detect finds the faces in an 8-bit RGB image scoring at least minScore,
// surest first. The image is scaled to fit the detector's square input, so
// its long edge sets the smallest face found (about 10 detector pixels).
func (f *Faces) Detect(pix []byte, w, h int, minScore float32) ([]Face, error) {
	if err := checkRGB(pix, w, h); err != nil {
		return nil, err
	}
	side := f.m.DetectSize
	scale := float64(side) / float64(max(w, h))
	dw, dh := max(1, int(math.Round(float64(w)*scale))), max(1, int(math.Round(float64(h)*scale)))
	small := resample(pix, w, h, dw, dh)
	// Planar BGR, 0..255, the rest of the square zero (YuNet was trained on
	// OpenCV's BGR images).
	plane := side * side
	data := make([]float32, 3*plane)
	for y := range dh {
		for x := range dw {
			o, i := (y*dw+x)*3, y*side+x
			data[i], data[plane+i], data[2*plane+i] = float32(small[o+2]), float32(small[o+1]), float32(small[o])
		}
	}
	in, err := ort.NewTensor(ort.NewShape(1, 3, int64(side), int64(side)), data)
	if err != nil {
		return nil, err
	}
	defer destroy(in)
	outs, err := f.run(false, in)
	if err != nil {
		return nil, fmt.Errorf("run face detector: %w", err)
	}
	return detectFaces(outs, side, w, h, minScore)
}

// detectFaces validates the detector's outputs before decoding image-space faces.
func detectFaces(outs [][]float32, side, w, h int, minScore float32) ([]Face, error) {
	scale := float64(side) / float64(max(w, h))
	n := len(yunetStrides)
	if len(outs) != 4*n {
		return nil, fmt.Errorf("face detector: got %d outputs, want %d", len(outs), 4*n)
	}
	var found []Face
	for k, stride := range yunetStrides {
		cls, obj, box, kps := outs[k], outs[n+k], outs[2*n+k], outs[3*n+k]
		cols := side / stride
		if len(obj) != len(cls) || len(box) != 4*len(cls) || len(kps) != 10*len(cls) {
			return nil, fmt.Errorf("face detector: unexpected output sizes at stride %d", stride)
		}
		st := float64(stride)
		for i := range cls {
			if !finite(float64(cls[i]), float64(obj[i])) {
				continue
			}
			score := float32(math.Sqrt(float64(clamp01(cls[i]) * clamp01(obj[i]))))
			if !(score >= minScore) { // also drops NaN, which compares false
				continue
			}
			r, c := float64(i/cols), float64(i%cols)
			cx, cy := (c+float64(box[4*i]))*st, (r+float64(box[4*i+1]))*st
			bw, bh := math.Exp(float64(box[4*i+2]))*st, math.Exp(float64(box[4*i+3]))*st
			fc := Face{X: (cx - bw/2) / scale, Y: (cy - bh/2) / scale, W: bw / scale, H: bh / scale, Score: score}
			ok := finite(fc.X, fc.Y, fc.W, fc.H)
			for j := range 5 {
				fc.Landmarks[j] = [2]float64{
					(float64(kps[10*i+2*j]) + c) * st / scale,
					(float64(kps[10*i+2*j+1]) + r) * st / scale,
				}
				ok = ok && finite(fc.Landmarks[j][0], fc.Landmarks[j][1])
			}
			if ok { // a broken output would sample outside the image when aligned
				found = append(found, clip(fc, float64(w), float64(h)))
			}
		}
	}
	return nonMaxSuppress(found, 0.3), nil
}

// FaceAnalysisVersion is the version of what Lumen computes around the face
// models (alignment, sharpness, yaw). A change re-runs the faces stage, as a
// new model does.
const FaceAnalysisVersion = "3"

// embedBatch is the most faces SFace embeds in one run.
const embedBatch = 32

// Identity is what recognition makes of one face.
type Identity struct {
	Vec []float32 // unit vector; the same person's faces point the same way. nil: not recognisable
	// Sharpness is how much detail the aligned face holds (the variance of
	// its Laplacian): blurred, dark or enlarged tiny faces score low, and
	// their vectors are less reliable.
	Sharpness float32
}

// Embed aligns each face of an 8-bit RGB image (as Detect found it there)
// and recognises it.
func (f *Faces) Embed(pix []byte, w, h int, faces []Face) ([]Identity, error) {
	if err := checkRGB(pix, w, h); err != nil {
		return nil, err
	}
	side := f.m.CropSize
	plane := side * side
	out := make([]Identity, 0, len(faces))
	for lo := 0; lo < len(faces); lo += embedBatch {
		batch := faces[lo:min(lo+embedBatch, len(faces))]
		data := make([]float32, len(batch)*3*plane)
		sharp := make([]float32, len(batch))
		for b, fc := range batch {
			crop, inside := align(pix, w, h, fc.Landmarks, side)
			sharp[b] = sharpness(crop, inside, side)
			base := b * 3 * plane
			for i := range plane { // planar RGB, 0..255: SFace normalises itself
				data[base+i] = float32(crop[3*i])
				data[base+plane+i] = float32(crop[3*i+1])
				data[base+2*plane+i] = float32(crop[3*i+2])
			}
		}
		in, err := ort.NewTensor(ort.NewShape(int64(len(batch)), 3, int64(side), int64(side)), data)
		if err != nil {
			return nil, err
		}
		res, err := f.run(true, in)
		destroy(in)
		if err != nil {
			return nil, fmt.Errorf("run face recognizer: %w", err)
		}
		vecs, err := unitRows(res[0], f.m.Dim, len(batch))
		if err != nil {
			return nil, err
		}
		for b, v := range vecs {
			out = append(out, Identity{Vec: v, Sharpness: sharp[b]})
		}
	}
	return out, nil
}

// sharpness is the variance of the Laplacian of an aligned face's
// brightness, over the face itself (eyes to chin, not the background at the
// crop's edges). Pixels next to where the photo ends are left out: the hard
// edge against black would count as detail.
func sharpness(crop []byte, inside []bool, side int) float32 {
	g := make([]float64, side*side)
	for i := range g {
		g[i] = 0.299*float64(crop[3*i]) + 0.587*float64(crop[3*i+1]) + 0.114*float64(crop[3*i+2])
	}
	var sum, sum2 float64
	var n int
	for y := side * 24 / 112; y < side*96/112; y++ {
		for x := side * 20 / 112; x < side*92/112; x++ {
			if i := y*side + x; !inside[i] || !inside[i-1] || !inside[i+1] || !inside[i-side] || !inside[i+side] {
				continue
			}
			v := 4*g[y*side+x] - g[y*side+x-1] - g[y*side+x+1] - g[(y-1)*side+x] - g[(y+1)*side+x]
			sum += v
			sum2 += v * v
			n++
		}
	}
	if n == 0 {
		return 0
	}
	mean := sum / float64(n)
	return float32(sum2/float64(n) - mean*mean)
}

// faceTemplate is where SFace expects the five landmarks in a 112 x 112 crop
// (the ArcFace template OpenCV's face recogniser aligns to).
var faceTemplate = [5][2]float64{
	{38.2946, 51.6963}, {73.5318, 51.5014}, {56.0252, 71.7366}, {41.5493, 92.3655}, {70.7299, 92.2041},
}

// Align warps the face whose landmarks are lm onto SFace's template: the
// least-squares similarity transform (rotation, scale, shift) that best maps
// the landmarks onto it. It returns a side x side 8-bit RGB crop. Big faces
// are averaged over several samples per pixel, so they don't alias.
func Align(pix []byte, w, h int, lm [5][2]float64, side int) []byte {
	crop, _ := align(pix, w, h, lm, side)
	return crop
}

// align is Align, also reporting which crop pixels come from inside the
// image (the rest are black), so sharpness can leave out the image's edge.
func align(pix []byte, w, h int, lm [5][2]float64, side int) (crop []byte, inside []bool) {
	k := float64(side) / 112
	var mx, my, tx, ty float64
	for i := range 5 {
		mx += lm[i][0] / 5
		my += lm[i][1] / 5
		tx += faceTemplate[i][0] * k / 5
		ty += faceTemplate[i][1] * k / 5
	}
	var num1, num2, den float64
	for i := range 5 {
		px, py := lm[i][0]-mx, lm[i][1]-my
		qx, qy := faceTemplate[i][0]*k-tx, faceTemplate[i][1]*k-ty
		num1 += px*qx + py*qy
		num2 += px*qy - py*qx
		den += px*px + py*py
	}
	crop, inside = make([]byte, side*side*3), make([]bool, side*side)
	if den == 0 {
		return crop, inside
	}
	// crop = [a -b; b a] * (image - m) + t, so image = inverse applied to crop.
	a, b := num1/den, num2/den
	inv := 1 / (a*a + b*b)
	srcPerPixel := math.Sqrt(inv) // image pixels per crop pixel
	n := min(4, max(1, int(math.Ceil(srcPerPixel))))
	for v := range side {
		for u := range side {
			var acc [3]float64
			in := true
			for sv := range n {
				for su := range n {
					du := float64(u) + (float64(su)+0.5)/float64(n) - 0.5 - tx
					dv := float64(v) + (float64(sv)+0.5)/float64(n) - 0.5 - ty
					x, y := inv*(a*du+b*dv)+mx, inv*(-b*du+a*dv)+my
					in = in && x >= 0 && y >= 0 && x <= float64(w-1) && y <= float64(h-1)
					c := bilinear(pix, w, h, x, y)
					acc[0], acc[1], acc[2] = acc[0]+c[0], acc[1]+c[1], acc[2]+c[2]
				}
			}
			o := (v*side + u) * 3
			for ch := range 3 {
				crop[o+ch] = uint8(math.Round(acc[ch] / float64(n*n)))
			}
			inside[v*side+u] = in
		}
	}
	return crop, inside
}

// bilinear samples an RGB image at (x, y); outside it is black.
func bilinear(pix []byte, w, h int, x, y float64) [3]float64 {
	if x < 0 || y < 0 || x > float64(w-1) || y > float64(h-1) {
		return [3]float64{}
	}
	x0, y0 := int(x), int(y)
	fx, fy := x-float64(x0), y-float64(y0)
	// The four neighbours (repeating the last row or column at the edge),
	// and their weights.
	dx, dy := 3, 3*w
	if x0 == w-1 {
		dx = 0
	}
	if y0 == h-1 {
		dy = 0
	}
	i := (y0*w + x0) * 3
	w00, w10, w01, w11 := (1-fx)*(1-fy), fx*(1-fy), (1-fx)*fy, fx*fy
	var c [3]float64
	for ch := range 3 {
		c[ch] = float64(pix[i+ch])*w00 + float64(pix[i+dx+ch])*w10 + float64(pix[i+dy+ch])*w01 + float64(pix[i+dy+dx+ch])*w11
	}
	return c
}

// resample scales an RGB image to dw x dh: averaging the covered pixels when
// shrinking (so small faces survive), bilinear when enlarging.
func resample(pix []byte, w, h, dw, dh int) []byte {
	out := make([]byte, dw*dh*3)
	sx, sy := float64(w)/float64(dw), float64(h)/float64(dh)
	if sx <= 1 && sy <= 1 {
		for y := range dh {
			for x := range dw {
				c := bilinear(pix, w, h, max(0, min(float64(w-1), (float64(x)+0.5)*sx-0.5)), max(0, min(float64(h-1), (float64(y)+0.5)*sy-0.5)))
				o := (y*dw + x) * 3
				out[o], out[o+1], out[o+2] = uint8(c[0]+0.5), uint8(c[1]+0.5), uint8(c[2]+0.5)
			}
		}
		return out
	}
	for y := range dh {
		y0 := int(float64(y) * sy)
		y1 := max(y0+1, min(h, int(float64(y+1)*sy)))
		for x := range dw {
			x0 := int(float64(x) * sx)
			x1 := max(x0+1, min(w, int(float64(x+1)*sx)))
			var r, g, b, n int
			for yy := y0; yy < y1; yy++ {
				row := pix[(yy*w+x0)*3 : (yy*w+x1)*3]
				for i := 0; i+2 < len(row); i += 3 {
					r, g, b = r+int(row[i]), g+int(row[i+1]), b+int(row[i+2])
				}
				n += x1 - x0
			}
			o := (y*dw + x) * 3
			out[o], out[o+1], out[o+2] = uint8((r+n/2)/n), uint8((g+n/2)/n), uint8((b+n/2)/n)
		}
	}
	return out
}

// nonMaxSuppress keeps the surest of any faces overlapping by more than
// maxIoU, surest first.
func nonMaxSuppress(faces []Face, maxIoU float64) []Face {
	slices.SortFunc(faces, func(a, b Face) int { return cmp.Compare(b.Score, a.Score) })
	keep := faces[:0:0]
	for _, f := range faces {
		if !slices.ContainsFunc(keep, func(k Face) bool { return iou(f, k) > maxIoU }) {
			keep = append(keep, f)
		}
	}
	return keep
}

func iou(a, b Face) float64 {
	x1, y1 := math.Max(a.X, b.X), math.Max(a.Y, b.Y)
	x2, y2 := math.Min(a.X+a.W, b.X+b.W), math.Min(a.Y+a.H, b.Y+b.H)
	inter := math.Max(0, x2-x1) * math.Max(0, y2-y1)
	if union := a.W*a.H + b.W*b.H - inter; union > 0 {
		return inter / union
	}
	return 0
}

// clip keeps a face's box inside a w x h image.
func clip(f Face, w, h float64) Face {
	x0, y0 := math.Max(0, f.X), math.Max(0, f.Y)
	x1, y1 := math.Min(w, f.X+f.W), math.Min(h, f.Y+f.H)
	f.X, f.Y, f.W, f.H = x0, y0, math.Max(0, x1-x0), math.Max(0, y1-y0)
	return f
}

func clamp01(x float32) float32 { return max(0, min(1, x)) }

// finite reports whether none of xs is NaN or infinite.
func finite(xs ...float64) bool {
	for _, x := range xs {
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return false
		}
	}
	return true
}

// ErrBadImage is what Detect and Embed return for pixels that aren't a w x h
// 8-bit RGB image: a problem of that file. Their other errors are the models'.
var ErrBadImage = errors.New("faces: bad image")

// checkRGB rejects pixels that aren't a w x h 8-bit RGB image.
func checkRGB(pix []byte, w, h int) error {
	if w <= 0 || h <= 0 || len(pix) != w*h*3 {
		return fmt.Errorf("%w (%dx%d, %d bytes)", ErrBadImage, w, h, len(pix))
	}
	return nil
}
