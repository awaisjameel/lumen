package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/theupdateframework/go-tuf/v2/metadata"
)

// Transport binds all updater requests to fixed HTTPS origins and a deadline.
type Transport struct {
	client  *http.Client
	origins map[string]bool
}

// NewTransport never accepts credentials, insecure TLS or unrestricted redirects.
func NewTransport(origins []string) (*Transport, error) {
	t := &Transport{origins: make(map[string]bool)}
	for _, origin := range origins {
		u, err := url.Parse(origin)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return nil, ErrInvalid
		}
		t.origins[u.Scheme+"://"+strings.ToLower(u.Host)] = true
	}
	t.client = &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return ErrInvalid
		}
		return t.allowed(req.URL)
	}}
	return t, nil
}

func (t *Transport) allowed(u *url.URL) error {
	if u.User != nil || u.Fragment != "" || !t.origins[u.Scheme+"://"+strings.ToLower(u.Host)] {
		return ErrInvalid
	}
	return nil
}

func (t *Transport) get(ctx context.Context, address string) (*http.Response, error) {
	u, err := url.Parse(address)
	if err != nil {
		return nil, err
	}
	if err := t.allowed(u); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "LumenUpdater/1")
	res, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		_ = res.Body.Close()
		return nil, &metadata.ErrDownloadHTTP{StatusCode: res.StatusCode, URL: address}
	}
	return res, nil
}

// scopedFetcher adapts TUF's context-free interface to one request lifetime.
type scopedFetcher struct {
	transport *Transport
	ctx       context.Context
}

func (f scopedFetcher) DownloadFile(address string, limit int64, _ time.Duration) ([]byte, error) {
	if limit < 0 || limit > 5<<20 {
		return nil, ErrInvalid
	}
	res, err := f.transport.get(f.ctx, address)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if int64(len(data)) > limit {
		return nil, ErrInvalid
	}
	return data, err
}

// Download streams a bounded authenticated archive; retries start from zero.
func (t *Transport) Download(ctx context.Context, c Candidate, name string, bandwidthKB int, progress func(int64)) error {
	if c.Length <= 0 || c.Length > maxPayload || !validHash(c.SHA256) {
		return ErrInvalid
	}
	res, err := t.get(ctx, c.URL)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.ContentLength > c.Length {
		return ErrInvalid
	}
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	r := io.LimitReader(res.Body, c.Length+1)
	buffer := make([]byte, 128<<10)
	var size int64
	started := time.Now()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := r.Read(buffer)
		if n > 0 {
			size += int64(n)
			if size > c.Length {
				return ErrInvalid
			}
			if _, err := f.Write(buffer[:n]); err != nil {
				return err
			}
			if _, err := h.Write(buffer[:n]); err != nil {
				return err
			}
			progress(size)
			if bandwidthKB > 0 {
				wait := time.Duration(float64(size)/float64(bandwidthKB*1024)*float64(time.Second)) - time.Since(started)
				if wait > 0 {
					timer := time.NewTimer(wait)
					select {
					case <-ctx.Done():
						timer.Stop()
						return ctx.Err()
					case <-timer.C:
					}
				}
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if size != c.Length || hex.EncodeToString(h.Sum(nil)) != c.SHA256 {
		return fmt.Errorf("runtime archive digest: %w", ErrInvalid)
	}
	if err := f.Sync(); err != nil {
		return err
	}
	return f.Close()
}
