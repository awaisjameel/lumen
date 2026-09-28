package store

import (
	"context"
	"math/bits"
	"math/rand/v2"
	"testing"

	"lumen/internal/core"
)

func TestDuplicateGroupsReviewOnlyVisibleCopiesAndSimilarPhotos(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	a := item(src.ID, `C:\Photos\a.jpg`, "a")
	aCopy := item(src.ID, `C:\Photos\copy.jpg`, "a")
	b := item(src.ID, `C:\Photos\b.jpg`, "b")
	c := item(src.ID, `C:\Photos\c.jpg`, "c")
	video := item(src.ID, `C:\Photos\movie.mp4`, "movie")
	video.Kind = core.KindVideo
	videoCopy := item(src.ID, `C:\Photos\movie-copy.mp4`, "movie")
	videoCopy.Kind = core.KindVideo
	check(t, errOf(st.Ingest(ctx, []IngestItem{a, aCopy, b, c, video, videoCopy})))
	var first, second, third int64
	check(t, st.r.QueryRowContext(ctx, `SELECT id FROM assets WHERE content_hash = ?`, a.Hash).Scan(&first))
	check(t, st.r.QueryRowContext(ctx, `SELECT id FROM assets WHERE content_hash = ?`, b.Hash).Scan(&second))
	check(t, st.r.QueryRowContext(ctx, `SELECT id FROM assets WHERE content_hash = ?`, c.Hash).Scan(&third))
	for _, row := range []struct {
		id     int64
		hash   int64
		width  int
		height int
	}{{first, 0, 1000, 800}, {second, 3, 2000, 1600}, {third, 3, 800, 1000}} {
		_, err := st.w.ExecContext(ctx, `UPDATE assets SET phash = ?, thumb_version = 1, width = ?, height = ? WHERE id = ?`, row.hash, row.width, row.height, row.id)
		check(t, err)
	}
	groups, err := st.DuplicateGroups(ctx)
	check(t, err)
	if len(groups) != 3 || groups[0].Kind != core.DuplicateExact || groups[1].Kind != core.DuplicateExact ||
		groups[2].Kind != core.DuplicateSimilar {
		t.Fatalf("groups = %+v", groups)
	}
	if len(groups[2].Items) != 2 || groups[2].Items[0].ID != first || groups[2].Items[1].ID != second {
		t.Fatalf("similar group = %+v", groups[2])
	}
	if len(groups[0].Items[0].Paths) != 2 || len(groups[1].Items[0].Paths) != 2 {
		t.Fatalf("exact paths = %+v", groups[:2])
	}
	for _, group := range groups {
		for _, it := range group.Items {
			if it.Size != 1 && it.Size != 5 { // one copy's bytes: "a", "b" or "movie"
				t.Fatalf("size of %d = %d, want one copy's size", it.ID, it.Size)
			}
		}
	}
	_, err = st.w.ExecContext(ctx, `UPDATE assets SET hidden = 1 WHERE id = ?`, second)
	check(t, err)
	groups, err = st.DuplicateGroups(ctx)
	check(t, err)
	for _, group := range groups {
		if group.Kind == core.DuplicateSimilar {
			t.Fatalf("hidden asset in similar group: %+v", group)
		}
	}
}

// randomPhotos returns n photos with random hashes, plus a near copy (a few
// bits flipped) of every tenth one, all the same shape.
func randomPhotos(n int) []duplicateCandidate {
	r := rand.New(rand.NewPCG(1, 2))
	photos := make([]duplicateCandidate, 0, n+n/10)
	for i := range n {
		h := r.Uint64()
		photos = append(photos, duplicateCandidate{DuplicateItem: core.DuplicateItem{ID: int64(i), Width: 4, Height: 3}, hash: h, hashed: true})
		if i%10 == 0 {
			for range r.IntN(nearBits + 1) {
				h ^= 1 << r.IntN(64)
			}
			photos = append(photos, duplicateCandidate{DuplicateItem: core.DuplicateItem{ID: int64(n + i), Width: 8, Height: 6}, hash: h, hashed: true})
		}
	}
	return photos
}

func TestLinkNearFindsEveryNearPair(t *testing.T) {
	photos := randomPhotos(2000)
	hashes := make([]uint64, len(photos))
	for i, p := range photos {
		hashes[i] = p.hash
	}
	type pair struct{ a, b int }
	found := map[pair]bool{}
	check(t, linkNear(context.Background(), hashes, func(a, b int) { found[pair{min(a, b), max(a, b)}] = true }))
	near := 0
	for i := range hashes {
		for j := i + 1; j < len(hashes); j++ {
			d := bits.OnesCount64(hashes[i] ^ hashes[j])
			if want := d <= nearBits; want != found[pair{i, j}] {
				t.Fatalf("pair %d,%d at distance %d: found %v", i, j, d, found[pair{i, j}])
			} else if want {
				near++
			}
		}
	}
	if near < 200 {
		t.Fatalf("only %d near pairs; the planted copies should all be found", near)
	}
}

func BenchmarkGroupDuplicates(b *testing.B) {
	photos := randomPhotos(300_000)
	for b.Loop() {
		if _, err := groupDuplicates(context.Background(), photos); err != nil {
			b.Fatal(err)
		}
	}
}
