//go:build !windows

package update

import (
	"context"
	"lumen/internal/core"
)

func automaticSafe(context.Context, core.UpdatePolicy) bool { return false }
