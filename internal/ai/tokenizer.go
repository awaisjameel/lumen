package ai

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

// Special token ids of the Gemma vocabulary SigLIP 2 uses.
const (
	padID = 0
	eosID = 1
)

// Tokenizer is SigLIP 2's Gemma BPE: spaces become "▁" and the whole text
// is one word; unknown characters fall back to byte tokens. It works on token
// ids only (no strings), read from tokenizer.bin, which
// scripts/prepare_models.py writes from Hugging Face's tokenizer.json.
type Tokenizer struct {
	chars  map[rune]int32   // single-character tokens
	bytes  [256]int32       // byte-fallback tokens <0x00>..<0xFF>
	merges map[uint64]merge // pair (a<<32 | b) -> merge
}

type merge struct{ rank, id int32 }

// LoadTokenizer reads tokenizer.bin (format in scripts/prepare_models.py).
func LoadTokenizer(path string) (*Tokenizer, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	bad := func(what string) error { return fmt.Errorf("tokenizer %s: %s", path, what) }
	if !bytes.HasPrefix(raw, []byte("LTOK")) {
		return nil, bad("not a tokenizer file")
	}
	r := reader{b: raw[4:]}
	if v := r.u32(); v != 1 {
		return nil, bad(fmt.Sprintf("version %d, want 1", v))
	}
	t := &Tokenizer{}
	n := r.u32()
	t.chars = make(map[rune]int32, n)
	for range n {
		c, id := r.u32(), r.u32()
		t.chars[rune(c)] = int32(id)
	}
	for i := range t.bytes {
		t.bytes[i] = int32(r.u32())
	}
	n = r.u32()
	t.merges = make(map[uint64]merge, n)
	for rank := range n {
		a, b, id := r.u32(), r.u32(), r.u32()
		k := uint64(a)<<32 | uint64(b)
		if _, dup := t.merges[k]; !dup { // the first (lowest-ranked) wins
			t.merges[k] = merge{rank: int32(rank), id: int32(id)}
		}
	}
	if r.err != nil || len(r.b) != 0 {
		return nil, bad("truncated or corrupt")
	}
	return t, nil
}

// Encode returns exactly n token ids for text: lower-cased, at most n-1
// tokens, then <eos>, then <pad> to fill (as SigLIP 2 was trained).
func (t *Tokenizer) Encode(text string, n int) []int64 {
	s := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(text)), " ", "▁")
	syms := make([]int32, 0, len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if id, ok := t.chars[r]; ok && size > 0 && !(r == utf8.RuneError && size == 1) {
			syms = append(syms, id)
		} else {
			for _, b := range []byte(s[i : i+size]) {
				syms = append(syms, t.bytes[b])
			}
		}
		i += size
	}
	// Repeatedly merge the adjacent pair with the lowest rank (leftmost on ties).
	for len(syms) > 1 {
		best, at := merge{rank: -1}, -1
		for i := 0; i+1 < len(syms); i++ {
			if m, ok := t.merges[uint64(syms[i])<<32|uint64(syms[i+1])]; ok && (at < 0 || m.rank < best.rank) {
				best, at = m, i
			}
		}
		if at < 0 {
			break
		}
		syms[at] = best.id
		syms = append(syms[:at+1], syms[at+2:]...)
	}
	ids := make([]int64, n)
	k := min(len(syms), n-1)
	for i, id := range syms[:k] {
		ids[i] = int64(id)
	}
	ids[k] = eosID
	for i := k + 1; i < n; i++ {
		ids[i] = padID
	}
	return ids
}

// reader decodes little-endian uint32s, remembering the first error.
type reader struct {
	b   []byte
	err error
}

func (r *reader) u32() uint32 {
	if len(r.b) < 4 {
		r.err = errors.New("short read")
		return 0
	}
	v := binary.LittleEndian.Uint32(r.b)
	r.b = r.b[4:]
	return v
}
