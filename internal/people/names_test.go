package people

import (
	"slices"
	"testing"

	"lumen/internal/core"
)

func TestFindNames(t *testing.T) {
	named := []core.Person{{ID: 1, Name: "Ali"}, {ID: 2, Name: "Ali Khan"}, {ID: 3, Name: "Sara"}, {ID: 4, Name: "Zoë"}, {ID: 5, Name: "Émile"}, {ID: 6, Name: "İbrahim"}}
	cases := []struct {
		query string
		ids   []int64
		rest  string
	}{
		{"Ali at the beach", []int64{1}, "at the beach"},
		{"ali khan birthday cake", []int64{2}, "birthday cake"}, // the longer name wins
		{"Ali and Sara", []int64{1, 3}, ""},                     // only fillers left
		{"Ali Khan and Ali Khan", []int64{2}, ""},
		{"photos of Sara", []int64{3}, ""},
		{"Alice in the snow", nil, "Alice in the snow"}, // whole words only
		{"Zoë's dog", []int64{4}, "'s dog"},
		{"a dog", nil, "a dog"},
		{"photos", nil, "photos"},                        // no name: fillers are the search
		{"ÉMILE at the lake", []int64{5}, "at the lake"}, // any case, any letters
		{"İbrahim ibrahim", []int64{6}, "ibrahim"},       // İ is its own letter; no panic
	}
	for _, c := range cases {
		ids, _, rest := FindNames(c.query, named)
		slices.Sort(ids)
		if !slices.Equal(ids, c.ids) || rest != c.rest {
			t.Errorf("%q: people %v, rest %q; want %v, %q", c.query, ids, rest, c.ids, c.rest)
		}
	}
}

func TestOddOnesFirst(t *testing.T) {
	faces := []core.Face{{ID: 1}, {ID: 2}, {ID: 3, ByUser: true}, {ID: 4}, {ID: 5}}
	vecs := [][]float32{{1, 0}, {1, 0}, {0, 1}, {0, 1}, {1, 0}} // 4 is unlike most; 3 too, but settled
	got := OddOnesFirst(faces, vecs)
	if got[0].ID != 4 || got[4].ID != 3 {
		t.Errorf("order = %v", got)
	}
}
