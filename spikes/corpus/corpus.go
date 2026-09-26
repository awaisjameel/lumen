// Package corpus lists the local test images the Phase 0 spikes run on. It
// only reads them; outputs go to spikes/out.
package corpus

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// Folders are the test sets: LUMEN_CORPUS (semicolon-separated) if set,
// else one folder per line in the git-ignored spikes/corpus.local, plus the
// downloaded samples in .cache. Paths stay out of the repo.
var Folders = defaultFolders()

func defaultFolders() []string {
	var dirs []string
	if env := os.Getenv("LUMEN_CORPUS"); env != "" {
		dirs = strings.Split(env, ";")
	} else if b, err := os.ReadFile("corpus.local"); err == nil {
		for _, l := range strings.Split(string(b), "\n") { // TrimSpace drops any \r
			if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
				dirs = append(dirs, l)
			}
		}
	}
	return append(dirs, filepath.Join(".cache", "heic"), filepath.Join(".cache", "camera"))
}

var imageExt = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".heic": true, ".heif": true, ".avif": true, ".gif": true, ".tif": true, ".tiff": true, ".bmp": true}

// Images returns up to n image paths, round-robin across folders so every
// folder is represented, in a stable order.
func Images(n int, exts ...string) []string {
	folders := Folders
	want := imageExt
	if len(exts) > 0 {
		want = map[string]bool{}
		for _, e := range exts {
			want[e] = true
		}
	}
	var lists [][]string
	for _, dir := range folders {
		var l []string
		filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && want[strings.ToLower(filepath.Ext(p))] {
				l = append(l, p)
			}
			return nil
		})
		sort.Strings(l)
		lists = append(lists, l)
	}
	var out []string
	for i := 0; len(out) < n; i++ {
		added := false
		for _, l := range lists {
			if i < len(l) && len(out) < n {
				out = append(out, l[i])
				added = true
			}
		}
		if !added {
			break
		}
	}
	return out
}

// Warm reads every file once so benchmarks measure decoding, not the disk.
func Warm(paths []string) (bytes int64) {
	var mu sync.Mutex
	ch := make(chan string)
	var wg sync.WaitGroup
	for range runtime.NumCPU() {
		wg.Go(func() {
			for p := range ch {
				b, _ := os.ReadFile(p)
				mu.Lock()
				bytes += int64(len(b))
				mu.Unlock()
			}
		})
	}
	for _, p := range paths {
		ch <- p
	}
	close(ch)
	wg.Wait()
	return bytes
}

// Ext is the lower-case extension used to group results.
func Ext(p string) string {
	e := strings.ToLower(filepath.Ext(p))
	if e == ".jpeg" {
		return ".jpg"
	}
	return e
}
