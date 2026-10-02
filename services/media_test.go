package services

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"lumen/internal/core"
	"lumen/internal/index"
	"lumen/internal/media"
	"lumen/internal/store"
	"lumen/internal/testenv"
)

func TestVideoMediaRoutes(t *testing.T) {
	ffmpeg := testenv.File(t, "ffmpeg", "ffmpeg.exe")
	ffprobe := testenv.File(t, "ffmpeg", "ffprobe.exe")
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "clip.mp4")
	cmd := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-y", "-f", "lavfi", "-i",
		"testsrc2=size=160x90:rate=5", "-t", "3", "-c:v", "libopenh264", "-b:v", "300k", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make sample: %v: %s", err, out)
	}
	st, err := store.Open(filepath.Join(dir, "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	src, err := st.AddSource(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Ingest(ctx, []store.IngestItem{{SourceID: src.ID, Path: path, Size: fi.Size(),
		Hash: []byte("video route fixture"), Kind: core.KindVideo, Ext: ".mp4", TakenAt: 1_700_000_000}}); err != nil {
		t.Fatal(err)
	}
	jobs, err := st.PendingJobs(ctx, core.StageMetadata, nil, 1)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs: %+v, %v", jobs, err)
	}
	id := jobs[0].AssetID
	d := media.Decoders{FFmpeg: ffmpeg, FFprobe: ffprobe}
	meta, err := d.Metadata(ctx, path, core.KindVideo)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveMetadata(ctx, "v", []store.MetadataResult{{AssetID: id, Width: meta.Width, Height: meta.Height,
		Duration: meta.Duration, VideoCodec: meta.VideoCodec, AudioCodec: meta.AudioCodec}}); err != nil {
		t.Fatal(err)
	}
	cache := media.NewCache(filepath.Join(dir, "cache"), d)
	thumb, err := d.Thumbnail(ctx, path, core.KindVideo, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.Write(id, thumb); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveThumbnails(ctx, "v", []store.ThumbResult{{AssetID: id}}); err != nil {
		t.Fatal(err)
	}
	stream, err := NewStreamer(st, d)
	if err != nil {
		t.Fatal(err)
	}
	go stream.Run()
	defer stream.Close()
	m := NewMediaService(st, cache, stream)
	for _, tc := range []struct {
		url, kind string
	}{
		{fmt.Sprintf("/thumb/%d/256?v=1", id), "image/jpeg"},
		{fmt.Sprintf("/sprite/%d?v=1", id), "image/jpeg"},
		{fmt.Sprintf("/frame/%d/1000", id), "image/jpeg"},
	} {
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, tc.url, http.NoBody))
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != tc.kind || rec.Body.Len() == 0 {
			t.Errorf("%s: status %d, content type %q, bytes %d", tc.url, rec.Code, rec.Header().Get("Content-Type"), rec.Body.Len())
		}
		if got := rec.Header().Get("Cache-Control"); got != "private, no-cache" {
			t.Errorf("%s: cache control = %q", tc.url, got)
		}
	}
	for _, url := range []string{fmt.Sprintf("/thumb/%d/300", id), fmt.Sprintf("/original/%d", id), "/thumb/x/256"} {
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", url, rec.Code)
		}
	}

	// Videos stream from the loopback server, really streamed.
	type response struct {
		status int
		header http.Header
		body   []byte
	}
	get := func(url, rng, host string) response {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
		if err != nil {
			t.Fatal(err)
		}
		if rng != "" {
			req.Header.Set("Range", rng)
		}
		if host != "" {
			req.Host = host
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response{res.StatusCode, res.Header, body}
	}
	base := m.VideoBase()
	if r := get(fmt.Sprintf("%s/video/%d", base, id), "bytes=0-15", ""); r.status != http.StatusPartialContent ||
		len(r.body) != 16 || r.header.Get("Content-Type") != "video/mp4" {
		t.Errorf("direct video Range: status %d, %d bytes, %q", r.status, len(r.body), r.header.Get("Content-Type"))
	}
	if r := get(fmt.Sprintf("%s/play/%d?at=1000", base, id), "", ""); r.status != http.StatusOK ||
		r.header.Get("Content-Type") != "video/mp4" || len(r.body) == 0 {
		t.Errorf("converted video: status %d, %q, %d bytes", r.status, r.header.Get("Content-Type"), len(r.body))
	}
	wrongSecret := strings.Replace(base, stream.secret, "0123", 1)
	if r := get(fmt.Sprintf("%s/video/%d", wrongSecret, id), "", ""); r.status != http.StatusNotFound {
		t.Errorf("wrong secret: status %d, want 404", r.status)
	}
	if r := get(fmt.Sprintf("%s/video/%d", base, id), "", "evil.example"); r.status != http.StatusForbidden {
		t.Errorf("other host: status %d, want 403", r.status)
	}
	blockID, err := st.SetBlock(ctx, core.BlockAsset, id, "", core.BlockPhotos)
	if err != nil {
		t.Fatal(err)
	}
	for _, url := range []string{fmt.Sprintf("/thumb/%d/256?v=1", id), fmt.Sprintf("/sprite/%d?v=1", id), fmt.Sprintf("/frame/%d/1000", id)} {
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody))
		if rec.Code != http.StatusNotFound {
			t.Errorf("blocked cached %s: status %d, want 404", url, rec.Code)
		}
	}
	for _, url := range []string{fmt.Sprintf("%s/video/%d", base, id), fmt.Sprintf("%s/play/%d", base, id)} {
		if r := get(url, "bytes=0-15", ""); r.status != http.StatusNotFound {
			t.Errorf("blocked video %s: status %d, want 404", url, r.status)
		}
	}
	if err := st.RemoveBlock(ctx, blockID); err != nil {
		t.Fatal(err)
	}
	if r := get(fmt.Sprintf("%s/video/%d", base, id), "bytes=0-15", ""); r.status != http.StatusPartialContent {
		t.Errorf("unblocked video: status %d, want 206", r.status)
	}
}

