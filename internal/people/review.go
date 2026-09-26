package people

import (
	"cmp"
	"slices"

	"lumen/internal/ai"
	"lumen/internal/core"
)

// OddOnesFirst orders a person's faces least like the rest first (vecs[i]
// is faces[i]'s identity vector), so a mistake in who is who shows at the
// top of the review. Faces the user placed go last: they are settled.
func OddOnesFirst(faces []core.Face, vecs [][]float32) []core.Face {
	if len(faces) == 0 {
		return faces
	}
	mean := make([]float32, len(vecs[0]))
	for _, v := range vecs {
		add(mean, v)
	}
	type scoredFace struct {
		f core.Face
		s float32
	}
	all := make([]scoredFace, len(faces))
	for i, f := range faces {
		all[i] = scoredFace{f, ai.Dot(vecs[i], mean)}
	}
	slices.SortStableFunc(all, func(a, b scoredFace) int {
		if a.f.ByUser != b.f.ByUser {
			if a.f.ByUser {
				return 1
			}
			return -1
		}
		return cmp.Compare(a.s, b.s)
	})
	out := make([]core.Face, len(all))
	for i, x := range all {
		out[i] = x.f
	}
	return out
}
