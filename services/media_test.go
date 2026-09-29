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
	"testing"

	"lumen/internal/core"
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
		{fmt.Sprintf("/thumb/%d/256", id), "image/jpeg"},
		{fmt.Sprintf("/sprite/%d", id), "image/jpeg"},
		{fmt.Sprintf("/frame/%d/1000", id), "image/jpeg"},
	} {
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, tc.url, http.NoBody))
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != tc.kind || rec.Body.Len() == 0 {
			t.Errorf("%s: status %d, content type %q, bytes %d", tc.url, rec.Code, rec.Header().Get("Content-Type"), rec.Body.Len())
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
}
