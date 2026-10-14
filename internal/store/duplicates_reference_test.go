package store

import (
	"context"
	"math/bits"
	"slices"

	"lumen/internal/core"
)

// The previous grouping and bucketing algorithm is an independent oracle for
// preserving connected groups while reducing the number of comparisons.
func referenceGroupDuplicates(ctx context.Context, candidates []duplicateCandidate) ([]core.DuplicateGroup, error) {
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
	if err := referenceLinkNear(ctx, hashes, func(a, b int) {
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

func referenceLinkNear(ctx context.Context, hashes []uint64, fn func(a, b int)) error {
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