// fakeDecoder makes thumbnails of fixed bytes, counting how often it is asked.
type fakeDecoder struct {
	media.Decoder
	calls atomic.Int32
}

func (d *fakeDecoder) Thumbnail(context.Context, string, core.Kind, int) (media.Thumbs, error) {
	d.calls.Add(1)
	return media.Thumbs{Small: []byte("small thumbnail"), Large: []byte("large thumbnail, rebuilt")}, nil
}

// shownLibrary is n photos with a cached thumbnail pair each, served by a
// MediaService whose view flusher writes through a counting sink.
type shownLibrary struct {
	st    *store.Store
	cache *media.Cache
	dec   *fakeDecoder
	m     *MediaService
	sink  *countingSink
	ids   []int64
}

// countingSink counts the calls that reach the database.
type countingSink struct {
	media.ViewSink
	calls atomic.Int32
	mu    sync.Mutex // guards last
	last  map[int64]int64
}

func (s *countingSink) TouchThumbs(ctx context.Context, views map[int64]int64) error {
	s.calls.Add(1)
	s.mu.Lock()
	s.last = views
	s.mu.Unlock()
	return s.ViewSink.TouchThumbs(ctx, views)
}

// batch is the views of the latest call.
func (s *countingSink) batch() map[int64]int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

