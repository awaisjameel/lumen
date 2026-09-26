package ai

import (
	"fmt"
	"math"
)

// Dot is the dot product of two vectors of one length: the cosine
// similarity of the unit vectors the models return.
func Dot(a, b []float32) float32 {
	var s float32
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}

// unitRows splits the first n rows of a [*, dim] output into unit vectors.
func unitRows(data []float32, dim, n int) ([][]float32, error) {
	if len(data) < n*dim {
		return nil, fmt.Errorf("model output has %d values, want at least %d", len(data), n*dim)
	}
	out := make([][]float32, n)
	for i := range n {
		v := data[i*dim : (i+1)*dim : (i+1)*dim]
		var norm float64
		for _, x := range v {
			norm += float64(x) * float64(x)
		}
		if norm == 0 {
			return nil, fmt.Errorf("model output %d is all zeros", i)
		}
		inv := float32(1 / math.Sqrt(norm))
		for j := range v {
			v[j] *= inv
		}
		out[i] = v
	}
	return out, nil
}
