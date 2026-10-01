package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"lumen/internal/core"
	"lumen/internal/media"
	"lumen/internal/store"
)

// Streamer serves videos to the viewer from its own HTTP server on this
// computer's loopback address. The app's origin can't: on Windows, Wails
// hands WebView2 a response only once it is complete, so a video converted
// while it plays would wait for the whole conversion, in memory, and
// seeking a big file would read all of it first. A real HTTP server streams.
//
//	{base}/video/{id}          the file itself, for videos the WebView plays (Range requests)
//	{base}/play/{id}?at={ms}   a video converted to H.264/AAC while it plays
//
// The server listens on 127.0.0.1 only, and every path starts with a random
// secret made at startup, so other programs and web pages can't read the
// user's files through it.
type Streamer struct {
	store   *store.Store
	decoder videoConverter
	srv     *http.Server
	ln      net.Listener
	secret  string
	cancel  context.CancelFunc
	mu      sync.Mutex // guards closing, and adding to active before Close waits
	closing bool
	active  sync.WaitGroup
}

// videoConverter lets stream lifecycle tests hold a conversion until its
// request is cancelled, without depending on a particular codec or speed.
type videoConverter interface {
	ConvertMP4(context.Context, string, int64) (func(io.Writer) error, error)
}

// NewStreamer starts listening on a free loopback port. Serve it with Run.
func NewStreamer(st *store.Store, d media.Decoders) (*Streamer, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, err
	}
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Streamer{store: st, decoder: d, ln: ln, secret: hex.EncodeToString(b[:]), cancel: cancel}
	s.srv = &http.Server{
		Handler:           s,
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          slog.NewLogLogger(slog.Default().Handler(), slog.LevelDebug),
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	return s, nil
}

// Base is the address the paths above follow.
func (s *Streamer) Base() string { return "http://" + s.ln.Addr().String() + "/" + s.secret }

// Run serves until Close.
func (s *Streamer) Run() {
	if err := s.srv.Serve(s.ln); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
		slog.Error("video server stopped", "err", err)
	}
}

// Close stops the server and every stream (and conversion) in progress.
func (s *Streamer) Close() error {
	s.mu.Lock()
	s.closing = true
	s.cancel()
	s.mu.Unlock()
	err := s.srv.Close()
	// Close can precede Run, before the HTTP server owns the listener.
	if lerr := s.ln.Close(); lerr != nil && !errors.Is(lerr, net.ErrClosed) {
		err = errors.Join(err, lerr)
	}
	s.active.Wait() // cancelled requests end their probes and conversions before the store closes
	return err
}

func (s *Streamer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		http.Error(w, "server closed", http.StatusServiceUnavailable)
		return
	}
	s.active.Add(1)
	s.mu.Unlock()
	defer s.active.Done()
	// Only this computer's WebView, addressing the server by its own
	// address (a web page renaming its host to 127.0.0.1 would not).
	if r.Host != s.ln.Addr().String() {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 3 || parts[0] != s.secret {
		http.NotFound(w, r)
		return
	}
	id, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	switch parts[1] {
	case "video":
		s.serveVideo(w, r, id)
	case "play":
		s.servePlay(w, r, id)
	default:
		http.NotFound(w, r)
	}
}

// serveVideo serves a video file the WebView plays as it is.
func (s *Streamer) serveVideo(w http.ResponseWriter, r *http.Request, id int64) {
	f, path, ok := assetFile(r.Context(), s.store, id, w)
	if !ok {
		return
	}
	if f.Kind != core.KindVideo || core.PlaybackOf(f.Ext, f.VideoCodec, f.AudioCodec) == core.PlayConvert {
		http.NotFound(w, r)
		return
	}
	serveFile(w, r, path, core.ContentType(f.Ext))
}

// servePlay converts a video while it streams. Seeking starts a new stream
// at the new point; no converted copy is kept.
func (s *Streamer) servePlay(w http.ResponseWriter, r *http.Request, id int64) {
	at, err := strconv.ParseInt(r.URL.Query().Get("at"), 10, 64)
	if r.URL.Query().Has("at") && (err != nil || at < 0) {
		http.Error(w, "invalid time", http.StatusBadRequest)
		return
	}
	f, path, ok := assetFile(r.Context(), s.store, id, w)
	if !ok {
		return
	}
	if f.Kind != core.KindVideo {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	convert, err := s.decoder.ConvertMP4(ctx, path, at)
	if !served(w, err, id) { // a damaged file fails here, before any header is sent
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodHead {
		return
	}
	if err := convert(conversionWriter{Writer: w, cancel: cancel}); err != nil && ctx.Err() == nil {
		slog.Warn("video conversion stopped", "asset", id, "err", err)
	}
}

// conversionWriter stops FFmpeg if writing the response fails. A subprocess
// must not keep running after its output pipe can no longer be copied.
type conversionWriter struct {
	io.Writer
	cancel context.CancelFunc
}

func (w conversionWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	if err != nil {
		w.cancel()
	}
	return n, err
}
