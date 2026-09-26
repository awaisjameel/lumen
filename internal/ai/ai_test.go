package ai

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"lumen/internal/testenv"
)

func model(t *testing.T) Model {
	t.Helper()
	rt := testenv.Runtime(t)
	if err := Init(rt); err != nil {
		t.Fatal(err)
	}
	m, err := LoadModel(filepath.Join(rt, "models", "siglip2"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// testdata/tokenizer_fixtures.json holds Hugging Face `tokenizers` output on
// the same tokenizer.json (scripts/prepare_models.py writes it).
func TestTokenizerMatchesReference(t *testing.T) {
	tok, err := LoadTokenizer(testenv.File(t, "models", "siglip2", "tokenizer.bin"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("testdata/tokenizer_fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Text string  `json:"text"`
		IDs  []int64 `json:"ids"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		got := tok.Encode(c.Text, 64)
		want := append(slices.Clone(c.IDs), make([]int64, 64-len(c.IDs))...)
		if !slices.Equal(got, want) {
			t.Errorf("%q:\n got %v\nwant %v", c.Text, got, want)
		}
	}
}

func TestProbabilityRoundTrip(t *testing.T) {
	m := Model{LogitScale: 112.67, LogitBias: -16.77}
	for _, p := range []float32{0.01, 0.1, 0.5, 0.9} {
		if got := m.Probability(m.Cosine(p)); got < p*0.999 || got > p*1.001 {
			t.Errorf("p %v -> cos %v -> p %v", p, m.Cosine(p), got)
		}
	}
}

// The shipped text model stores its token table as int8; it must agree with
// the original fp16 export (deps/src) closely enough to rank the same way.
func TestShrunkTextModelAgrees(t *testing.T) {
	m := model(t)
	src := filepath.Join(testenv.Root(t), "deps", "src", "siglip2", "text_model_fp16.onnx")
	if _, err := os.Stat(src); err != nil {
		t.Skip("original text model not in deps/src")
	}
	texts := []string{"a photo of a dog", "a beach at sunset", "a screenshot of source code", "a receipt",
		"a group photo", "birthday cake with candles", "snow-covered mountains", "a handwritten note",
		"a cat sleeping on a sofa", "fireworks over a city", "東京タワー", "a bar chart"}
	shipped, err := OpenTextEncoder(m)
	if err != nil {
		t.Fatal(err)
	}
	defer shipped.Close()
	orig, err := openTextEncoder(m, src)
	if err != nil {
		t.Fatal(err)
	}
	defer orig.Close()
	a, err := shipped.Encode(texts)
	if err != nil {
		t.Fatal(err)
	}
	b, err := orig.Encode(texts)
	if err != nil {
		t.Fatal(err)
	}
	worst := float32(1)
	for i := range texts {
		worst = min(worst, dot(a[i], b[i]))
	}
	t.Logf("int8 token table vs fp16: worst cosine %.5f over %d texts", worst, len(texts))
	if worst < 0.995 {
		t.Errorf("worst cosine %.4f, want >= 0.995", worst)
	}
}

// Solid colours are the one image content a test can make without shipping
// photos; SigLIP must still pair each with its description, on both devices.
func TestImagesMatchTheirDescriptions(t *testing.T) {
	m := model(t)
	side := m.ImageSize
	solid := func(r, g, b byte) []byte {
		p := make([]byte, side*side*3)
		for i := 0; i < len(p); i += 3 {
			p[i], p[i+1], p[i+2] = r, g, b
		}
		return p
	}
	images := [][]byte{solid(220, 20, 20), solid(20, 40, 220), solid(20, 180, 40)}
	texts := []string{"a plain red image", "a plain blue image", "a plain green image"}

	te, err := OpenTextEncoder(m)
	if err != nil {
		t.Fatal(err)
	}
	defer te.Close()
	tv, err := te.Encode(texts)
	if err != nil {
		t.Fatal(err)
	}
	var byDevice [][][]float32
	for _, dev := range []Device{GPU, CPU} {
		ie, err := OpenImageEncoder(m, dev)
		if err != nil {
			t.Fatal(err)
		}
		iv, err := ie.Encode(images)
		got := ie.Device()
		ie.Close()
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("asked for %s, ran on %s", dev, got)
		for i, v := range iv {
			best := 0
			for j := range tv {
				if dot(v, tv[j]) > dot(v, tv[best]) {
					best = j
				}
			}
			if best != i {
				t.Errorf("%s: image %d best matches %q", dev, i, texts[best])
			}
		}
		byDevice = append(byDevice, iv)
	}
	for i := range images {
		if c := dot(byDevice[0][i], byDevice[1][i]); c < 0.99 {
			t.Errorf("image %d: GPU and CPU embeddings differ (cosine %.4f)", i, c)
		}
	}
}

func dot(a, b []float32) float32 {
	var s float32
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}

// A model whose reload failed (GPU lost, CPU load failed) must report an
// error on every later run, never crash.
func TestClosedSessionErrors(t *testing.T) {
	var s *session
	if _, err := s.run(nil); err != errClosed {
		t.Errorf("nil session: %v", err)
	}
	s = &session{device: GPU}
	s.close()
	if _, err := s.run(nil); err != errClosed {
		t.Errorf("closed session: %v", err)
	}
}
