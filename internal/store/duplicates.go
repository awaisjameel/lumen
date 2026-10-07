package store

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"math/bits"
	"slices"
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

// DuplicateGroups lists visible exact file copies and likely similar photos.
// Similarity compares upright thumbnail pHashes; it never modifies files.
func (s *Store) DuplicateGroups(ctx context.Context) ([]core.DuplicateGroup, error) {
	// Candidates and their paths must describe one snapshot while indexing continues.
	var groups []core.DuplicateGroup
	err := s.read(ctx, func(tx *sql.Tx) error {
		var err error
		groups, err = duplicateGroups(ctx, tx)
		return err
	})
	return groups, err
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
	photos := make([]duplicateCandidate, 0, len(candidates))
	for _, c := range candidates {
		if c.copies > 1 {
			groups = append(groups, core.DuplicateGroup{Kind: core.DuplicateExact, Items: []core.DuplicateItem{c.DuplicateItem}})
		}
		if c.hashed {
			photos = append(photos, c)
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
	hashes := make([]uint64, len(photos))
	for i, p := range photos {
		hashes[i] = p.hash
	}
	if err := linkNear(ctx, hashes, func(a, b int) {
		// A camera's RAW file and the JPEG it saved beside it are a negative
		// and its print, not two copies: they are never offered as similar,
		// which would suggest removing one. RAW files still group together.
		if sameShape(photos[a].DuplicateItem, photos[b].DuplicateItem) && isRaw(photos[a].Ext) == isRaw(photos[b].Ext) {
			join(a, b)
		}
	}); err != nil {
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

func isRaw(ext string) bool { return core.FamilyOf(ext) == core.FamilyRAW }

func newestFirst(a, b core.DuplicateItem) int {
	return cmp.Or(cmp.Compare(b.TakenAt, a.TakenAt), cmp.Compare(a.ID, b.ID))
}

// linkNear calls fn for every pair of hashes within nearBits. Each pass
// buckets the hashes by one pair of bytes (a counting sort) and compares only
// hashes within a bucket.
func linkNear(ctx context.Context, hashes []uint64, fn func(a, b int)) error {
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
				for x := start[k]; x < start[k+1]; x++ {
					for y := x + 1; y < start[k+1]; y++ {
						if bits.OnesCount64(sorted[x]^sorted[y]) <= nearBits {
							fn(order[x], order[y])
						}
					}
				}
			}
		}
	}
	return nil
}

// sameShape compares aspect ratios (without division): hashes alone can match
// unrelated panoramas and crops.
func sameShape(a, b core.DuplicateItem) bool {
	left, right := int64(a.Width)*int64(b.Height), int64(a.Height)*int64(b.Width)
	return left*100 >= right*95 && right*100 >= left*95
}
