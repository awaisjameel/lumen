package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
)

// FullDigest hashes every byte, unlike the scanner's sampled large-file identity.
func FullDigest(ctx context.Context, in io.Reader) (digest string, size int64, err error) {
	h := sha256.New()
	var total int64
	buf := make([]byte, 64<<10)
	for {
		if err := ctx.Err(); err != nil {
			return "", total, err
		}
		n, err := in.Read(buf)
		if n > 0 {
			_, _ = h.Write(buf[:n])
			total += int64(n)
		}
		if errors.Is(err, io.EOF) {
			return hex.EncodeToString(h.Sum(nil)), total, nil
		}
		if err != nil {
			return "", total, err
		}
	}
}
