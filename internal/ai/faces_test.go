package ai

import (
	"context"
	"math"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"lumen/internal/core"
	"lumen/internal/media"
	"lumen/internal/testenv"
)

func TestDetectorDropsNonFiniteOutputsBeforeDecodingFaces(t *testing.T) {
	for _, tc := range []struct {
		name   string
		output int
		value  float32
	}{
		{"infinite class", 0, float32(math.Inf(1))},
		{"infinite objectness", 3, float32(math.Inf(1))},
		{"NaN class", 0, float32(math.NaN())},
		{"NaN objectness", 3, float32(math.NaN())},
		{"infinite box", 6, float32(math.Inf(1))},
		{"NaN box", 6, float32(math.NaN())},
		{"infinite landmark", 9, float32(math.Inf(1))},
		{"NaN landmark", 9, float32(math.NaN())},
	} {
		t.Run(tc.name, func(t *testing.T) {
			outs := detectorOutputs()
			outs[tc.output][0] = tc.value
			got, err := detectFaces(outs, 32, 32, 32, 0.8)
			if err != nil || len(got) != 0 {
				t.Fatalf("invalid output produced faces: %+v, %v", got, err)
			}
		})
	}
	got, err := detectFaces(detectorOutputs(), 32, 32, 32, 0.8)
	if err != nil || len(got) != 1 || got[0].Score != 1 {
		t.Fatalf("valid output = %+v, %v, want one face", got, err)
	}
	if _, err := detectFaces(nil, 32, 32, 32, 0.8); err == nil {
		t.Fatal("missing detector outputs were accepted")
	}
}

func detectorOutputs() [][]float32 {
	outs := make([][]float32, 4*len(yunetStrides))
	for k, stride := range yunetStrides {
		n := (32 / stride) * (32 / stride)
		outs[k], outs[3+k] = make([]float32, n), make([]float32, n)
		outs[6+k], outs[9+k] = make([]float32, 4*n), make([]float32, 10*n)
	}
	outs[0][0], outs[3][0] = 1, 1
	outs[6][0], outs[6][1] = 2, 2
	return outs
}

// Five dots placed by a known rotation, scale and shift of the template must
// land back on the template's points after alignment.
func TestAlignUndoesRotationAndScale(t *testing.T) {
	const w, h = 400, 300
	pix := make([]byte, w*h*3)
	angle, scale, ox, oy := 20*math.Pi/180, 1.8, 150.0, 40.0
	var lm [5][2]float64
	for i, p := range faceTemplate {
		x := scale*(p[0]*math.Cos(angle)-p[1]*math.Sin(angle)) + ox
		y := scale*(p[0]*math.Sin(angle)+p[1]*math.Cos(angle)) + oy
		lm[i] = [2]float64{x, y}
		for dy := -3; dy <= 3; dy++ {
			for dx := -3; dx <= 3; dx++ {
				o := ((int(y)+dy)*w + int(x) + dx) * 3
				pix[o], pix[o+1], pix[o+2] = 255, 255, 255
			}
		}
	}
	crop := Align(pix, w, h, lm, 112)
	at := func(x, y float64) byte { return crop[(int(math.Round(y))*112+int(math.Round(x)))*3] }
	for i, p := range faceTemplate {
		if v := at(p[0], p[1]); v < 150 {
			t.Errorf("landmark %d: crop is %d at the template point, want bright", i, v)
		}
	}
	if v := at(10, 10); v != 0 {
		t.Errorf("corner is %d, want black", v)
	}
}

func TestResampleAverages(t *testing.T) {
	// 4x2: left half 0, right half 200 -> 2x1 averages exactly.
	pix := make([]byte, 4*2*3)
	for y := range 2 {
		for x := 2; x < 4; x++ {
			o := (y*4 + x) * 3
			pix[o], pix[o+1], pix[o+2] = 200, 200, 200
		}
	}
	out := resample(pix, 4, 2, 2, 1)
	if out[0] != 0 || out[3] != 200 {
		t.Errorf("resampled %v, want [0 0 0 200 200 200]", out)
	}
	if up := resample(pix, 4, 2, 8, 4); len(up) != 8*4*3 {
		t.Errorf("enlarged to %d bytes", len(up))
	}
}

