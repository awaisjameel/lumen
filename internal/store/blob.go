package store

import (
	"encoding/binary"
	"math"
)

// Vectors are stored as BLOBs: int8 image embeddings (one byte per value)
// and float32 prompt embeddings (little-endian).

func int8Bytes(v []int8) []byte {
	b := make([]byte, len(v))
	for i, x := range v {
		b[i] = byte(x)
	}
	return b
}

// bytesInt8 appends b to dst as int8s.
func bytesInt8(dst []int8, b []byte) []int8 {
	for _, x := range b {
		dst = append(dst, int8(x))
	}
	return dst
}

func float32Bytes(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(x))
	}
	return b
}

// bytesFloat32 decodes float32Bytes; a malformed blob decodes to nil.
func bytesFloat32(b []byte) []float32 {
	if len(b)%4 != 0 {
		return nil
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return v
}
