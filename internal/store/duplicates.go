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

// duplicateKey is what grouping needs of a candidate. Most candidates never
// form a group, so their display metadata is read only once groups are known.
type duplicateKey struct {
	id            int64
	width, height int
	ext           string
	hash          uint64
	copies        bool // more than one file holds these bytes
	hashed        bool // a photo with a thumbnail pHash and known dimensions
}

func (k duplicateKey) item() core.DuplicateItem {
	return core.DuplicateItem{ID: k.id, Width: k.width, Height: k.height, Ext: k.ext}
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
	// Most candidates never form a group. Read their display metadata only
	// after grouping, while this transaction still holds the same snapshot.
	rows, err := tx.QueryContext(ctx, `SELECT a.id, COALESCE(a.phash, 0), COALESCE(a.width, 0), COALESCE(a.height, 0), a.ext,
		(count(*) > 1) | CASE WHEN a.kind = ? AND a.phash IS NOT NULL AND a.thumb_version > 0
			AND a.width > 0 AND a.height > 0 THEN 2 ELSE 0 END AS flags
		FROM assets a JOIN files f ON f.asset_id = a.id
		WHERE `+visibleA+` GROUP BY a.id HAVING flags != 0`, core.KindPhoto)
	candidates, err := scanDuplicateCandidates(ctx, rows, err)
	if err != nil {
		return nil, err
	}
	groups, err := groupDuplicates(ctx, candidates, func(i int) core.DuplicateItem { return candidates[i].item() })
	if err != nil {
		return nil, err
	}
	if err := duplicatePaths(ctx, tx, groups); err != nil {
		return nil, err
	}
	sortDuplicateGroups(groups) // after the paths, which also read the dates the order uses
	markBest(groups)            // after the paths: the rule reads each item's files
	return groups, nil
}

// scanDuplicateCandidates reuses the scan destinations and reads SQLite integers
// directly, avoiding decimal conversion for every dimension.
func scanDuplicateCandidates(ctx context.Context, rows *sql.Rows, err error) ([]duplicateKey, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	candidates := []duplicateKey{}
	var c duplicateKey
	var hash, width, height, flags int64
	for rows.Next() {
		if len(candidates)%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if err := rows.Scan(&c.id, &hash, &width, &height, &c.ext, &flags); err != nil {
			return nil, err
		}
		c.hash, c.width, c.height = uint64(hash), int(width), int(height)
		c.copies, c.hashed = flags&1 != 0, flags&2 != 0
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}

// duplicatePaths fills in display metadata and file copies only for grouped
// assets. Modified times are Unix seconds, one per path.
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
	rows, err := tx.QueryContext(ctx, `SELECT f.asset_id, f.path, f.size, f.mtime,
		a.thumb_version, a.taken_at, a.ext, a.camera IS NOT NULL, a.date_from = ?,
		a.lat IS NOT NULL AND a.lon IS NOT NULL, a.added_at
		FROM files f JOIN assets a ON a.id = f.asset_id
		WHERE f.asset_id IN (SELECT value FROM json_each(?)) ORDER BY f.asset_id, f.path`, core.DateFromEXIF, string(list))
	if err != nil {
		return err
	}
	defer rows.Close()
	files := make(map[int64]*core.DuplicateItem, len(ids))
	for rows.Next() {
		var id, size, mtime int64
		var path string
		var item core.DuplicateItem
		if err := rows.Scan(&id, &path, &size, &mtime, &item.Thumb, &item.TakenAt, &item.Ext,
			&item.Camera, &item.ExifDate, &item.GPS, &item.AddedAt); err != nil {
			return err
		}
		c := files[id]
		if c == nil {
			item.Size = size
			c = &item
			files[id] = c
		}
		c.Paths = append(c.Paths, path)
		c.Mtimes = append(c.Mtimes, mtime/int64(time.Second)) // the column holds nanoseconds
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for g := range groups {
		for i := range groups[g].Items {
			it := &groups[g].Items[i]
			if c := files[it.ID]; c != nil {
				it.Paths, it.Mtimes, it.Size = c.Paths, c.Mtimes, c.Size
				it.Thumb, it.TakenAt, it.Ext = c.Thumb, c.TakenAt, c.Ext
				it.Camera, it.ExifDate, it.GPS, it.AddedAt = c.Camera, c.ExifDate, c.GPS, c.AddedAt
			}
		}
	}
	return nil
}

// groupDuplicates groups candidates, unordered; item gives the wire item for
// candidate i.
func groupDuplicates(ctx context.Context, candidates []duplicateKey, item func(int) core.DuplicateItem) ([]core.DuplicateGroup, error) {
	groups := []core.DuplicateGroup{}
	hashed := 0
	for i, c := range candidates {
		if c.copies {
			groups = append(groups, core.DuplicateGroup{Kind: core.DuplicateExact, Items: []core.DuplicateItem{item(i)}})
		}
		if c.hashed {
			hashed++
		}
	}
	photos := candidates
	indices := []int(nil)
	if hashed != len(candidates) {
		photos = make([]duplicateKey, 0, hashed)
		indices = make([]int, 0, hashed)
		for i, c := range candidates {
			if c.hashed {
				photos = append(photos, c)
				indices = append(indices, i)
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
	for i := range photos {
		if root := find(i); size[root] > 1 {
			index := i
			if indices != nil {
				index = indices[i]
			}
			clusters[root] = append(clusters[root], item(index))
		}
	}
	for _, items := range clusters {
		groups = append(groups, core.DuplicateGroup{Kind: core.DuplicateSimilar, Items: items})
	}
	return groups, nil
}

func sortDuplicateGroups(groups []core.DuplicateGroup) {
	for _, g := range groups {
		slices.SortFunc(g.Items, newestFirst)
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
}

// linkDuplicatePhotos pairs each distinct hash once. Equal hashes retain all
// their shapes, so a wide crop cannot hide a near match to a narrower one.
func linkDuplicatePhotos(ctx context.Context, photos []duplicateKey, join func(int, int)) error {
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
		if isRaw(p.ext) {
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
		return cmp.Compare(int64(photos[a].width)*int64(photos[b].height), int64(photos[b].width)*int64(photos[a].height))
	}
	for _, m := range members {
		for _, ids := range [][]int{m.normal, m.raw} {
			slices.SortFunc(ids, ratio)
			// Adjacent compatible ratios connect exactly the same components as
			// every compatible pair, even when similarity is not transitive.
			for i := 1; i < len(ids); i++ {
				if sameShape(photos[ids[i-1]], photos[ids[i]]) {
					join(ids[i-1], ids[i])
				}
			}
		}
	}
	linkShapes := func(a, b []int) {
		for x, y := 0, 0; x < len(a) && y < len(b); {
			if sameShape(photos[a[x]], photos[b[y]]) {
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
func sameShape(a, b duplicateKey) bool {
	left, right := int64(a.width)*int64(b.height), int64(a.height)*int64(b.width)
	return left*100 >= right*95 && right*100 >= left*95
}
