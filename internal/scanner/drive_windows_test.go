//go:build windows

package scanner

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"testing"
)

func TestSeekPenaltyRequiresACompleteDescriptor(t *testing.T) {
	raw := make([]byte, 12)
	binary.LittleEndian.PutUint32(raw[:4], 12)
	binary.LittleEndian.PutUint32(raw[4:8], 12)
	for _, seeks := range []byte{0, 1} {
		raw[8] = seeks
		got, err := seekPenaltyDescriptor(raw, 12)
		if err != nil || got != (seeks == 1) {
			t.Fatalf("descriptor=%d: %v, %v", seeks, got, err)
		}
	}
	for _, tc := range []struct {
		name     string
		raw      []byte
		returned uint32
	}{
		{"short return", raw, 8}, {"short buffer", raw[:8], 12}, {"invalid header", make([]byte, 12), 12},
		{"invalid boolean", append(append([]byte{}, raw[:8]...), 2, 0, 0, 0), 12},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := seekPenaltyDescriptor(tc.raw, tc.returned); err == nil {
				t.Fatal("invalid drive response accepted")
			}
		})
	}
}

func TestDriveProbeReportsCancellationBeforeOpeningTheVolume(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := driveSeekPenalty(ctx, `Z:\missing`); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled probe=%v", err)
	}
}

func BenchmarkDriveSeekPenalty(b *testing.B) {
	root := os.Getenv("LUMEN_SCAN_BENCH_ROOT")
	if root == "" {
		b.Fatal("LUMEN_SCAN_BENCH_ROOT must name a folder on the drive to probe")
	}
	for range b.N {
		seeks, err := driveSeekPenalty(b.Context(), root)
		if err != nil {
			b.Fatal(err)
		}
		b.Logf("seek_penalty=%v", seeks)
	}
}
