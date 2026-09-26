// Package people groups faces into people. The faces stage stores each
// face's identity vector; this package decides who is who, and keeps doing
// so as photos arrive, without ever undoing what the user said.
package people

import (
	"cmp"
	"container/heap"
	"context"
	"runtime"
	"slices"

	"lumen/internal/ai"
	"lumen/internal/parallel"
	"lumen/internal/store"
)

// Params tune clustering.
type Params struct {
	// Threshold is the least average cosine similarity between two groups of
	// faces for them to be one person. Phase 0 found 0.42 kept every group to
	// one person on a hard set (many similar-looking men, same lighting).
	Threshold float32
	// MinFaces is how many clear faces, in as many photos, a new person
	// needs. Fewer stay unplaced until more photos of them arrive.
	MinFaces int
	// A face is clear when sharp enough (ai.Identity.Sharpness) and not
	// turned too far (ai.Face.Yaw). Unclear faces may join someone but never
	// start a new person: blurred, dark and profile faces look alike
	// whoever they are. On real group photos about one face in seven was
	// below sharpness 15, and one in thirty turned past 0.45 (profiles),
	// mostly unrecognisable.
	MinSharpness float32
	MaxYaw       float32
	// Neighbours is how many most similar faces each unplaced face is linked
	// to; merges follow those links.
	Neighbours int
}

// DefaultParams are the tuned settings.
var DefaultParams = Params{Threshold: 0.42, MinFaces: 3, MinSharpness: 15, MaxYaw: 0.45, Neighbours: 12}

// Cluster decides where unplaced faces go, from the faces (all made by one
// model), the people and the user's rejections. It is average-linkage
// agglomerative clustering: the two most similar groups merge, repeatedly,
// while their average similarity is at least p.Threshold. A group is either
// an existing person with their faces, or faces not placed yet.
//
// With unit vectors the average similarity of two groups is exactly the dot
// product of their mean vectors, so a group is just its vector sum and size.
// Groups can only reach the threshold through a pair of faces (or a face and
// a person) that do, so only those links are followed.
//
// What the user said always holds:
//   - faces placed with a person stay there; clustering only places faces
//     no one has placed yet
//   - two faces in one photo are never one person
//   - a face the user took out of a person never goes back to them
//   - two confirmed people (named, or with faces the user placed) are never
//     merged; an unconfirmed person may fold into anyone
//
// in.Links may carry each unplaced face's most similar faces from an earlier
// run (see Linker); without them they are worked out from scratch.
func Cluster(in Input, p Params) store.ClusterChanges {
	if in.Links == nil {
		in.Links = NewLinker(p).Update(in.Faces)
	}
	c := newClustering(in.Faces, in.People, in.Rejections)
	c.link(in.Links, p)
	c.merge(p.Threshold)
	return c.changes(p)
}

// Input is what Cluster works from.
type Input struct {
	Faces      []store.ClusterFace // all made by one model
	People     []store.ClusterPerson
	Rejections []store.Rejection
	Links      map[int64][]int64 // unplaced face -> its most similar faces
}

// group is a person or a set of unplaced faces while clustering.
type group struct {
	sum     []float32
	n       int
	faces   []int            // indexes into clustering.faces, unplaced ones only
	assets  map[int64]bool   // photos the group's faces are in
	persons []int64          // existing people in the group
	rejects map[int64]bool   // people a face in the group was rejected from
	named   bool             // holds a confirmed person
	links   map[int]struct{} // groups it may merge with
	version int              // bumped on every change, to spot stale queue entries
	dead    bool             // merged into another group
}

type clustering struct {
	faces   []store.ClusterFace
	groups  []*group
	groupOf map[int64]int  // face ID -> its group
	confirm map[int64]bool // confirmed people
}