func TestNonMaxSuppressKeepsSurest(t *testing.T) {
	faces := []Face{
		{X: 0, Y: 0, W: 10, H: 10, Score: 0.8},
		{X: 1, Y: 1, W: 10, H: 10, Score: 0.95}, // same face, surer
		{X: 50, Y: 50, W: 10, H: 10, Score: 0.7},
	}
	got := nonMaxSuppress(faces, 0.3)
	if len(got) != 2 || got[0].Score != 0.95 || got[1].Score != 0.7 {
		t.Errorf("kept %+v", got)
	}
}

// On real photos of people: faces are found, and one face seen at two
// sizes gets nearly the same identity vector, while two different faces in
// one photo don't.
func TestFacesOnRealPhotos(t *testing.T) {
	photos := testenv.FacePhotos(t, 12)
	rt := testenv.Runtime(t)
	if err := Init(rt); err != nil {
		t.Fatal(err)
	}
	m, err := LoadFaceModel(filepath.Join(rt, "models", "faces"))
	if err != nil {
		t.Fatal(err)
	}
	f, err := OpenFaces(m, CPU, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	dec := media.Decoders{}
	ctx := context.Background()
	var found, same, different int
	var worstSame, bestDifferent float32 = 1, -1
	for _, p := range photos {
		im, err := dec.Decode(ctx, p, core.KindPhoto, 1, 2048)
		if err != nil {
			t.Fatal(err)
		}
		faces, err := f.Detect(im.Pix, im.W, im.H, 0.8)
		if err != nil {
			t.Fatal(err)
		}
		var big []Face
		for _, fc := range faces {
			if min(fc.W, fc.H) >= 80 {
				big = append(big, fc)
			}
		}
		found += len(faces)
		if len(big) == 0 {
			continue
		}
		vecs, err := f.Embed(im.Pix, im.W, im.H, big)
		if err != nil {
			t.Fatal(err)
		}
		// The same photo at 60%: the same faces, scaled.
		small, err := dec.Decode(ctx, p, core.KindPhoto, 1, 2048*6/10)
		if err != nil {
			t.Fatal(err)
		}
		k := float64(small.W) / float64(im.W)
		scaled := make([]Face, len(big))
		for i, fc := range big {
			scaled[i] = fc
			for j := range 5 {
				scaled[i].Landmarks[j] = [2]float64{fc.Landmarks[j][0] * k, fc.Landmarks[j][1] * k}
			}
		}
		again, err := f.Embed(small.Pix, small.W, small.H, scaled)
		if err != nil {
			t.Fatal(err)
		}
		for i := range vecs {
			worstSame = min(worstSame, dot(vecs[i].Vec, again[i].Vec))
			same++
			if vecs[i].Sharpness <= 0 {
				t.Errorf("face %d: sharpness %v", i, vecs[i].Sharpness)
			}
			for j := i + 1; j < len(vecs); j++ {
				bestDifferent = max(bestDifferent, dot(vecs[i].Vec, vecs[j].Vec))
				different++
			}
		}
	}
	t.Logf("%d photos: %d faces; same face at two sizes: worst cosine %.3f (%d); different faces in one photo: best %.3f (%d)",
		len(photos), found, worstSame, same, bestDifferent, different)
	if found == 0 || same == 0 {
		t.Fatal("no faces found")
	}
	if worstSame < 0.8 {
		t.Errorf("one face at two sizes: cosine %.3f, want >= 0.8", worstSame)
	}
	if different > 0 && bestDifferent >= worstSame {
		t.Errorf("different faces (%.3f) as alike as one face at two sizes (%.3f)", bestDifferent, worstSame)
	}
}

// On the GPU the face models must find the faces the CPU finds and give
// them the same identity vectors, so faces found on either cluster together
// (a library indexed partly on each, or moved to the CPU when the GPU fails).
func TestFacesOnTheGPUMatchTheCPU(t *testing.T) {
	photos := testenv.FacePhotos(t, 24)
	rt := testenv.Runtime(t)
	if err := Init(rt); err != nil {
		t.Fatal(err)
	}
	m, err := LoadFaceModel(filepath.Join(rt, "models", "faces"))
	if err != nil {
		t.Fatal(err)
	}
	onGPU, err := OpenFaces(m, GPU, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer onGPU.Close()
	if onGPU.Device() != GPU {
		t.Skip("no usable GPU")
	}
	onCPU, err := OpenFaces(m, CPU, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer onCPU.Close()
	var faces, missed int
	worst := float32(1)
	for _, p := range photos {
		im, err := media.Decoders{}.Decode(context.Background(), p, core.KindPhoto, 1, 2048)
		if err != nil {
			t.Fatal(err)
		}
		fc, err := onCPU.Detect(im.Pix, im.W, im.H, 0.8)
		if err != nil {
			t.Fatal(err)
		}
		fg, err := onGPU.Detect(im.Pix, im.W, im.H, 0.75) // a face just over the line on one is just under on the other
		if err != nil {
			t.Fatal(err)
		}
		var pc, pg []Face
		for _, a := range fc {
			k := slices.IndexFunc(fg, func(b Face) bool { return iou(a, b) > 0.8 })
			if k < 0 {
				missed++
				continue
			}
			pc, pg = append(pc, a), append(pg, fg[k])
		}
		faces += len(fc)
		if len(pc) == 0 {
			continue
		}
		vc, err := onCPU.Embed(im.Pix, im.W, im.H, pc)
		if err != nil {
			t.Fatal(err)
		}
		vg, err := onGPU.Embed(im.Pix, im.W, im.H, pg)
		if err != nil {
			t.Fatal(err)
		}
		for k := range vc {
			worst = min(worst, dot(vc[k].Vec, vg[k].Vec))
		}
	}
	t.Logf("%d photos: %d faces, %d not found on the GPU; worst cosine between the devices %.5f", len(photos), faces, missed, worst)
	if faces == 0 {
		t.Fatal("no faces found")
	}
	if missed > 0 {
		t.Errorf("%d faces the CPU found were not found on the GPU", missed)
	}
	if worst < 0.99 {
		t.Errorf("a face's vectors on the two devices have a cosine of %.5f, want >= 0.99", worst)
	}

	// While the image model has the GPU, faces are still found, on the CPU.
	im, err := media.Decoders{}.Decode(context.Background(), photos[0], core.KindPhoto, 1, 2048)
	if err != nil {
		t.Fatal(err)
	}
	gpu.mu.Lock() // as the image model does while it runs
	done := make(chan error, 1)
	go func() {
		_, err := onGPU.Detect(im.Pix, im.W, im.H, 0.8)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(20 * time.Second):
		t.Error("face detection waited for the GPU")
	}
	gpu.mu.Unlock()
}

func TestYaw(t *testing.T) {
	front := Face{Landmarks: faceTemplate}
	if y := front.Yaw(); y > 0.05 {
		t.Errorf("template face yaw %.3f, want about 0", y)
	}
	turned := front
	turned.Landmarks[2][0] += 25 // nose tip well to one side
	if y := turned.Yaw(); y < 0.5 {
		t.Errorf("turned face yaw %.3f, want > 0.5", y)
	}
	// Tilted 60 degrees but looking at the camera: not turned.
	rolled := front
	sin, cos := math.Sincos(math.Pi / 3)
	for i, p := range front.Landmarks {
		rolled.Landmarks[i] = [2]float64{56 + (p[0]-56)*cos - (p[1]-56)*sin, 56 + (p[0]-56)*sin + (p[1]-56)*cos}
	}
	if y := rolled.Yaw(); y > 0.05 {
		t.Errorf("rolled face yaw %.3f, want about 0", y)
	}
}

func BenchmarkAlign(b *testing.B) {
	const w, h = 2048, 1536
	pix := make([]byte, w*h*3)
	for i := range pix {
		pix[i] = byte(i * 7)
	}
	// A 400 px face: 4 x 4 samples per crop pixel.
	lm := [5][2]float64{{900, 600}, {1150, 600}, {1025, 750}, {930, 900}, {1120, 900}}
	for b.Loop() {
		Align(pix, w, h, lm, 112)
	}
}
