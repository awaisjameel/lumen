package services

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
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
	m := NewMediaService(st, cache)
	for _, tc := range []struct {
		url, kind string
		status    int
	}{
		{fmt.Sprintf("/thumb/%d/256", id), "image/jpeg", http.StatusOK},
		{fmt.Sprintf("/sprite/%d", id), "image/jpeg", http.StatusOK},
		{fmt.Sprintf("/frame/%d/1000", id), "image/jpeg", http.StatusOK},
		{fmt.Sprintf("/play/%d?at=1000", id), "video/mp4", http.StatusOK},
	} {
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, tc.url, http.NoBody))
		if rec.Code != tc.status || rec.Header().Get("Content-Type") != tc.kind || rec.Body.Len() == 0 {
			t.Errorf("%s: status %d, content type %q, bytes %d", tc.url, rec.Code, rec.Header().Get("Content-Type"), rec.Body.Len())
		}
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("/original/%d", id), http.NoBody)
	req.Header.Set("Range", "bytes=0-15")
	m.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent || rec.Body.Len() != 16 {
		t.Fatalf("direct video Range: status %d, bytes %d", rec.Code, rec.Body.Len())
	}
}
