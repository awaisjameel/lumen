package search

import (
	"bytes"
	_ "embed"
	"encoding/gob"
	"log/slog"
	"math"
	"sync"

	"lumen/internal/ai"
)

// prompts.bin holds the text model's vectors for every prompt of the
// built-in Things, made on the machine that built Lumen (see
// TestShippedPromptVectors). A new library learns the Things from it instead
// of running the text model on the CPU for half a minute before any photo can
// be embedded. Prompts it lacks (a user's own Things, or Things added since it
// was made) are encoded as before.
//
//go:embed prompts.bin
var promptsBin []byte

// promptFile is prompts.bin's content.
type promptFile struct {
	Model   string // the ai.Model Version the vectors belong to
	Dim     int
	Prompts []string
	Vecs    []int16 // Dim per prompt: a unit vector's components times math.MaxInt16
}

// shippedPrompts decodes prompts.bin once.
var shippedPrompts = sync.OnceValue(func() promptFile {
	var f promptFile
	if err := gob.NewDecoder(bytes.NewReader(promptsBin)).Decode(&f); err != nil {
		slog.Error("read shipped prompt vectors", "err", err)
		return promptFile{}
	}
	if len(f.Vecs) != len(f.Prompts)*f.Dim {
		slog.Error("shipped prompt vectors are incomplete", "prompts", len(f.Prompts), "values", len(f.Vecs))
		return promptFile{}
	}
	return f
})

// shippedVectors returns the shipped vector of each prompt for m, as unit
// vectors, or nil if they were made with another model.
func shippedVectors(m ai.Model) map[string][]float32 {
	f := shippedPrompts()
	if f.Model != m.Version || f.Dim != m.Dim {
		return nil
	}
	out := make(map[string][]float32, len(f.Prompts))
	for i, p := range f.Prompts {
		q := f.Vecs[i*f.Dim : (i+1)*f.Dim]
		v := make([]float32, f.Dim)
		for k, x := range q {
			v[k] = float32(x) / math.MaxInt16
		}
		if ai.Normalise(v) {
			out[p] = v
		}
	}
	return out
}

// encodePromptFile quantizes vectors (unit vectors, one per prompt) into
// prompts.bin's format.
func encodePromptFile(model string, dim int, prompts []string, vecs [][]float32) ([]byte, error) {
	f := promptFile{Model: model, Dim: dim, Prompts: prompts, Vecs: make([]int16, 0, len(prompts)*dim)}
	for _, v := range vecs {
		for _, x := range v {
			f.Vecs = append(f.Vecs, int16(math.Round(float64(max(-1, min(1, x)))*math.MaxInt16)))
		}
	}
	var b bytes.Buffer
	err := gob.NewEncoder(&b).Encode(f)
	return b.Bytes(), err
}
