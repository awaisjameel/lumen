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
	stream, err := NewStreamer(st, d, nil)
	if err != nil {
		t.Fatal(err)
	}
	go stream.Run()
	defer stream.Close()
	m := NewMediaService(st, cache, stream, nil)
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
	lib.m = NewMediaService(st, cache, nil, nil)
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
	s := NewLibraryService(l.st, ix, nil)
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
	check(t, NewLibraryService(l.st, nil, nil).SetThumbCacheMB(ctx, 512))
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

// coveredMedia is a library of four assets, each with cached pictures, served
// by a MediaService and a Streamer that share one gate: a photo and a video
// that an asset block covers, and a photo and a video nothing covers. No FFmpeg is needed: the pictures are in the cache already and
// the converter is a fake.
type coveredMedia struct {
	st                                 *store.Store
	gate                               *BlockGate
	m                                  *MediaService
	stream                             *Streamer
	photo, video, openPhoto, openVideo int64
	clock                              *time.Time // the gate's time, which a test moves
}

// fixedConversion is a converter whose output is a few bytes.
type fixedConversion struct{}

func (fixedConversion) ConvertMP4(context.Context, string, int64) (func(io.Writer) error, error) {
	return func(w io.Writer) error { _, err := w.Write([]byte("converted")); return err }, nil
}

func newCoveredMedia(t *testing.T) *coveredMedia {
	t.Helper()
	ctx := t.Context()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	src, err := st.AddSource(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	c := &coveredMedia{st: st, gate: &BlockGate{}}
	now := time.Now()
	c.clock = &now
	c.gate.clock = func() time.Time { return now }
	files := []struct {
		name string
		kind core.Kind
		ext  string
		id   *int64
	}{
		{"covered", core.KindPhoto, ".jpg", &c.photo},
		{"covered", core.KindVideo, ".mp4", &c.video},
		{"open", core.KindPhoto, ".jpg", &c.openPhoto},
		{"open", core.KindVideo, ".mp4", &c.openVideo},
	}
	cache := media.NewCache(filepath.Join(dir, "thumbs"), &fakeDecoder{})
	var meta []store.MetadataResult
	var thumbs []store.ThumbResult
	for i, f := range files {
		path := filepath.Join(dir, fmt.Sprintf("%d%s", i, f.ext))
		if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := st.Ingest(ctx, []store.IngestItem{{SourceID: src.ID, Path: path, Size: 10, MTime: 1,
			Hash: []byte(path), Kind: f.kind, Ext: f.ext, TakenAt: 1_700_000_000, DateFrom: core.DateFromFile}}); err != nil {
			t.Fatal(err)
		}
		id := int64(i + 1)
		*f.id = id
		m := store.MetadataResult{AssetID: id, Width: 4, Height: 3, Orientation: 1}
		if f.kind == core.KindVideo {
			m.VideoCodec, m.Duration = "h264", 2000
		}
		meta = append(meta, m)
		thumbs = append(thumbs, store.ThumbResult{AssetID: id})
		if err := cache.Write(id, media.Thumbs{Small: []byte("small"), Large: []byte("large"), Sprite: []byte("sprite")}); err != nil {
			t.Fatal(err)
		}
	}
	// A frame of each video, in the cache as a request would have left it.
	for _, id := range []int64{c.video, c.openVideo} {
		frame := cache.FramePath(id, 1000)
		if err := os.MkdirAll(filepath.Dir(frame), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(frame, []byte("frame"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SaveMetadata(ctx, "v", meta); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveThumbnails(ctx, "v", thumbs); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{c.photo, c.video} {
		if _, err := st.SetBlock(ctx, core.BlockAsset, id, "", core.BlockPhotos); err != nil {
			t.Fatal(err)
		}
	}
	c.stream, err = NewStreamer(st, media.Decoders{}, c.gate)
	if err != nil {
		t.Fatal(err)
	}
	c.stream.decoder = fixedConversion{}
	t.Cleanup(func() { _ = c.stream.Close() })
	c.m = NewMediaService(st, cache, c.stream, c.gate)
	return c
}

// get answers a request to the media service.
func (c *coveredMedia) get(t *testing.T, url string) int {
	t.Helper()
	rec := httptest.NewRecorder()
	c.m.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, url, http.NoBody))
	return rec.Code
}

// stream answers a request to the video server.
func (c *coveredMedia) streamed(t *testing.T, path string) int {
	t.Helper()
	rec := httptest.NewRecorder()
	c.stream.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, c.stream.Base()+path, http.NoBody))
	return rec.Code
}

func TestCoveredMediaIsServedOnEveryRouteOnlyWhileTheGateIsUnlocked(t *testing.T) {
	c := newCoveredMedia(t)
	routes := []struct {
		name, path string
		streamed   bool // served by the video server, not the media service
	}{
		{"photo thumbnail", fmt.Sprintf("/thumb/%d/256?v=1", c.photo), false},
		{"large photo thumbnail", fmt.Sprintf("/thumb/%d/1024?v=1", c.photo), false},
		{"photo original", fmt.Sprintf("/original/%d", c.photo), false},
		{"video thumbnail", fmt.Sprintf("/thumb/%d/256?v=1", c.video), false},
		{"video sprite", fmt.Sprintf("/sprite/%d?v=1", c.video), false},
		{"video frame", fmt.Sprintf("/frame/%d/1000", c.video), false},
		{"video file", fmt.Sprintf("/video/%d", c.video), true},
		{"converted video", fmt.Sprintf("/play/%d?at=0", c.video), true},
	}
	for _, r := range routes {
		t.Run(r.name, func(t *testing.T) {
			status := func() int {
				if r.streamed {
					return c.streamed(t, r.path)
				}
				return c.get(t, r.path)
			}
			if got := status(); got != http.StatusNotFound {
				t.Fatalf("locked: status %d, want 404", got)
			}
			c.gate.open(c.gate.now())
			if got := status(); got != http.StatusOK {
				t.Fatalf("unlocked: status %d, want 200", got)
			}
			c.gate.close()
			if got := status(); got != http.StatusNotFound {
				t.Fatalf("locked again: status %d, want 404 at once", got)
			}
		})
	}
}

func TestUncoveredMediaIsServedWhetherTheGateIsLockedOrNot(t *testing.T) {
	c := newCoveredMedia(t)
	for _, unlocked := range []bool{false, true} {
		if unlocked {
			c.gate.open(c.gate.now())
		}
		for _, url := range []string{fmt.Sprintf("/thumb/%d/256?v=1", c.openPhoto), fmt.Sprintf("/original/%d", c.openPhoto)} {
			if got := c.get(t, url); got != http.StatusOK {
				t.Errorf("%s with unlocked = %v: status %d, want 200", url, unlocked, got)
			}
		}
	}
}

func TestMediaServiceWithoutAGateNeverServesCoveredMedia(t *testing.T) {
	c := newCoveredMedia(t)
	m := NewMediaService(c.st, c.m.cache, nil, nil) // no gate given: it is one that stays locked
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, fmt.Sprintf("/thumb/%d/256?v=1", c.photo), http.NoBody))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", rec.Code)
	}
}