func newShownLibrary(t *testing.T, n int) *shownLibrary {
	t.Helper()
	ctx := t.Context()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	src, err := st.AddSource(ctx, filepath.Join(dir, "photos"))
	if err != nil {
		t.Fatal(err)
	}
	items := make([]store.IngestItem, n)
	for i := range items {
		path := filepath.Join(dir, "photos", fmt.Sprintf("%d.jpg", i+1))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
			t.Fatal(err)
		}
		items[i] = store.IngestItem{SourceID: src.ID, Path: path, Size: 8, MTime: 1, Hash: []byte(path),
			Kind: core.KindPhoto, Ext: ".jpg", TakenAt: 1_700_000_000, DateFrom: core.DateFromFile}
	}
	if _, err := st.Ingest(ctx, items); err != nil {
		t.Fatal(err)
	}
	dec := &fakeDecoder{}
	cache := media.NewCache(filepath.Join(dir, "thumbs"), dec)
	lib := &shownLibrary{st: st, cache: cache, dec: dec}
	var meta []store.MetadataResult
	var thumbs []store.ThumbResult
	for id := int64(1); id <= int64(n); id++ {
		lib.ids = append(lib.ids, id)
		meta = append(meta, store.MetadataResult{AssetID: id, Width: 4, Height: 3, Orientation: 1})
		thumbs = append(thumbs, store.ThumbResult{AssetID: id})
		if err := cache.Write(id, media.Thumbs{Small: []byte("small thumbnail"), Large: []byte("large thumbnail"),
			Sprite: []byte("sprite")}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SaveMetadata(ctx, "v", meta); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveThumbnails(ctx, "v", thumbs); err != nil {
		t.Fatal(err)
	}
	lib.sink = &countingSink{ViewSink: st}
	lib.m = NewMediaService(st, cache, nil)
	lib.m.views.sink = lib.sink
	return lib
}

func (l *shownLibrary) get(t *testing.T, url string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	l.m.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, url, http.NoBody))
	return rec
}

func TestEvictedThumbnailIsRebuiltOnItsNextRequest(t *testing.T) {
	l := newShownLibrary(t, 1)
	if rec := l.get(t, "/thumb/1/1024?v=1"); rec.Code != http.StatusOK || rec.Body.String() != "large thumbnail" {
		t.Fatalf("cached: status %d, body %q", rec.Code, rec.Body.String())
	}
	if l.dec.calls.Load() != 0 {
		t.Fatal("a cached thumbnail was decoded")
	}
	l.cache.ViewsSaved(l.cache.PendingViews()) // the database has heard of the view: now evictable
	if freed, err := l.cache.Evict(t.Context(), []int64{1}); err != nil || freed == 0 {
		t.Fatalf("evict: freed %d, %v", freed, err)
	}
	before := l.cache.Bytes()
	rec := l.get(t, "/thumb/1/1024?v=1")
	if rec.Code != http.StatusOK || rec.Body.String() != "large thumbnail, rebuilt" {
		t.Fatalf("after eviction: status %d, body %q", rec.Code, rec.Body.String())
	}
	if l.dec.calls.Load() != 1 {
		t.Fatalf("%d rebuilds, want 1", l.dec.calls.Load())
	}
	if l.m.ThumbCacheBytes() <= before {
		t.Fatalf("the rebuilt thumbnails were not counted: %d bytes, was %d", l.m.ThumbCacheBytes(), before)
	}
}

func TestManyThumbnailRequestsMakeOneDatabaseWrite(t *testing.T) {
	l := newShownLibrary(t, 20)
	for range 5 { // the grid scrolls over the same photos again and again
		for _, id := range l.ids {
			if rec := l.get(t, fmt.Sprintf("/thumb/%d/256?v=1", id)); rec.Code != http.StatusOK {
				t.Fatalf("thumbnail %d: status %d", id, rec.Code)
			}
		}
	}
	if rec := l.get(t, "/sprite/3?v=1"); rec.Code != http.StatusOK {
		t.Fatalf("sprite: status %d", rec.Code)
	}
	if n := l.sink.calls.Load(); n != 0 {
		t.Fatalf("%d database writes while 101 requests were served, want none", n)
	}
	if got := len(l.cache.PendingViews()); got != 20 {
		t.Fatalf("%d assets waiting to be saved, want 20", got)
	}

	l.m.views.flush(t.Context())
	if n := l.sink.calls.Load(); n != 1 {
		t.Fatalf("%d database writes for the flush, want 1", n)
	}
	if got := len(l.sink.batch()); got != 20 {
		t.Fatalf("the one write held %d assets, want 20", got)
	}
	if got := len(l.cache.PendingViews()); got != 0 {
		t.Fatalf("%d views still waiting after the write", got)
	}
	l.m.views.flush(t.Context())
	if n := l.sink.calls.Load(); n != 1 {
		t.Fatalf("an empty flush wrote again (%d calls)", n)
	}
}

