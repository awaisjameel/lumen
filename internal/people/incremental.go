package people

import (
	"cmp"
	"context"
	"slices"

	"lumen/internal/ai"
	"lumen/internal/store"
)

// clusterState belongs to Run's goroutine. Vectors are retained to rebuild an
// edited person's sum in face-ID order, exactly as the full reference does.
// Append-only placements extend that sum without visiting existing faces.
type clusterState struct {
	cursor   store.ClusterCursor
	faces    map[int64]store.ClusterFace
	loose    map[int64]store.ClusterFace
	people   map[int64]*personSum
	pairs    map[personPair]bool
	rejected map[int64][]int64
	params   Params
}

type personSum struct {
	members     map[int64]struct{}
	sum, mean   []float32
	assets      map[facePlace]bool
	rejects     map[int64]bool
	first, last int64
	user        bool
}

type personPair struct{ a, b int64 }

func newClusterState(p Params) *clusterState {
	return &clusterState{faces: map[int64]store.ClusterFace{}, loose: map[int64]store.ClusterFace{}, people: map[int64]*personSum{}, pairs: map[personPair]bool{}, rejected: map[int64][]int64{}, params: p}
}

func (s *clusterState) update(ctx context.Context, snapshot store.ClusterSnapshot) error {
	dirty := map[int64]bool{}
	rebuild := map[int64]bool{}
	rejectDirty := map[int64]bool{}
	if snapshot.Full {
		s.faces = map[int64]store.ClusterFace{}
		s.loose = map[int64]store.ClusterFace{}
		s.people = map[int64]*personSum{}
		s.pairs = map[personPair]bool{}
	}
	for _, id := range snapshot.Removed {
		if old, ok := s.faces[id]; ok && old.Person != 0 {
			delete(s.people[old.Person].members, id)
			dirty[old.Person], rebuild[old.Person] = true, true
		}
		delete(s.faces, id)
		delete(s.loose, id)
	}
	for i, f := range snapshot.Faces {
		if i%1024 == 0 && ctx.Err() != nil {
			return ctx.Err()
		}
		if old, ok := s.faces[f.ID]; ok {
			if old.Person != 0 {
				delete(s.people[old.Person].members, old.ID)
				dirty[old.Person], rebuild[old.Person] = true, true
			}
			delete(s.loose, old.ID)
		}
		s.faces[f.ID] = f
		if f.Person == 0 {
			s.loose[f.ID] = f
			continue
		}
		ps := s.people[f.Person]
		if ps == nil {
			ps = &personSum{members: map[int64]struct{}{}, sum: make([]float32, len(f.Vec)), assets: map[facePlace]bool{}, rejects: map[int64]bool{}, first: f.ID}
			s.people[f.Person] = ps
		}
		ps.members[f.ID] = struct{}{}
		dirty[f.Person] = true
		if f.ID <= ps.last {
			rebuild[f.Person] = true
		}
		if !rebuild[f.Person] {
			add(ps.sum, f.Vec)
			ps.last = f.ID
			ps.user = ps.user || f.ByUser
			ps.assets[facePlace{f.AssetID, f.Frame}] = true
			for _, person := range s.rejected[f.ID] {
				ps.rejects[person] = true
			}
		}
	}
	rejected := map[int64][]int64{}
	for _, r := range snapshot.Rejections {
		rejected[r.Face] = append(rejected[r.Face], r.Person)
	}
	// Rejections need not change a face row, and may refer to placed faces.
	for id, old := range s.rejected {
		if !slices.Equal(old, rejected[id]) {
			if f := s.faces[id]; f.Person != 0 {
				dirty[f.Person], rejectDirty[f.Person] = true, true
			}
		}
	}
	for id, now := range rejected {
		if !slices.Equal(now, s.rejected[id]) {
			if f := s.faces[id]; f.Person != 0 {
				dirty[f.Person], rejectDirty[f.Person] = true, true
			}
		}
	}
	s.rejected = rejected
	for id := range dirty {
		if err := ctx.Err(); err != nil {
			return err
		}
		ps := s.people[id]
		if len(ps.members) == 0 {
			delete(s.people, id)
			continue
		}
		if rebuild[id] {
			ps.rebuild(s.faces)
		}
		if rebuild[id] || rejectDirty[id] {
			ps.rejects = map[int64]bool{}
			for face := range ps.members {
				for _, person := range rejected[face] {
					ps.rejects[person] = true
				}
			}
		}
		ps.mean = scaled(ps.sum, 1/float32(len(ps.members)))
	}
	for pair := range s.pairs {
		if dirty[pair.a] || dirty[pair.b] {
			delete(s.pairs, pair)
		}
	}
	for id := range dirty {
		a := s.people[id]
		if a == nil {
			continue
		}
		for other, b := range s.people {
			if other == id || (dirty[other] && other < id) {
				continue
			}
			if ai.Dot(a.mean, b.mean) >= s.params.Threshold {
				s.pairs[personPair{min(id, other), max(id, other)}] = true
			}
		}
	}
	s.cursor = snapshot.Cursor
	return ctx.Err()
}

