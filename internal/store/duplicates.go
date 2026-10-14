package store

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"math/bits"
	"slices"
	"sync"
	"time"

	"lumen/internal/core"
)

// Photos whose 64-bit perceptual hashes differ in at most nearBits bits are
// suggested as similar.
const nearBits = 6

// Hashes within nearBits of each other differ in at most nearBits of their
// eight bytes, so at least two bytes agree exactly (pigeonhole). Bucketing by
// each of the 28 byte pairs finds every such pair without comparing all photos.
const hashBytes = 8

type duplicateCandidate struct {
	core.DuplicateItem
	copies int
	hash   uint64
	hashed bool // a photo with a thumbnail pHash and known dimensions
}

type duplicateRevision struct {
	events, visible, covers, duplicates int64
}

type duplicateCache struct {
	mu       sync.Mutex // Guards the revision and immutable cached groups.
	revision duplicateRevision
	loaded   bool
	groups   []core.DuplicateGroup
}

// DuplicateGroups lists visible exact file copies and likely similar photos.
// Similarity compares upright thumbnail pHashes; it never modifies files.
func (s *Store) DuplicateGroups(ctx context.Context) ([]core.DuplicateGroup, error) {
	c := &s.duplicatesCache
	c.mu.Lock()
	defer c.mu.Unlock()
	// Read the revision after waiting: a concurrent build may predate this call.
	// Candidates, paths and the revision must all describe the same snapshot.
	var groups []core.DuplicateGroup
	var revision duplicateRevision
	err := s.read(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT value, visible_value, cover_value, duplicate_value
			FROM event_revision WHERE id = 1`).Scan(&revision.events, &revision.visible, &revision.covers, &revision.duplicates); err != nil {
			return err
		}
		if c.loaded && c.revision == revision {
			groups = c.groups
			return nil
		}
		var err error
		groups, err = duplicateGroups(ctx, tx)
		return err
	})
	if err != nil {
		return nil, err
	}
	c.groups, c.revision, c.loaded = groups, revision, true
	return cloneDuplicateGroups(groups), nil
}

func cloneDuplicateGroups(groups []core.DuplicateGroup) []core.DuplicateGroup {
	cloned := slices.Clone(groups)
	for g := range cloned {
		cloned[g].Items = slices.Clone(groups[g].Items)
		for i := range cloned[g].Items {
			cloned[g].Items[i].Paths = slices.Clone(groups[g].Items[i].Paths)
			cloned[g].Items[i].Mtimes = slices.Clone(groups[g].Items[i].Mtimes)
		}
	}
	return cloned
}

func duplicateGroups(ctx context.Context, tx *sql.Tx) ([]core.DuplicateGroup, error) {
	rows, err := tx.QueryContext(ctx, `SELECT a.id, COALESCE(a.phash, 0), COALESCE(a.width, 0), COALESCE(a.height, 0),
		a.thumb_version, a.taken_at, a.ext, a.camera IS NOT NULL, a.date_from = ?, a.lat IS NOT NULL AND a.lon IS NOT NULL, a.added_at,
		a.kind = ? AND a.phash IS NOT NULL AND a.thumb_version > 0 AND a.width > 0 AND a.height > 0 AS hashed,
		count(*) AS copies
		FROM assets a JOIN files f ON f.asset_id = a.id
		WHERE `+visibleA+` GROUP BY a.id HAVING copies > 1 OR hashed`, core.DateFromEXIF, core.KindPhoto)
	candidates, err := collect(rows, err, func(r *sql.Rows) (c duplicateCandidate, err error) {
		var hash int64 // SQLite integers are signed; the bits are what matter
		err = r.Scan(&c.ID, &hash, &c.Width, &c.Height, &c.Thumb, &c.TakenAt, &c.Ext, &c.Camera, &c.ExifDate, &c.GPS, &c.AddedAt,
			&c.hashed, &c.copies)
		c.hash, c.Paths, c.Mtimes = uint64(hash), []string{}, []int64{}
		return c, err
	})
	if err != nil {
		return nil, err
	}
	groups, err := groupDuplicates(ctx, candidates)
	if err != nil {
		return nil, err
	}
	if err := duplicatePaths(ctx, tx, groups); err != nil {
		return nil, err
	}
	markBest(groups) // after the paths: the rule reads each item's files
	return groups, nil
}

// duplicatePaths fills in each grouped asset's file paths, their modified
// times (in Unix seconds, one per path) and file size.
func duplicatePaths(ctx context.Context, tx *sql.Tx, groups []core.DuplicateGroup) error {
	ids := []int64{}
	for _, g := range groups {
		for _, it := range g.Items {
			ids = append(ids, it.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	list, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT asset_id, path, size, mtime FROM files
		WHERE asset_id IN (SELECT value FROM json_each(?)) ORDER BY asset_id, path`, string(list))
	if err != nil {
		return err
	}
	defer rows.Close()
	type copies struct {
		paths  []string
		mtimes []int64
		size   int64
	}
	files := make(map[int64]*copies, len(ids))
	for rows.Next() {
		var id, size, mtime int64
		var path string
		if err := rows.Scan(&id, &path, &size, &mtime); err != nil {
			return err
		}
		c := files[id]
		if c == nil {
			c = &copies{size: size}
			files[id] = c
		}
		c.paths = append(c.paths, path)
		c.mtimes = append(c.mtimes, mtime/int64(time.Second)) // the column holds nanoseconds
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for g := range groups {
		for i := range groups[g].Items {
			it := &groups[g].Items[i]
			if c := files[it.ID]; c != nil {
				it.Paths, it.Mtimes, it.Size = c.paths, c.mtimes, c.size
			}
		}
	}
	return nil
}

func groupDuplicates(ctx context.Context, candidates []duplicateCandidate) ([]core.DuplicateGroup, error) {
	groups := []core.DuplicateGroup{}
	hashed := 0
	for _, c := range candidates {
		if c.copies > 1 {
			groups = append(groups, core.DuplicateGroup{Kind: core.DuplicateExact, Items: []core.DuplicateItem{c.DuplicateItem}})
		}
		if c.hashed {
			hashed++
		}
	}
	// Most libraries have a thumbnail for every candidate. Reuse that slice
	// instead of copying all the metadata merely to filter a few exact groups.
	photos := candidates
	if hashed != len(candidates) {
		photos = make([]duplicateCandidate, 0, hashed)
		for _, c := range candidates {
			if c.hashed {
				photos = append(photos, c)
			}
		}
	}
	parent := make([]int, len(photos))
	for i := range parent {
		parent[i] = i
	}
	find := func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	join := func(a, b int) {
		if a, b = find(a), find(b); a != b {
			parent[b] = a
		}
	}
	if err := linkDuplicatePhotos(ctx, photos, join); err != nil {
		return nil, err
	}
	size := make([]int, len(photos))
	for i := range photos {
		size[find(i)]++
	}
	clusters := make(map[int][]core.DuplicateItem)
	for i, p := range photos {
		if root := find(i); size[root] > 1 {
			clusters[root] = append(clusters[root], p.DuplicateItem)
		}
	}
	for _, items := range clusters {
		slices.SortFunc(items, newestFirst)
		groups = append(groups, core.DuplicateGroup{Kind: core.DuplicateSimilar, Items: items})
	}
	slices.SortFunc(groups, func(a, b core.DuplicateGroup) int {
		if a.Kind != b.Kind {
			if a.Kind == core.DuplicateExact {
				return -1
			}
			return 1
		}
		return newestFirst(a.Items[0], b.Items[0])
	})
	return groups, nil
}

// linkDuplicatePhotos pairs each distinct hash once. Equal hashes retain all
// their shapes, so a wide crop cannot hide a near match to a narrower one.
func linkDuplicatePhotos(ctx context.Context, photos []duplicateCandidate, join func(int, int)) error {
	type shapes struct{ normal, raw []int }
	type heads struct{ normal, raw int }
	hashes := make([]uint64, 0, len(photos))
	byHash := make(map[uint64]int, len(photos))
	lists := make([]heads, 0, len(photos))
	next := make([]int, len(photos))
	for i, p := range photos {
		if i%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		h, ok := byHash[p.hash]
		if !ok {
			h = len(hashes)
			byHash[p.hash] = h
			hashes = append(hashes, p.hash)
			lists = append(lists, heads{-1, -1})
		}
		// A RAW file and its JPEG remain separate suggestions for review.
		if isRaw(p.Ext) {
			next[i], lists[h].raw = lists[h].raw, i
		} else {
			next[i], lists[h].normal = lists[h].normal, i
		}
	}
	// One backing array avoids a separate allocation for every singleton hash.
	ordered := make([]int, 0, len(photos))
	members := make([]shapes, len(hashes))
	for h, list := range lists {
		start := len(ordered)
		for i := list.normal; i >= 0; i = next[i] {
			ordered = append(ordered, i)
		}
		middle := len(ordered)
		for i := list.raw; i >= 0; i = next[i] {
			ordered = append(ordered, i)
		}
		members[h] = shapes{ordered[start:middle:middle], ordered[middle:len(ordered):len(ordered)]}
	}
	ratio := func(a, b int) int {
		return cmp.Compare(int64(photos[a].Width)*int64(photos[b].Height), int64(photos[b].Width)*int64(photos[a].Height))
	}
	for _, m := range members {
		for _, ids := range [][]int{m.normal, m.raw} {
			slices.SortFunc(ids, ratio)
			// Adjacent compatible ratios connect exactly the same components as
			// every compatible pair, even when similarity is not transitive.
			for i := 1; i < len(ids); i++ {
				if sameShape(photos[ids[i-1]].DuplicateItem, photos[ids[i]].DuplicateItem) {
					join(ids[i-1], ids[i])
				}
			}
		}
	}
	linkShapes := func(a, b []int) {
		for x, y := 0, 0; x < len(a) && y < len(b); {
			if sameShape(photos[a[x]].DuplicateItem, photos[b[y]].DuplicateItem) {
				join(a[x], b[y])
			}
			// The smaller ratio cannot match a later ratio if it missed this one.
			if ratio(a[x], b[y]) <= 0 {
				x++
			} else {
				y++
			}
		}
	}
	return linkNear(ctx, hashes, func(a, b int) {
		linkShapes(members[a].normal, members[b].normal)
		linkShapes(members[a].raw, members[b].raw)
	})
}

func isRaw(ext string) bool { return core.FamilyOf(ext) == core.FamilyRAW }

func newestFirst(a, b core.DuplicateItem) int {
	return cmp.Or(cmp.Compare(b.TakenAt, a.TakenAt), cmp.Compare(a.ID, b.ID))
}

// linkNear calls fn for every pair of hashes within nearBits. Each pass
// buckets the hashes by one pair of bytes (a counting sort) and compares only
// hashes within a bucket.
func linkNear(ctx context.Context, hashes []uint64, fn func(a, b int)) error {
	if len(hashes) < 2 {
		return ctx.Err()
	}
	const buckets = 1 << 16
	order := make([]int, len(hashes))
	sorted := make([]uint64, len(hashes)) // hashes in bucket order, read contiguously
	start := make([]int, buckets+1)
	next := make([]int, buckets)
	for p := range hashBytes {
		for q := p + 1; q < hashBytes; q++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			key := func(h uint64) int { return int(uint8(h>>(8*p)))<<8 | int(uint8(h>>(8*q))) }
			clear(start)
			for _, h := range hashes {
				start[key(h)+1]++
			}
			for k := 1; k <= buckets; k++ {
				start[k] += start[k-1]
			}
			copy(next, start)
			for i, h := range hashes {
				k := key(h)
				order[next[k]], sorted[next[k]] = i, h
				next[k]++
			}
			for k := range buckets {
				if k%1024 == 0 {
					if err := ctx.Err(); err != nil {
						return err
					}
				}
				for x := start[k]; x < start[k+1]; x++ {
					for y := x + 1; y < start[k+1]; y++ {
						difference := sorted[x] ^ sorted[y]
						if bits.OnesCount64(difference) <= nearBits && firstHashBytePair(difference, p, q) {
							fn(order[x], order[y])
						}
					}
				}
			}
		}
	}
	return nil
}

// firstHashBytePair avoids repeating a near pair in every matching byte bucket.
func firstHashBytePair(difference uint64, p, q int) bool {
	first, second := -1, -1
	for i := range hashBytes {
		if uint8(difference>>(8*i)) == 0 {
			if first < 0 {
				first = i
			} else {
				second = i
				break
			}
		}
	}
	return first == p && second == q
}

// sameShape compares aspect ratios (without division): hashes alone can match
// unrelated panoramas and crops.
func sameShape(a, b core.DuplicateItem) bool {
	left, right := int64(a.Width)*int64(b.Height), int64(a.Height)*int64(b.Width)
	return left*100 >= right*95 && right*100 >= left*95
}
