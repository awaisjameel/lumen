package siglip

import (
	"slices"
	"testing"
)

// Expected ids come from Hugging Face `tokenizers` on the same tokenizer.json.
func TestEncodeMatchesReference(t *testing.T) {
	tok, err := LoadTokenizer("../.cache/models/siglip2/tokenizer.json")
	if err != nil {
		t.Skip(err)
	}
	cases := map[string][]int64{
		"a photo of a dog":    {235250, 2686, 576, 476, 5929, 1},
		"Beach sunset":        {54901, 22097, 1},
		"people in an office": {15565, 575, 671, 4844, 1},
		"screenshot of code":  {42834, 576, 3409, 1},
		"café résumé 😀 naïve": {81453, 96537, 79937, 155245, 1},
		"Ali at the beach":    {2287, 696, 573, 8318, 1},
		"  two  spaces ":      {9721, 139, 46633, 1},
	}
	for text, want := range cases {
		got := tok.Encode(text)
		if len(got) != MaxTokens || !slices.Equal(got[:len(want)], want) || slices.ContainsFunc(got[len(want):], func(id int64) bool { return id != padID }) {
			t.Errorf("%q: got %v, want %v then padding", text, got[:len(want)+2], want)
		}
	}
}
