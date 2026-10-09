package services

import "context"

// SetThumbCacheMB sets the thumbnail cache's size cap. The least recently
// viewed thumbnails are dropped in the background to fit, and made again
// when next shown.
func (s *LibraryService) SetThumbCacheMB(ctx context.Context, mb int) error {
	mb, err := s.store.SetThumbCacheMB(ctx, mb)
	if err != nil {
		return err
	}
	s.ix.SetThumbCacheMB(mb) // takes effect at once: the eviction loop wakes
	return nil
}

// ThumbCacheBytes is the thumbnail cache's current size on disk.
func (m *MediaService) ThumbCacheBytes() int64 {
	return m.cache.Bytes()
}

// ViewingCacheBytes is how much disk the viewing copies take: the bounded
// copies the viewer shows of photos the WebView cannot display itself.
func (m *MediaService) ViewingCacheBytes() (int64, error) {
	return m.cache.RenderBytes()
}

// ClearViewingCache deletes the viewing copies and returns the bytes freed.
// Each is made again when its photo is next opened; originals, thumbnails
// and everything the library knows stay as they are.
func (m *MediaService) ClearViewingCache(ctx context.Context) (int64, error) {
	return m.cache.ClearRenders(ctx)
}
