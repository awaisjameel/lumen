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

// Normalise scales v to unit length in place. It reports false, leaving v
// as it was, when v has no direction: all zeros, or not finite (a model
// running in half precision can overflow).
func Normalise(v []float32) bool {
	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	if norm == 0 || math.IsNaN(norm) || math.IsInf(norm, 0) {
		return false
	}
	inv := float32(1 / math.Sqrt(norm))
	for i := range v {
		v[i] *= inv
	}
	return true
}

// unitRows splits the first n rows of a [*, dim] output into unit vectors.
// A row that is not a usable vector (see Normalise) is nil, so one bad input
// fails alone rather than with its whole batch.
func unitRows(data []float32, dim, n int) ([][]float32, error) {
	if len(data) < n*dim {
		return nil, fmt.Errorf("model output has %d values, want at least %d", len(data), n*dim)
	}
	out := make([][]float32, n)
	for i := range n {
		if v := data[i*dim : (i+1)*dim : (i+1)*dim]; Normalise(v) {
			out[i] = v
		}
	}
	return out, nil
}