// lockingWriter closes the gate as the first bytes are written, then keeps
// recording what is written after that.
type lockingWriter struct {
	*httptest.ResponseRecorder
	gate *BlockGate
	once sync.Once
}

func (w *lockingWriter) Write(p []byte) (int, error) {
	w.once.Do(w.gate.close)
	return w.ResponseRecorder.Write(p)
}

// bigFile fills the file of asset id with size bytes.
func (c *coveredMedia) bigFile(t *testing.T, id int64, size int) {
	t.Helper()
	f, err := c.st.AssetFile(t.Context(), id, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.Paths[0], make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLockingStopsACoveredPhotoOrVideoAlreadyBeingSent(t *testing.T) {
	const size = 1 << 20
	cases := []struct {
		name string
		id   func(c *coveredMedia) int64
		do   func(c *coveredMedia, w http.ResponseWriter, id int64)
	}{
		{"original", func(c *coveredMedia) int64 { return c.photo }, func(c *coveredMedia, w http.ResponseWriter, id int64) {
			c.m.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, fmt.Sprintf("/original/%d", id), http.NoBody))
		}},
		{"video file", func(c *coveredMedia) int64 { return c.video }, func(c *coveredMedia, w http.ResponseWriter, id int64) {
			c.stream.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, fmt.Sprintf("%s/video/%d", c.stream.Base(), id), http.NoBody))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newCoveredMedia(t)
			id := tc.id(c)
			c.bigFile(t, id, size)
			c.gate.open(c.gate.now())
			w := &lockingWriter{ResponseRecorder: httptest.NewRecorder(), gate: c.gate}
			tc.do(c, w, id)
			if got := w.Body.Len(); got == 0 || got >= size/2 {
				t.Fatalf("%d of %d bytes were sent after the screen locked as the first were; want the response cut at once", got, size)
			}
		})
	}
}

// longConversion writes until a write fails, saying whether its context ended.
type longConversion struct {
	written   atomic.Int64
	cancelled atomic.Bool
}

func (c *longConversion) ConvertMP4(ctx context.Context, _ string, _ int64) (func(io.Writer) error, error) {
	return func(w io.Writer) error {
		chunk := make([]byte, 64<<10)
		for range 100 {
			n, err := w.Write(chunk)
			c.written.Add(int64(n))
			if err != nil {
				c.cancelled.Store(ctx.Err() != nil)
				return err
			}
		}
		return nil
	}, nil
}

