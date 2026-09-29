package search

import (
	"flag"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"lumen/internal/ai"
	"lumen/internal/testenv"
)

var updatePrompts = flag.Bool("update-prompts", false,
	"encode the built-in Things' prompts with the text model into prompts.bin")

// vocabularyPrompts lists every built-in prompt once, sorted.
func vocabularyPrompts() []string {
	var all []string
	for _, l := range Vocabulary {
		all = append(all, l.Prompts...)
	}
	slices.Sort(all)
	return slices.Compact(all)
}

// Every built-in prompt ships with its vector. If this fails, the Vocabulary
// changed: run go test ./internal/search -run TestShippedPromptVectors -update-prompts
// (it needs deps/runtime).
func TestShippedPromptsMatchTheVocabulary(t *testing.T) {
	f := shippedPrompts()
	if f.Model == "" {
		t.Fatal("prompts.bin is unreadable")
	}
	want := vocabularyPrompts()
	if !slices.Equal(f.Prompts, want) {
		var missing, extra []string
		for _, p := range want {
			if !slices.Contains(f.Prompts, p) {
				missing = append(missing, p)
			}
		}
		for _, p := range f.Prompts {
			if !slices.Contains(want, p) {
				extra = append(extra, p)
			}
		}
		t.Errorf("prompts.bin is out of date (missing %q, extra %q): run with -update-prompts", missing, extra)
	}
}

// The shipped vectors are what the text model makes, for the model Lumen
// ships. With -update-prompts, it writes them instead.
func TestShippedPromptVectors(t *testing.T) {
	rt := testenv.Runtime(t)
	if err := ai.Init(rt); err != nil {
		t.Fatal(err)
	}
	m, err := ai.LoadModel(filepath.Join(rt, "models", "siglip2"))
	if err != nil {
		t.Fatal(err)
	}
	enc, err := ai.OpenTextEncoder(m)
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	encode := func(prompts []string) [][]float32 {
		var out [][]float32
		for lo := 0; lo < len(prompts); lo += textBatch {
			vecs, err := enc.Encode(prompts[lo:min(lo+textBatch, len(prompts))])
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, vecs...)
		}
		return out
	}

	prompts := vocabularyPrompts()
	if *updatePrompts {
		b, err := encodePromptFile(m.Version, m.Dim, prompts, encode(prompts))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile("prompts.bin", b, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %d prompts for %s (%d bytes)", len(prompts), m.Version, len(b))
		return
	}
	shipped := shippedVectors(m)
	if shipped == nil {
		t.Fatalf("prompts.bin was made with model %q, not %q: run with -update-prompts", shippedPrompts().Model, m.Version)
	}
	var sample []string
	for i := 0; i < len(prompts); i += 16 {
		sample = append(sample, prompts[i])
	}
	for i, v := range encode(sample) {
		if cos := ai.Dot(v, shipped[sample[i]]); cos < 0.9999 {
			t.Errorf("%q: shipped vector is %.5f from the model's", sample[i], cos)
		}
	}
}
