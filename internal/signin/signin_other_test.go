//go:build !windows

package signin

import (
	"errors"
	"testing"
)

func TestVerifyIsUnavailableWhereThereIsNoWindows(t *testing.T) {
	if err := Verify(t.Context(), 0, "test"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Verify: %v, want ErrUnavailable", err)
	}
}