func TestViewsAreSavedOnATimerWithoutRewritingWhenNothingWasShown(t *testing.T) {
	l := newShownLibrary(t, 3)
	l.m.views.every = 10 * time.Millisecond
	check(t, l.m.ServiceStartup(t.Context(), application.ServiceOptions{}))
	defer func() { check(t, l.m.ServiceShutdown()) }()
	if rec := l.get(t, "/thumb/1/256?v=1"); rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	deadline := time.Now().Add(10 * time.Second)
	for l.sink.calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(60 * time.Millisecond) // several more ticks, with nothing shown
	if n := l.sink.calls.Load(); n != 1 {
		t.Fatalf("%d writes, want 1", n)
	}
}

func TestShutdownSavesTheViewsStillInMemory(t *testing.T) {
	l := newShownLibrary(t, 3)
	check(t, l.m.ServiceStartup(t.Context(), application.ServiceOptions{})) // the 30 s timer never fires here
	if rec := l.get(t, "/thumb/2/256?v=1"); rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if n := l.sink.calls.Load(); n != 0 {
		t.Fatalf("%d writes before shutdown, want none", n)
	}
	check(t, l.m.ServiceShutdown())
	if n := l.sink.calls.Load(); n != 1 {
		t.Fatalf("shutdown made %d writes, want 1", n)
	}
	if got := l.sink.batch(); len(got) != 1 || got[2] == 0 {
		t.Fatalf("shutdown saved %v, want asset 2's view", got)
	}
}

func TestThumbCacheCapSettingIsStoredClampedAndReachesTheCache(t *testing.T) {
	ctx := t.Context()
	l := newShownLibrary(t, 1)
	ix := index.New(l.st, core.PathsIn(t.TempDir()), l.cache, index.Models{}, quietEvents{})
	s := NewLibraryService(l.st, ix)
	if got, err := s.Settings(ctx); err != nil || got.ThumbCacheMB != 2048 {
		t.Fatalf("default cap = %d MB (%v), want 2048", got.ThumbCacheMB, err)
	}
	for _, tc := range []struct{ set, want int }{{4096, 4096}, {10, core.ThumbCacheMinMB}, {1 << 30, core.ThumbCacheMaxMB}} {
		check(t, s.SetThumbCacheMB(ctx, tc.set))
		got, err := s.Settings(ctx)
		if err != nil || got.ThumbCacheMB != tc.want {
			t.Errorf("set %d: settings show %d MB (%v), want %d", tc.set, got.ThumbCacheMB, err, tc.want)
		}
		if l.cache.Cap() != int64(tc.want)<<20 {
			t.Errorf("set %d: cache cap = %d bytes, want %d MB", tc.set, l.cache.Cap(), tc.want)
		}
	}
	// Without an indexer (as in these tests) the setting is still stored.
	check(t, NewLibraryService(l.st, nil).SetThumbCacheMB(ctx, 512))
}

func TestThumbCacheBytesIsTheCachesRunningTotal(t *testing.T) {
	l := newShownLibrary(t, 2)
	want := int64(2 * (len("small thumbnail") + len("large thumbnail") + len("sprite")))
	if got := l.m.ThumbCacheBytes(); got != want {
		t.Fatalf("ThumbCacheBytes = %d, want %d", got, want)
	}
}

// quietEvents is an indexer's event sink that ignores everything.
type quietEvents struct{}

func (quietEvents) Progress(core.Progress)      {}
func (quietEvents) LibraryChanged(core.Changes) {}

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestAThumbnailEvictedWhileBeingServedIsRebuiltInsteadOfAnswering404(t *testing.T) {
	l := newShownLibrary(t, 1)
	opens := 0
	l.m.open = func(path string) (*os.File, error) {
		opens++
		if opens == 1 { // the cap evicts the file after it was seen and before it is opened
			if _, evicted, err := l.cache.EvictOne(t.Context(), 1); err != nil || !evicted {
				t.Fatalf("setup eviction: %v, %v", evicted, err)
			}
		}
		return core.OpenShared(path)
	}
	rec := l.get(t, "/thumb/1/1024?v=1")
	if rec.Code != http.StatusOK || rec.Body.String() != "large thumbnail, rebuilt" {
		t.Fatalf("status %d, body %q, want the rebuilt thumbnail", rec.Code, rec.Body.String())
	}
}