func TestLockingStopsAConvertedCoveredVideoAndItsConversion(t *testing.T) {
	c := newCoveredMedia(t)
	conv := &longConversion{}
	c.stream.decoder = conv
	c.gate.open(c.gate.now())
	w := &lockingWriter{ResponseRecorder: httptest.NewRecorder(), gate: c.gate}
	c.stream.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, fmt.Sprintf("%s/play/%d", c.stream.Base(), c.video), http.NoBody))
	if got := conv.written.Load(); got == 0 || got >= 10<<20 {
		t.Fatalf("%d bytes of 100 chunks were converted after the lock; want it stopped at once", got)
	}
	if !conv.cancelled.Load() {
		t.Fatal("the conversion's context was not ended by the lock")
	}
}

func TestLockingDoesNotStopAVideoThatNeedsNoUnlock(t *testing.T) {
	c := newCoveredMedia(t)
	const size = 1 << 20
	c.bigFile(t, c.openVideo, size)
	c.gate.open(c.gate.now())
	w := &lockingWriter{ResponseRecorder: httptest.NewRecorder(), gate: c.gate}
	c.stream.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, fmt.Sprintf("%s/video/%d", c.stream.Base(), c.openVideo), http.NoBody))
	if got := w.Body.Len(); got != size {
		t.Fatalf("%d of %d bytes of an ordinary video were sent, with the screen locking meanwhile", got, size)
	}
}

func TestBrowsingOrdinaryMediaDoesNotKeepTheBlockedScreenUnlocked(t *testing.T) {
	c := newCoveredMedia(t)
	c.gate.open(c.gate.now())
	for range 3 { // 27 minutes of browsing, with the screen's idle timeout at 10
		*c.clock = c.clock.Add(blockUnlockIdle - time.Minute)
		for _, url := range []string{
			fmt.Sprintf("/thumb/%d/256?v=1", c.openPhoto), fmt.Sprintf("/thumb/%d/1024?v=1", c.openPhoto),
			fmt.Sprintf("/original/%d", c.openPhoto), fmt.Sprintf("/sprite/%d?v=1", c.openVideo),
			fmt.Sprintf("/frame/%d/1000", c.openVideo),
		} {
			if got := c.get(t, url); got != http.StatusOK {
				t.Fatalf("%s: status %d", url, got)
			}
		}
		if got := c.streamed(t, fmt.Sprintf("/video/%d", c.openVideo)); got != http.StatusOK {
			t.Fatalf("ordinary video: status %d", got)
		}
		// A photo that is gone is nothing the screen is used for either.
		if got := c.get(t, "/thumb/999/256?v=1"); got != http.StatusNotFound {
			t.Fatalf("unknown photo: status %d", got)
		}
	}
	if c.gate.Unlocked() {
		t.Fatal("ordinary browsing kept the Blocked screen unlocked past its idle timeout")
	}
}

func TestServingCoveredMediaCountsAsUseOfTheBlockedScreen(t *testing.T) {
	c := newCoveredMedia(t)
	c.gate.open(c.gate.now())
	for range 3 {
		*c.clock = c.clock.Add(blockUnlockIdle - time.Minute)
		if got := c.get(t, fmt.Sprintf("/thumb/%d/256?v=1", c.photo)); got != http.StatusOK {
			t.Fatalf("covered thumbnail: status %d, want 200 while it is being looked at", got)
		}
	}
	*c.clock = c.clock.Add(blockUnlockIdle + time.Second)
	if got := c.get(t, fmt.Sprintf("/thumb/%d/256?v=1", c.photo)); got != http.StatusNotFound {
		t.Fatalf("covered thumbnail after the idle timeout: status %d, want 404", got)
	}
}

// stalledConversion produces nothing until its context ends.
type stalledConversion struct {
	started chan struct{}
	ended   chan error
}

func (c stalledConversion) ConvertMP4(ctx context.Context, _ string, _ int64) (func(io.Writer) error, error) {
	return func(io.Writer) error {
		close(c.started)
		select {
		case <-ctx.Done():
			c.ended <- ctx.Err()
		case <-time.After(5 * time.Second):
			c.ended <- nil
		}
		return nil
	}, nil
}

func TestLockingEndsAConversionThatIsNotSendingAnything(t *testing.T) {
	c := newCoveredMedia(t)
	conv := stalledConversion{make(chan struct{}), make(chan error, 1)}
	c.stream.decoder = conv
	c.gate.open(c.gate.now())
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.stream.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet,
			fmt.Sprintf("%s/play/%d", c.stream.Base(), c.video), http.NoBody))
	}()
	awaitStream(t, conv.started)
	c.gate.close()
	awaitStream(t, done)
	if err := <-conv.ended; err == nil {
		t.Fatal("the conversion went on after the screen locked")
	}
}
