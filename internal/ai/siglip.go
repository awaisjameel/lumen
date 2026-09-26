package ai

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

// Model describes the prepared SigLIP 2 files (siglip2.json, written by
// scripts/prepare_models.py).
type Model struct {
	Dir        string  `json:"-"`
	Version    string  `json:"version"`   // stored with every embedding; a change re-runs the stage
	Batch      int     `json:"batch"`     // images per vision run (fixed input shape)
	ImageSize  int     `json:"imageSize"` // square input side
	MaxTokens  int     `json:"maxTokens"` // text input length
	Dim        int     `json:"dim"`       // embedding size
	LogitScale float64 `json:"logitScale"`
	LogitBias  float64 `json:"logitBias"`
}

// LoadModel reads the model description in dir.
func LoadModel(dir string) (Model, error) {
	m := Model{Dir: dir}
	raw, err := os.ReadFile(filepath.Join(dir, "siglip2.json"))
	if err != nil {
		return m, fmt.Errorf("SigLIP model missing: %w", err)
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return m, fmt.Errorf("read siglip2.json: %w", err)
	}
	if m.Version == "" || m.Batch <= 0 || m.ImageSize <= 0 || m.MaxTokens <= 1 || m.Dim <= 0 || m.LogitScale <= 0 {
		return m, fmt.Errorf("siglip2.json is incomplete: %+v", m)
	}
	return m, nil
}

// Probability turns a cosine similarity between an image and a text into
// SigLIP's own calibrated "this text describes this image" probability.
func (m Model) Probability(cos float32) float32 {
	return float32(1 / (1 + math.Exp(-(m.LogitScale*float64(cos) + m.LogitBias))))
}

// Cosine is Probability's inverse: the similarity that gives probability p.
func (m Model) Cosine(p float32) float32 {
	return float32((math.Log(float64(p)/(1-float64(p))) - m.LogitBias) / m.LogitScale)
}

// ImageEncoder embeds images. It prefers the GPU and falls back to the CPU
// if DirectML can't load the model or fails while running it.
type ImageEncoder struct {
	m   Model
	mu  sync.Mutex // one run at a time; the input buffer is reused
	s   *session
	buf []float32
}

// OpenImageEncoder loads the vision tower on device (GPU falls back to CPU).
func OpenImageEncoder(m Model, device Device) (*ImageEncoder, error) {
	e := &ImageEncoder{m: m, buf: make([]float32, m.Batch*3*m.ImageSize*m.ImageSize)}
	if err := e.open(device); err != nil {
		return nil, err
	}
	return e, nil
}

func (e *ImageEncoder) open(device Device) error {
	path := filepath.Join(e.m.Dir, "vision.onnx")
	// Background work: on the CPU leave half the cores for everything else.
	threads := max(1, runtime.NumCPU()/2)
	if device == GPU {
		s, err := openSession(path, GPU, threads)
		if err == nil {
			e.s = s
			return nil
		}
		slog.Warn("image model can't use the GPU; using the CPU", "err", err)
	}
	s, err := openSession(path, CPU, threads)
	if err != nil {
		return err
	}
	e.s = s
	return nil
}

// Device reports where the model is running (it can move to the CPU).
func (e *ImageEncoder) Device() Device {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.s.device
}

// BatchSize is how many images Encode takes at most.
func (e *ImageEncoder) BatchSize() int { return e.m.Batch }

// pixelScale maps a byte to SigLIP's input range: (x/255 - 0.5) / 0.5.
var pixelScale = func() (t [256]float32) {
	for i := range t {
		t[i] = float32(i)/127.5 - 1
	}
	return t
}()

// Encode embeds up to BatchSize images, each ImageSize x ImageSize 8-bit RGB
// (media.ModelInput), and returns one unit vector per image.
func (e *ImageEncoder) Encode(images [][]byte) ([][]float32, error) {
	s, plane := e.m.ImageSize, e.m.ImageSize*e.m.ImageSize
	if len(images) == 0 || len(images) > e.m.Batch {
		return nil, fmt.Errorf("encode %d images: batch holds 1 to %d", len(images), e.m.Batch)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	// Planar (channel-first) layout; the unused tail of a short batch is zero.
	clear(e.buf)
	for b, img := range images {
		if len(img) != plane*3 {
			return nil, fmt.Errorf("image %d is %d bytes, want %d", b, len(img), plane*3)
		}
		r, g, bl := e.buf[(b*3)*plane:], e.buf[(b*3+1)*plane:], e.buf[(b*3+2)*plane:]
		for px := range plane {
			r[px], g[px], bl[px] = pixelScale[img[px*3]], pixelScale[img[px*3+1]], pixelScale[img[px*3+2]]
		}
	}
	in, err := ort.NewTensor(ort.NewShape(int64(e.m.Batch), 3, int64(s), int64(s)), e.buf)
	if err != nil {
		return nil, err
	}
	defer in.Destroy()
	out, err := e.s.run(in)
	if err != nil && e.s.device == GPU {
		// A GPU can go away (driver update, device removed): carry on on the CPU.
		slog.Warn("image model failed on the GPU; switching to the CPU", "err", err)
		e.s.close() // if the CPU fails to load too, later runs report errClosed
		if oerr := e.open(CPU); oerr != nil {
			return nil, oerr
		}
		out, err = e.s.run(in)
	}
	if err != nil {
		return nil, fmt.Errorf("run image model: %w", err)
	}
	return unitRows(out[0], e.m.Dim, len(images))
}

// Close frees the model.
func (e *ImageEncoder) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.s.close()
}

// TextEncoder embeds text (search queries and tag prompts) on the CPU, where
// one short query takes tens of milliseconds.
type TextEncoder struct {
	m   Model
	tok *Tokenizer
	s   *session
}

// OpenTextEncoder loads the text tower and tokenizer.
func OpenTextEncoder(m Model) (*TextEncoder, error) {
	return openTextEncoder(m, filepath.Join(m.Dir, "text.onnx"))
}

func openTextEncoder(m Model, model string) (*TextEncoder, error) {
	tok, err := LoadTokenizer(filepath.Join(m.Dir, "tokenizer.bin"))
	if err != nil {
		return nil, err
	}
	s, err := openSession(model, CPU, 0)
	if err != nil {
		return nil, err
	}
	return &TextEncoder{m: m, tok: tok, s: s}, nil
}

// Encode embeds texts and returns one unit vector per text. Safe for
// concurrent use.
func (e *TextEncoder) Encode(texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	ids := make([]int64, 0, len(texts)*e.m.MaxTokens)
	for _, t := range texts {
		ids = append(ids, e.tok.Encode(t, e.m.MaxTokens)...)
	}
	in, err := ort.NewTensor(ort.NewShape(int64(len(texts)), int64(e.m.MaxTokens)), ids)
	if err != nil {
		return nil, err
	}
	defer in.Destroy()
	out, err := e.s.run(in)
	if err != nil {
		return nil, fmt.Errorf("run text model: %w", err)
	}
	return unitRows(out[0], e.m.Dim, len(texts))
}

// Close frees the model.
func (e *TextEncoder) Close() { e.s.close() }
