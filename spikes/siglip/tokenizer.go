// Package siglip runs SigLIP 2 (image and text towers) for the Phase 0 spike.
package siglip

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

// SigLIP 2 text settings: lower-cased input, Gemma BPE, <eos> appended,
// right-padded with <pad> to 64 tokens. No <bos>.
const (
	MaxTokens = 64
	padID     = 0
	eosID     = 1
)

// Tokenizer is the Gemma BPE model from tokenizer.json: no pre-tokenizer
// (spaces become "▁" and stay inside the one "word"), byte fallback for
// characters outside the vocabulary.
type Tokenizer struct {
	vocab map[string]int64
	ranks map[string]int // "left right" -> merge rank
}

// LoadTokenizer reads a Hugging Face tokenizer.json.
func LoadTokenizer(path string) (*Tokenizer, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var raw struct {
		Model struct {
			Type   string           `json:"type"`
			Vocab  map[string]int64 `json:"vocab"`
			Merges [][2]string      `json:"merges"`
		} `json:"model"`
	}
	if err := json.NewDecoder(f).Decode(&raw); err != nil {
		return nil, err
	}
	if raw.Model.Type != "BPE" {
		return nil, fmt.Errorf("tokenizer model %q, want BPE", raw.Model.Type)
	}
	t := &Tokenizer{vocab: raw.Model.Vocab, ranks: make(map[string]int, len(raw.Model.Merges))}
	for i, m := range raw.Model.Merges {
		k := m[0] + " " + m[1]
		if _, dup := t.ranks[k]; !dup {
			t.ranks[k] = i
		}
	}
	return t, nil
}

// Encode returns exactly MaxTokens ids for text.
func (t *Tokenizer) Encode(text string) []int64 {
	s := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(text)), " ", "▁")
	syms := make([]string, 0, len(s))
	for _, r := range s {
		syms = append(syms, string(r))
	}
	// Repeatedly merge the adjacent pair with the lowest rank (leftmost wins).
	for len(syms) > 1 {
		best, at := -1, -1
		for i := 0; i+1 < len(syms); i++ {
			if r, ok := t.ranks[syms[i]+" "+syms[i+1]]; ok && (best < 0 || r < best) {
				best, at = r, i
			}
		}
		if at < 0 {
			break
		}
		syms[at] += syms[at+1]
		syms = append(syms[:at+1], syms[at+2:]...)
	}
	ids := make([]int64, 0, MaxTokens)
	for _, sym := range syms {
		if id, ok := t.vocab[sym]; ok {
			ids = append(ids, id)
			continue
		}
		for i := 0; i < len(sym); { // byte fallback
			_, n := utf8.DecodeRuneInString(sym[i:])
			for _, b := range []byte(sym[i : i+n]) {
				ids = append(ids, t.vocab[fmt.Sprintf("<0x%02X>", b)])
			}
			i += n
		}
	}
	if len(ids) > MaxTokens-1 {
		ids = ids[:MaxTokens-1]
	}
	ids = append(ids, eosID)
	for len(ids) < MaxTokens {
		ids = append(ids, padID)
	}
	return ids
}