func (p *personSum) rebuild(faces map[int64]store.ClusterFace) {
	ids := make([]int64, 0, len(p.members))
	for id := range p.members {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	p.first, p.last = ids[0], ids[len(ids)-1]
	p.sum = make([]float32, len(faces[p.first].Vec))
	p.assets, p.user = map[facePlace]bool{}, false
	for _, id := range ids {
		f := faces[id]
		add(p.sum, f.Vec)
		p.assets[facePlace{f.AssetID, f.Frame}] = true
		p.user = p.user || f.ByUser
	}
}

func (s *clusterState) looseFaces() []store.ClusterFace {
	out := make([]store.ClusterFace, 0, len(s.loose))
	for _, f := range s.loose {
		out = append(out, f)
	}
	slices.SortFunc(out, func(a, b store.ClusterFace) int { return cmp.Compare(a.ID, b.ID) })
	return out
}

func (s *clusterState) clustering(loose []store.ClusterFace, people []store.ClusterPerson) *clustering {
	c := newClustering(loose, people, nil)
	first := make([]int64, 0, len(c.groups)+len(s.people))
	for _, f := range loose {
		first = append(first, f.ID)
		g := c.groups[c.groupOf[f.ID]]
		for _, id := range s.rejected[f.ID] {
			g.rejects[id] = true
		}
	}
	for id, p := range s.people {
		if p.user {
			c.confirm[id] = true
		}
		c.groups = append(c.groups, &group{sum: p.sum, n: len(p.members), assets: p.assets, extraAssets: map[facePlace]bool{}, rejects: p.rejects, persons: []int64{id}, links: map[int]struct{}{}, shared: true})
		first = append(first, p.first)
	}
	order := make([]int, len(c.groups))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(a, b int) int { return cmp.Compare(first[a], first[b]) })
	groups := make([]*group, len(order))
	for i, old := range order {
		groups[i] = c.groups[old]
	}
	c.groups = groups
	c.groupOf = map[int64]int{}
	for i, g := range groups {
		if len(g.persons) != 0 {
			g.named = c.confirm[g.persons[0]]
		} else {
			c.groupOf[loose[g.faces[0]].ID] = i
		}
	}
	return c
}

func (s *clusterState) cluster(ctx context.Context, loose []store.ClusterFace, people []store.ClusterPerson, links map[int64][]int64) (store.ClusterChanges, error) {
	c := s.clustering(loose, people)
	byPerson := map[int64]int{}
	for i, g := range c.groups {
		if len(g.persons) != 0 {
			byPerson[g.persons[0]] = i
		}
	}
	// Only retained neighbour targets need the face-to-group lookup.
	for _, ids := range links {
		for _, id := range ids {
			if f, ok := s.faces[id]; ok && f.Person != 0 {
				c.groupOf[id] = byPerson[f.Person]
			}
		}
	}
	if err := c.linkCached(ctx, links, s.params, s, byPerson); err != nil {
		return store.ClusterChanges{}, err
	}
	if err := c.merge(ctx, s.params.Threshold); err != nil {
		return store.ClusterChanges{}, err
	}
	return c.changes(s.params), nil
}
