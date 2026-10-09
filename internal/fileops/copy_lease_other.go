//go:build !windows

package fileops

import "context"

// LeaseCopyFolders checks the same regular-folder policy on other platforms.
func LeaseCopyFolders(ctx context.Context, dirs ...string) (func(), error) {
	for _, dir := range dirs {
		if err := exportFolder(ctx, dir); err != nil {
			return nil, err
		}
	}
	return func() {}, nil
}
