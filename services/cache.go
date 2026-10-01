package services

import "context"

// SetThumbCacheMB sets the thumbnail cache's size cap. The least recently
// viewed thumbnails are dropped in the background to fit, and made again
// when next shown.
func (s *LibraryService) SetThumbCacheMB(ctx context.Context, mb int) error {
	return unbuilt(ctx, mb) // Phase 5 unit U3
}

// ThumbCacheBytes is the thumbnail cache's current size on disk.
func (m *MediaService) ThumbCacheBytes() int64 {
	return 0 // Phase 5 unit U3
}