func newClustering(faces []store.ClusterFace, people []store.ClusterPerson, rejections []store.Rejection) *clustering {
	c := &clustering{faces: faces, groupOf: make(map[int64]int, len(faces)), confirm: map[int64]bool{}}
	for _, p := range people {
		if p.Named {
			c.confirm[p.ID] = true
		}
	}
	rejected := map[int64][]int64{} // face -> people
	for _, r := range rejections {
		rejected[r.Face] = append(rejected[r.Face], r.Person)
	}
	byPerson := map[int64]int{} // person -> their group
	for i, f := range faces {
		gi, ok := byPerson[f.Person]
		if f.Person == 0 || !ok {
			gi = len(c.groups)
			c.groups = append(c.groups, &group{sum: make([]float32, len(f.Vec)),
				assets: map[int64]bool{}, rejects: map[int64]bool{}, links: map[int]struct{}{}})
			if f.Person != 0 {
				byPerson[f.Person] = gi
				c.groups[gi].persons = []int64{f.Person}
			}
		}
		g := c.groups[gi]
		c.groupOf[f.ID] = gi
		if f.Person == 0 {
			g.faces = append(g.faces, i)
		} else if f.ByUser {
			c.confirm[f.Person] = true
		}
		add(g.sum, f.Vec)
		g.n++
		g.assets[f.AssetID] = true
		for _, p := range rejected[f.ID] {
			g.rejects[p] = true
		}
	}
	for _, g := range c.groups {
		g.named = slices.ContainsFunc(g.persons, func(p int64) bool { return c.confirm[p] })
	}
	return c
}

// link connects every unplaced face to its most similar faces (links) and
// people, and people to each other.
func (c *clustering) link(links map[int64][]int64, p Params) {
	connect := func(i, j int) {
		if i != j {
			c.groups[i].links[j] = struct{}{}
			c.groups[j].links[i] = struct{}{}
		}
	}
	var loose, placed []int // group indexes
	for i, g := range c.groups {
		if len(g.persons) == 0 {
			loose = append(loose, i)
		} else {
			placed = append(placed, i)
		}
	}
	for _, i := range loose {
		for _, other := range links[c.faces[c.groups[i].faces[0]].ID] {
			if j, ok := c.groupOf[other]; ok {
				connect(i, j)
			}
		}
	}
	// Each loose face against each person's mean: its average similarity to
	// their faces.
	means := make([][]float32, len(c.groups))
	for _, i := range placed {
		means[i] = scaled(c.groups[i].sum, 1/float32(c.groups[i].n))
	}
	near := make([][]scored, len(loose))
	parallel.For(context.Background(), len(loose), runtime.NumCPU(), func(k int) {
		v := c.groups[loose[k]].sum // a single face: its sum is its vector
		for _, j := range placed {
			if s := ai.Dot(v, means[j]); s >= p.Threshold {
				near[k] = keepTop(near[k], scored{j, s}, p.Neighbours, byScore)
			}
		}
	})
	for k, i := range loose {
		for _, b := range near[k] {
			connect(i, b.i)
		}
	}
	for a, i := range placed {
		for _, j := range placed[a+1:] {
			if ai.Dot(means[i], means[j]) >= p.Threshold {
				connect(i, j)
			}
		}
	}
}

// merge joins the most similar linked groups while they reach threshold.
func (c *clustering) merge(threshold float32) {
	q := &queue{}
	push := func(a, b int) {
		if s := c.similarity(a, b); s >= threshold {
			heap.Push(q, pair{a, b, s, c.groups[a].version, c.groups[b].version})
		}
	}
	for i, g := range c.groups {
		for j := range g.links {
			if i < j {
				push(i, j)
			}
		}
	}
	for q.Len() > 0 {
		e := heap.Pop(q).(pair)
		a, b := c.groups[e.a], c.groups[e.b]
		if a.dead || b.dead || a.version != e.va || b.version != e.vb || !c.allowed(a, b) {
			continue // stale: the groups changed since, and were re-queued then
		}
		// Keep the bigger group's maps and fold the smaller one in.
		keep, gone := e.a, e.b
		if len(b.assets) > len(a.assets) {
			keep, gone = gone, keep
		}
		c.join(keep, gone)
		for j := range c.groups[keep].links {
			push(keep, j)
		}
	}
}

func (c *clustering) similarity(a, b int) float32 {
	ga, gb := c.groups[a], c.groups[b]
	return ai.Dot(ga.sum, gb.sum) / float32(ga.n*gb.n)
}

