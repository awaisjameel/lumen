package services

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"lumen/internal/core"
	"lumen/internal/media"
	"lumen/internal/store"
)

func TestAVideoInABlockedEventAnswers404ToEveryRouteAndPlaysAgainOnceUnblocked(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	src, err := st.AddSource(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	cache := media.NewCache(filepath.Join(dir, "cache"), &fakeDecoder{})
	const takenAt = 1_700_000_000

	// addClip ingests an mp4 the WebView plays as it is, with its pictures cached.
	addClip := func(name string, at int64) int64 {
		t.Helper()
		path := filepath.Join(dir, name+".mp4")
		if err := os.WriteFile(path, []byte("0123456789abcdef"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := st.Ingest(ctx, []store.IngestItem{{SourceID: src.ID, Path: path, Size: 16, MTime: 1,
			Hash: []byte(name), Kind: core.KindVideo, Ext: ".mp4", TakenAt: at, DateFrom: core.DateFromFile}}); err != nil {
			t.Fatal(err)
		}
		jobs, err := st.PendingJobs(ctx, core.StageMetadata, nil, 10)
		if err != nil {
			t.Fatal(err)
		}
		var id int64
		for _, j := range jobs {
			if j.Path == path {
				id = j.AssetID
			}
		}
		if id == 0 {
			t.Fatalf("clip %s was not queued", name)
		}
		if err := st.SaveMetadata(ctx, "v", []store.MetadataResult{{AssetID: id, Width: 4, Height: 3, Orientation: 1,
			TakenAt: at, VideoCodec: "h264", AudioCodec: "aac"}}); err != nil {
			t.Fatal(err)
		}
		if err := cache.Write(id, media.Thumbs{Small: []byte("small"), Large: []byte("large"), Sprite: []byte("sprite")}); err != nil {
			t.Fatal(err)
		}
		if err := st.SaveThumbnails(ctx, "v", []store.ThumbResult{{AssetID: id}}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	first := addClip("first", takenAt)

	stream, err := NewStreamer(st, media.Decoders{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	go stream.Run()
	defer stream.Close()
	m := NewMediaService(st, cache, stream, nil)

	// status is the answer of the picture routes and of the loopback video server.
	status := func(id int64, frame bool) (pictures []int, videos []int) {
		t.Helper()
		urls := []string{fmt.Sprintf("/thumb/%d/256", id), fmt.Sprintf("/thumb/%d/1024", id), fmt.Sprintf("/sprite/%d", id)}
		if frame { // only a blocked clip may be asked: an unblocked one would need FFmpeg
			urls = append(urls, fmt.Sprintf("/frame/%d/1000", id))
		}
		for _, url := range urls {
			rec := httptest.NewRecorder()
			m.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody))
			pictures = append(pictures, rec.Code)
		}
		for _, url := range []string{fmt.Sprintf("%s/video/%d", m.VideoBase(), id), fmt.Sprintf("%s/play/%d", m.VideoBase(), id)} {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Range", "bytes=0-3")
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.Copy(io.Discard, res.Body)
			_ = res.Body.Close()
			videos = append(videos, res.StatusCode)
		}
		return pictures, videos
	}
	if _, videos := status(first, false); videos[0] != http.StatusPartialContent {
		t.Fatalf("an unblocked clip: /video answers %d, want 206", videos[0])
	}

	blockID, err := st.SetBlock(ctx, core.BlockEvent, first, "", core.BlockPhotos)
	if err != nil {
		t.Fatal(err)
	}
	// A second clip dated into the event afterwards is covered as it is saved.
	second := addClip("second", takenAt+60)
	for _, id := range []int64{first, second} {
		pictures, videos := status(id, true)
		for i, code := range append(pictures, videos...) {
			if code != http.StatusNotFound {
				t.Errorf("clip %d, route %d of a blocked event answers %d, want 404", id, i, code)
			}
		}
	}

	if err := st.RemoveBlock(ctx, blockID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{first, second} {
		if pictures, videos := status(id, false); pictures[0] != http.StatusOK || videos[0] != http.StatusPartialContent {
			t.Errorf("clip %d after unblocking: thumbnail %d, video %d; want 200 and 206", id, pictures[0], videos[0])
		}
	}
}
