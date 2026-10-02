package services

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"lumen/internal/core"
	"lumen/internal/media"
	"lumen/internal/store"
)

func openStreamTest(t *testing.T) (stream *Streamer, assetID int64) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	src, err := st.AddSource(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Ingest(t.Context(), []store.IngestItem{{SourceID: src.ID, Path: path,
		Hash: []byte("stream"), Kind: core.KindVideo, Ext: ".mp4", TakenAt: 1}}); err != nil {
		t.Fatal(err)
	}
	jobs, err := st.PendingJobs(t.Context(), core.StageMetadata, nil, 1)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("video job: %+v, %v", jobs, err)
	}
	id := jobs[0].AssetID
	if err := st.SaveMetadata(t.Context(), "test", []store.MetadataResult{{AssetID: id, VideoCodec: "h264"}}); err != nil {
		t.Fatal(err)
	}
	s, err := NewStreamer(st, media.Decoders{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, id
}

func awaitStream(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not finish its lifecycle step")
	}
}

func TestStreamCloseBeforeRunClosesItsListener(t *testing.T) {
	s, id := openStreamTest(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, s.Base()+"/video/"+strconv.FormatInt(id, 10), http.NoBody)
	s.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("request after Close = %d, want 503", w.Code)
	}
	conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(t.Context(), "tcp", s.ln.Addr().String())
	if err == nil {
		_ = conn.Close()
		t.Fatal("closed stream still accepts connections")
	}
	s.Run() // A delayed server start must also finish after Close.
}

func TestStreamChecksHostSecretAndDirectRangesWithoutModels(t *testing.T) {
	s, id := openStreamTest(t)
	url := s.Base() + "/video/" + strconv.FormatInt(id, 10)
	for _, tc := range []struct {
		name, host, url, rng string
		status               int
		body                 string
	}{
		{"range", "", url, "bytes=2-5", http.StatusPartialContent, "2345"},
		{"suffix", "", url, "bytes=-3", http.StatusPartialContent, "789"},
		{"past end", "", url, "bytes=10-", http.StatusRequestedRangeNotSatisfiable, ""},
		{"host", "evil.example", url, "", http.StatusForbidden, ""},
		{"secret", "", "http://" + s.ln.Addr().String() + "/wrong/video/1", "", http.StatusNotFound, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.url, http.NoBody)
			if tc.host != "" {
				r.Host = tc.host
			}
			r.Header.Set("Range", tc.rng)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != tc.status || (tc.body != "" && w.Body.String() != tc.body) {
				t.Fatalf("response = %d %q, want %d %q", w.Code, w.Body.String(), tc.status, tc.body)
			}
		})
	}
}

type heldConversion struct{ started, cancelled, release chan struct{} }

func (c heldConversion) ConvertMP4(ctx context.Context, _ string, _ int64) (func(io.Writer) error, error) {
	return func(io.Writer) error {
		close(c.started)
		<-ctx.Done()
		close(c.cancelled)
		<-c.release
		return ctx.Err()
	}, nil
}

func TestStreamCancelsConversionsAndCloseWaitsForThem(t *testing.T) {
	for _, disconnect := range []bool{false, true} {
		t.Run(strconv.FormatBool(disconnect), func(t *testing.T) {
			s, id := openStreamTest(t)
			c := heldConversion{make(chan struct{}), make(chan struct{}), make(chan struct{})}
			var release sync.Once
			t.Cleanup(func() { release.Do(func() { close(c.release) }) })
			s.decoder = c
			runDone := make(chan struct{})
			go func() { s.Run(); close(runDone) }()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			r, err := http.NewRequestWithContext(ctx, http.MethodGet, s.Base()+"/play/"+strconv.FormatInt(id, 10), http.NoBody)
			if err != nil {
				t.Fatal(err)
			}
			requestDone := make(chan struct{})
			go func() {
				defer close(requestDone)
				if res, err := http.DefaultClient.Do(r); err == nil {
					_ = res.Body.Close()
				}
			}()
			awaitStream(t, c.started)
			if disconnect {
				cancel()
				awaitStream(t, c.cancelled)
			}
			closed := make(chan struct{})
			go func() { _ = s.Close(); close(closed) }()
			awaitStream(t, c.cancelled)
			select {
			case <-closed:
				t.Fatal("Close returned while a conversion was still running")
			case <-time.After(20 * time.Millisecond):
			}
			release.Do(func() { close(c.release) })
			awaitStream(t, closed)
			awaitStream(t, runDone)
			awaitStream(t, requestDone)
		})
	}
}

type failedResponse struct{ *httptest.ResponseRecorder }

func (failedResponse) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

type failedOutputConversion struct{ cancelled bool }

func (c *failedOutputConversion) ConvertMP4(ctx context.Context, _ string, _ int64) (func(io.Writer) error, error) {
	return func(w io.Writer) error {
		_, err := w.Write([]byte("frame"))
		c.cancelled = errors.Is(ctx.Err(), context.Canceled)
		return err
	}, nil
}

func TestStreamWriteFailureCancelsTheConversion(t *testing.T) {
	s, id := openStreamTest(t)
	c := &failedOutputConversion{}
	s.decoder = c
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, s.Base()+"/play/"+strconv.FormatInt(id, 10), http.NoBody)
	s.ServeHTTP(failedResponse{httptest.NewRecorder()}, r)
	if !c.cancelled {
		t.Fatal("failed response write left the conversion context live")
	}
}