// allowed reports whether two groups may be one person.
func (c *clustering) allowed(a, b *group) bool {
	if a.named && b.named {
		return false
	}
	small, big := a, b
	if len(small.assets) > len(big.assets) {
		small, big = big, small
	}
	for asset := range small.assets {
		if big.assets[asset] {
			return false // two faces in one photo
		}
	}
	for _, p := range a.persons {
		if b.rejects[p] {
			return false
		}
	}
	for _, p := range b.persons {
		if a.rejects[p] {
			return false
		}
	}
	return true
}

// join folds group gone into group keep.
func (c *clustering) join(keep, gone int) {
	k, g := c.groups[keep], c.groups[gone]
	add(k.sum, g.sum)
	k.n += g.n
	k.faces = append(k.faces, g.faces...)
	k.persons = append(k.persons, g.persons...)
	k.named = k.named || g.named
	for a := range g.assets {
		k.assets[a] = true
	}
	for p := range g.rejects {
		k.rejects[p] = true
	}
	for j := range g.links {
		if j != keep {
			k.links[j] = struct{}{}
			other := c.groups[j].links
			delete(other, gone)
			other[keep] = struct{}{}
		}
	}
	delete(k.links, gone)
	k.version++
	g.dead, g.links, g.assets, g.rejects = true, nil, nil, nil
}

// changes turns the final groups into what the store saves.
func (c *clustering) changes(p Params) store.ClusterChanges {
	out := store.ClusterChanges{Assign: map[int64]int64{}}
	for _, g := range c.groups {
		if g.dead {
			continue
		}
		if len(g.persons) == 0 {
			clearIn := map[int64]bool{} // photos with a clear face of them
			for _, i := range g.faces {
				if f := c.faces[i]; f.Sharpness >= p.MinSharpness && f.Yaw <= p.MaxYaw {
					clearIn[f.AssetID] = true
				}
			}
			if len(clearIn) >= p.MinFaces {
				ids := make([]int64, len(g.faces))
				for i, f := range g.faces {
					ids[i] = c.faces[f].ID
				}
				slices.Sort(ids)
				out.NewPeople = append(out.NewPeople, ids)
			}
			continue
		}
		// Everyone in the group becomes one person: the confirmed one if
		// there is one, else the oldest.
		into := slices.MinFunc(g.persons, func(a, b int64) int {
			if c.confirm[a] != c.confirm[b] {
				if c.confirm[a] {
					return -1
				}
				return 1
			}
			return cmp.Compare(a, b)
		})
		for _, person := range g.persons {
			if person != into {
				out.Merges = append(out.Merges, store.Merge{From: person, Into: into})
			}
		}
		for _, f := range g.faces {
			out.Assign[c.faces[f].ID] = into
		}
	}
	slices.SortFunc(out.Merges, func(a, b store.Merge) int { return cmp.Compare(a.From, b.From) })
	return out
}

// scored is a group and its similarity to another.
type scored struct {
	i int
	s float32
}

func byScore(a, b scored) int {
	if c := cmp.Compare(b.s, a.s); c != 0 {
		return c
	}
	return cmp.Compare(a.i, b.i)
}

// keepTop adds x to list, the (at most k) best so far in order.
func keepTop[T any](list []T, x T, k int, order func(a, b T) int) []T {
	if len(list) == k && order(x, list[k-1]) >= 0 {
		return list
	}
	at, _ := slices.BinarySearchFunc(list, x, order)
	list = slices.Insert(list, at, x)
	return list[:min(len(list), k)]
}

// pair is a queued candidate merge, valid while both groups are at the
// recorded versions.
type pair struct {
	a, b   int
	s      float32
	va, vb int
}

// queue is a max-heap of pairs by similarity (ties: lower indexes first, so
// runs are repeatable).
type queue []pair

func (q queue) Len() int { return len(q) }
func (q queue) Less(i, j int) bool {
	if q[i].s != q[j].s {
		return q[i].s > q[j].s
	}
	return q[i].a < q[j].a || (q[i].a == q[j].a && q[i].b < q[j].b)
}
func (q queue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q *queue) Push(x any)   { *q = append(*q, x.(pair)) }
func (q *queue) Pop() any {
	old := *q
	x := old[len(old)-1]
	*q = old[:len(old)-1]
	return x
}

func add(dst, v []float32) {
	for i := range dst {
		dst[i] += v[i]
	}
}

func scaled(v []float32, k float32) []float32 {
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = x * k
	}
	return out
}
