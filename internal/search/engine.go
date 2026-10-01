package search

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"lumen/internal/ai"
	"lumen/internal/core"
	"lumen/internal/store"
)

// Search tuning. SigLIP's probabilities are well calibrated for specific
// captions but low for generic ones ("people in an office" peaks near 0.04
// on a folder of office photos), so results are cut relative to the best
// match, with an absolute floor so a query matching nothing returns nothing.
const (
	searchFloor    = 0.0005 // minimum probability for any result
	searchRelative = 100    // keep results whose odds are within this factor of the best one's
	searchLimit    = 1000   // most results a search returns
	maxTagsPerItem = 12     // most AI tags stored per asset
	kindShare      = 0.5    // the best kind applies when its softmax share is above this
	textIdle       = 5 * time.Minute
	textBatch      = 32 // prompts embedded per run of the text model
	queryCache     = 64 // recent query embeddings kept
	// unavailableTTL is how long the set of assets that must not take up
	// result slots (hidden, trashed, gone) is reused. A stale set only
	// costs slots: results are filtered by visibility again when shown.
	unavailableTTL = 10 * time.Second
)

// MaxQueryRunes bounds queries before named matching and AI tokenization.
const MaxQueryRunes = 500

var (
	// ErrNotLoaded means the engine could not load its index or Things;
	// the error it wraps says why.
	ErrNotLoaded = errors.New("search could not start")
	// ErrClosed means the engine was closed (Lumen is shutting down).
	ErrClosed = errors.New("search has stopped")
)

// Engine answers searches and scores Things. It owns the in-memory index,
// loads the text model when a search needs it and frees it when idle.
type Engine struct {
	store *store.Store
	model ai.Model
	index *Index
	video *videoIndex

	started atomic.Bool
	loaded  chan struct{} // closed once Start's first load of the index and labels ended
	stopped chan struct{} // closed once Start's goroutine (loading and retrying) ended
	loadMu  sync.Mutex
	loadErr error // why the last load failed; nil once one succeeded

	textMu   sync.Mutex
	text     *ai.TextEncoder
	textIdle *time.Timer
	textUsed time.Time
	closed   bool                 // Close ran: the text model is never loaded again
	queries  map[string][]float32 // recent query embeddings
	shipped  map[string][]float32 // the built-in Things' prompts, encoded when Lumen was built

	unavailableMu       sync.Mutex
	unavailable         map[int64]bool
	unavailableAt       time.Time
	unavailableRevision int64

	labelsMu sync.RWMutex
	labels   []label
	// labelsGen changes whenever labels do: a batch tagged under an older
	// generation is tagged again before it is saved (see Commit).
	labelsGen atomic.Uint64
	// tagMu orders CreateLabel's re-scoring of the index with Commit, so
	// every image is scored against a new Thing exactly once: by the
	// re-scoring if it was in the index by then, else when it is committed.
	tagMu sync.Mutex
}

type label struct {
	id        int64
	vecs      [][]float32 // one unit vector per prompt
	exclusive bool        // a broad kind of photo (see Label)
	sink      bool        // a hidden kind: absorbs photos, never tags them
	floor     float32     // least probability that counts (see Label)
}

// builtins finds a built-in label's definition by name. Names are unique
// across all tags, so a user's own tag can never be mistaken for one.
var builtins = func() map[string]Label {
	m := make(map[string]Label, len(Vocabulary))
	for _, l := range Vocabulary {
		m[strings.ToLower(l.Name)] = l
	}
	return m
}()

// newLabel builds a label from a stored tag and its prompt vectors.
func newLabel(t store.AutoTag, vecs [][]float32) label {
	l := label{id: t.ID, vecs: vecs, exclusive: t.Exclusive}
	if def, ok := builtins[strings.ToLower(t.Name)]; ok && t.Builtin {
		l.sink, l.floor = def.Sink, def.Floor
	}
	return l
}

// NewEngine prepares an engine for model. Call Start before use.
func NewEngine(st *store.Store, m ai.Model) *Engine {
	return &Engine{store: st, model: m, index: NewIndex(m.Dim), video: newVideoIndex(m.Dim),
		loaded: make(chan struct{}), stopped: make(chan struct{}),
		queries: map[string][]float32{}, shipped: shippedVectors(m)}
}

// Model is the SigLIP model the engine uses.
func (e *Engine) Model() ai.Model { return e.model }

// A load that fails (the database was busy, the text model failed to load)
// is tried again after loadRetry, doubling up to loadRetryMax, until one
// succeeds or Start's ctx ends; meanwhile Wait reports the failure.
const (
	loadRetry    = 30 * time.Second
	loadRetryMax = 10 * time.Minute
)

// Start loads the index and the Things in the background. Cancel ctx before
// Close to stop a load in progress.
func (e *Engine) Start(ctx context.Context) {
	e.started.Store(true)
	go func() {
		defer close(e.stopped)
		first := true
		for retry := loadRetry; ; retry = min(2*retry, loadRetryMax) {
			t := time.Now()
			err := e.load(ctx)
			e.loadMu.Lock()
			e.loadErr = err
			e.loadMu.Unlock()
			if first {
				close(e.loaded)
				first = false
			}
			if err == nil {
				e.labelsMu.RLock()
				things := len(e.labels)
				e.labelsMu.RUnlock()
				slog.Info("search ready", "images", e.index.Len(), "things", things, "took", time.Since(t).Round(time.Millisecond))
				return
			}
			if ctx.Err() != nil {
				return
			}
			slog.Error("search engine failed to load", "err", err, "retry in", retry)
			select {
			case <-ctx.Done():
				return
			case <-time.After(retry):
			}
		}
	}()
}

// Wait blocks until Start has finished its first load. A failed load (the
// latest, while it is retried) is ErrNotLoaded, wrapping why.
func (e *Engine) Wait(ctx context.Context) error {
	done := func() error {
		e.loadMu.Lock()
		err := e.loadErr
		e.loadMu.Unlock()
		if err != nil && !errors.Is(err, context.Canceled) {
			return fmt.Errorf("%w: %w", ErrNotLoaded, err)
		}
		return err
	}
	select {
	case <-e.loaded:
		return done()
	default:
	}
	if !e.started.Load() {
		return fmt.Errorf("%w: not started", ErrNotLoaded) // waiting would never end
	}
	select {
	case <-e.loaded:
		return done()
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close waits for Start's loading to end, then frees the text model for
// good: searches after Close fail with ErrClosed.
func (e *Engine) Close() {
	if e.started.Load() {
		<-e.stopped
	}
	e.textMu.Lock()
	defer e.textMu.Unlock()
	e.closed = true
	e.closeText()
}

func (e *Engine) load(ctx context.Context) error {
	changed, err := e.syncVocabulary(ctx)
	if err != nil {
		return fmt.Errorf("sync things: %w", err)
	}
	if err := e.store.Embeddings(ctx, e.model.Version, e.index.Put); err != nil {
		return fmt.Errorf("load embeddings: %w", err)
	}
	if err := e.video.load(func(add func(asset, at int64, q []int8, scale float32)) error {
		return e.store.VideoEmbeddings(ctx, e.model.Version, add)
	}); err != nil {
		return fmt.Errorf("load video embeddings: %w", err)
	}
	if err := e.loadLabels(ctx, changed); err != nil {
		return err
	}
	if changed {
		// Stamp only after re-tagging succeeds, so an interrupted upgrade retries.
		return e.store.SetInt(ctx, core.SettingVocabulary, VocabularyVersion)
	}
	return nil
}

// syncVocabulary writes the built-in Things into the database when this
// version of Lumen changed them, and reports whether it did.
func (e *Engine) syncVocabulary(ctx context.Context) (bool, error) {
	have, err := e.store.Int(ctx, core.SettingVocabulary, 0)
	if err != nil || have == VocabularyVersion {
		return false, err
	}
	labels := make([]store.VocabLabel, len(Vocabulary))
	for i, l := range Vocabulary {
		labels[i] = store.VocabLabel{Name: l.Name, Prompts: l.Prompts, Exclusive: l.Exclusive, Sink: l.Sink}
	}
	if err := e.store.SyncVocabulary(ctx, labels); err != nil {
		return false, err
	}
	return true, nil
}

// loadLabels reads every AI-applied tag and embeds the prompts of any that
// are new or changed. If the built-in Things changed (vocabChanged, or one
// of them was learned again), the library is re-tagged, since kinds are
// judged against each other; a user's Thing learned again is only scored
// across the library, as CreateLabel does.
func (e *Engine) loadLabels(ctx context.Context, vocabChanged bool) error {
	tags, err := e.store.AutoTags(ctx)
	if err != nil {
		return err
	}
	var stale []store.AutoTag
	var relearned []label // stale user Things, to score across the library
	retagAll := vocabChanged
	labels := make([]label, 0, len(tags))
	for _, t := range tags {
		if t.Model != e.model.Version || len(t.Vecs) != len(t.Prompts)*e.model.Dim {
			stale = append(stale, t)
			continue
		}
		labels = append(labels, newLabel(t, split(t.Vecs, e.model.Dim)))
	}
	// Vectors learned are stored last, once the library is tagged with them:
	// until then those Things stay stale, so a load cut short learns (and
	// tags with) them again.
	learned := make([]label, 0, len(stale))
	if len(stale) > 0 {
		slog.Info("learning things", "count", len(stale))
		for _, t := range stale {
			vecs, err := e.EncodeText(ctx, t.Prompts...)
			if err != nil {
				return fmt.Errorf("thing %q: %w", t.Name, err)
			}
			l := newLabel(t, vecs)
			labels = append(labels, l)
			learned = append(learned, l)
			if t.Builtin || t.Exclusive {
				retagAll = true
			} else {
				relearned = append(relearned, l)
			}
		}
	}
	e.labelsMu.Lock()
	e.labels = labels
	e.labelsGen.Add(1)
	e.labelsMu.Unlock()
	if e.index.Len() > 0 {
		if retagAll {
			if err := e.retag(ctx); err != nil { // re-tag every photo from its stored vector
				return err
			}
		} else {
			for _, l := range relearned {
				if err := e.rescore(ctx, l); err != nil {
					return fmt.Errorf("score a relearned thing: %w", err)
				}
			}
		}
	}
	for _, l := range learned {
		if err := e.store.SetTagVectors(ctx, l.id, e.model.Version, slices.Concat(l.vecs...)); err != nil {
			return err
		}
	}
	return nil
}

// retag recomputes every photo's AI tags from the index. Needed when the
// built-in Things change, since kinds are judged against each other.
func (e *Engine) retag(ctx context.Context) error {
	t := time.Now()
	var sets []store.AssetTagSet
	v := make([]float32, e.model.Dim)
	videos := map[int64][]store.TagScore{}
	e.video.each(func(id int64, q []int8, scale float32) {
		for i, x := range q {
			v[i] = float32(x) * scale
		}
		videos[id] = BestTags(videos[id], e.Tags(v))
	})
	e.index.Each(func(id int64, q []int8, scale float32) {
		if tags, ok := videos[id]; ok {
			sets = append(sets, store.AssetTagSet{AssetID: id, Tags: tags})
			return
		}
		for i, x := range q {
			v[i] = float32(x) * scale
		}
		sets = append(sets, store.AssetTagSet{AssetID: id, Tags: e.Tags(v)})
	})
	if err := e.store.ReplaceAITags(ctx, sets); err != nil {
		return fmt.Errorf("re-tag library: %w", err)
	}
	slog.Info("library re-tagged", "images", len(sets), "took", time.Since(t).Round(time.Millisecond))
	return nil
}

// rescore computes one new (not exclusive) tag for every image in the index.
// Unlike Tags, it does not hold each image to its maxTagsPerItem best: a
// Thing the user asked for is kept wherever it scores, until the image is
// next tagged as a whole.
func (e *Engine) rescore(ctx context.Context, l label) error {
	best := map[int64]float32{}
	for _, v := range l.vecs {
		videos := map[int64]bool{}
		record := func(id int64, cos float32) {
			if p := e.model.Probability(cos); p >= max(core.TagConfidenceMin, l.floor) && p > best[id] {
				best[id] = p
			}
		}
		e.video.scoreAll(v, func(id int64, cos float32) {
			videos[id] = true
			record(id, cos)
		})
		e.index.ScoreAll(v, func(id int64, cos float32) {
			if !videos[id] {
				record(id, cos)
			}
		})
	}
	scores := make([]store.AssetScore, 0, len(best))
	for id, p := range best {
		scores = append(scores, store.AssetScore{AssetID: id, Score: p})
	}
	return e.store.SetTagScores(ctx, l.id, scores)
}

// BestTags combines sampled frames using each tag's strongest score, with
// the same limit and ordering used for photos and subsequent re-tagging.
func BestTags(sets ...[]store.TagScore) []store.TagScore {
	best := map[int64]float32{}
	for _, tags := range sets {
		for _, tag := range tags {
			best[tag.TagID] = max(best[tag.TagID], tag.Score)
		}
	}
	out := make([]store.TagScore, 0, len(best))
	for id, score := range best {
		out = append(out, store.TagScore{TagID: id, Score: score})
	}
	return topTags(out)
}

func topTags(tags []store.TagScore) []store.TagScore {
	slices.SortFunc(tags, func(a, b store.TagScore) int {
		if c := cmp.Compare(b.Score, a.Score); c != 0 {
			return c
		}
		return cmp.Compare(a.TagID, b.TagID)
	})
	return tags[:min(len(tags), maxTagsPerItem)]
}

// LabelsGeneration identifies the current Things: pass it to Commit with
// the batch tagged under them.
func (e *Engine) LabelsGeneration() uint64 { return e.labelsGen.Load() }

// Commit saves a batch of embeddings tagged (by Tags) under labels
// generation gen, with save, then makes it searchable. A batch tagged
// before the Things changed is tagged again first, so a Thing created
// meanwhile misses none of it.
func (e *Engine) Commit(gen uint64, rs []store.EmbedResult, save func([]store.EmbedResult) error) error {
	e.tagMu.Lock()
	defer e.tagMu.Unlock()
	if e.labelsGen.Load() != gen {
		for i := range rs {
			e.retagResult(&rs[i])
		}
	}
	if err := save(rs); err != nil {
		return err
	}
	for _, r := range rs {
		if r.Vec != nil {
			e.index.Put(r.AssetID, r.Vec, r.Scale)
			if r.Frames != nil {
				e.video.replace(r.AssetID, r.Frames)
			}
		}
	}
	return nil
}

// retagResult tags a result again from its stored vectors, as retag does:
// a video from its frames, a photo from its own.
func (e *Engine) retagResult(r *store.EmbedResult) {
	if r.Vec == nil {
		return
	}
	v := make([]float32, e.model.Dim)
	unquantize := func(q []int8, scale float32) []float32 {
		for i, x := range q {
			v[i] = float32(x) * scale
		}
		return v
	}
	if r.Frames == nil {
		r.Tags = e.Tags(unquantize(r.Vec, r.Scale))
		return
	}
	r.Tags = nil
	for _, f := range r.Frames {
		r.Tags = BestTags(r.Tags, e.Tags(unquantize(f.Vec, f.Scale)))
	}
}

// Indexed is how many images are searchable.
func (e *Engine) Indexed() int { return e.index.Len() }

// Tags scores one image (a unit vector) against every Thing and returns the
// surest, from core.TagConfidenceMin (or the Thing's own floor) up. A
// Thing's score is SigLIP's probability for its best prompt; the kind of
// photo that clearly fits best (a softmax share above kindShare among the
// kinds) scores at least that share, so it shows whatever the confidence
// setting. A sink kind can win, but is never a tag.
func (e *Engine) Tags(v []float32) []store.TagScore {
	e.labelsMu.RLock()
	defer e.labelsMu.RUnlock()
	cos := make([]float32, len(e.labels))
	bestKind := -1
	for i, l := range e.labels {
		cos[i] = -1
		for _, p := range l.vecs {
			cos[i] = max(cos[i], ai.Dot(v, p))
		}
		if l.exclusive && (bestKind < 0 || cos[i] > cos[bestKind]) {
			bestKind = i
		}
	}
	var kindWin float32
	if bestKind >= 0 {
		// The winner's share of exp(scale * cos) over all kinds.
		var sum float64
		for i, l := range e.labels {
			if l.exclusive {
				sum += math.Exp(e.model.LogitScale * float64(cos[i]-cos[bestKind]))
			}
		}
		kindWin = float32(1 / sum)
	}
	var out []store.TagScore
	for i, l := range e.labels {
		if l.sink {
			continue
		}
		p := e.model.Probability(cos[i])
		if i == bestKind && kindWin > kindShare {
			p = max(p, kindWin)
		}
		if p >= max(core.TagConfidenceMin, l.floor) {
			out = append(out, store.TagScore{TagID: l.id, Score: p})
		}
	}
	return topTags(out)
}

// CreateLabel makes a new Thing the AI looks for (e.g. "Eiffel Tower") and
// finds it across the library straight away. A Thing of that name the AI
// already looks for is returned as it is; one the user only tags by hand is
// made one the AI looks for too.
func (e *Engine) CreateLabel(ctx context.Context, name string) (int64, error) {
	if err := e.Wait(ctx); err != nil {
		return 0, err
	}
	name = strings.TrimSpace(name)
	prompts := []string{strings.ToLower(name), "a photo of " + strings.ToLower(name)}
	id, err := e.store.CreateTag(ctx, name, core.TagLabel, prompts)
	existed := errors.Is(err, store.ErrTagExists)
	if existed {
		var kind core.TagKind
		if id, kind, err = e.store.TagByName(ctx, name); err != nil || kind != core.TagManual {
			return id, err
		}
		err = e.store.MakeLabel(ctx, id, prompts)
	}
	if err != nil {
		return 0, err
	}
	vecs, err := e.EncodeText(ctx, prompts...)
	if err == nil {
		// Photos the indexer embeds from now on are scored against the new
		// Thing as they arrive; rescore covers those already indexed.
		l := label{id: id, vecs: vecs}
		e.tagMu.Lock()
		e.labelsMu.Lock()
		e.labels = append(e.labels, l)
		e.labelsGen.Add(1)
		e.labelsMu.Unlock()
		err = e.rescore(ctx, l)
		if err == nil {
			// Until scoring commits, leave the Thing stale so a restart
			// scores existing photos again after an interrupted creation.
			err = e.store.SetTagVectors(ctx, id, e.model.Version, slices.Concat(vecs...))
		}
		e.tagMu.Unlock()
	}
	if err != nil {
		// Don't leave a half-made Thing behind, even when ctx ended (the
		// likely cause). A manual tag the user had stays theirs: without
		// vectors, it is learned again when search next starts.
		e.forget(id)
		cleanup := context.WithoutCancel(ctx)
		var cerr error
		if existed {
			cerr = e.store.SetTagVectors(cleanup, id, "", nil)
		} else {
			cerr = e.store.DeleteTag(cleanup, id)
		}
		if cerr != nil {
			slog.Error("undo half-made thing", "tag", id, "err", cerr)
		}
		return 0, err
	}
	return id, nil
}

// ForgetLabel stops scoring a deleted tag. It waits for a load in progress,
// which might otherwise bring the tag back.
func (e *Engine) ForgetLabel(ctx context.Context, id int64) {
	if e.started.Load() {
		select {
		case <-e.loaded:
		case <-ctx.Done():
		}
	}
	e.forget(id)
}

func (e *Engine) forget(id int64) {
	e.labelsMu.Lock()
	defer e.labelsMu.Unlock()
	e.labels = slices.DeleteFunc(e.labels, func(l label) bool { return l.id == id })
	e.labelsGen.Add(1)
}

// Search returns the assets matching a natural-language query, best first.
// With allow, only assets it accepts are searched (a person's photos).
// complete is false when more matched than are returned.
func (e *Engine) Search(ctx context.Context, query string, allow func(id int64) bool) (hits []Hit, complete bool, err error) {
	query = cleanQuery(query)
	if query == "" {
		return nil, true, nil
	}
	if err := e.Wait(ctx); err != nil {
		return nil, false, err
	}
	vecs, err := e.EncodeText(ctx, query)
	if err != nil {
		return nil, false, err
	}
	unavailable, err := e.unavailableAssets(ctx)
	if err != nil {
		return nil, false, err
	}
	eligible := func(id int64) bool {
		return !unavailable[id] && (allow == nil || allow(id))
	}
	floor := e.model.Cosine(searchFloor)
	best := map[int64]Hit{}
	// A video is found by its moments, which say where it matches; its mean
	// vector (kept for photos-only paths) could match where no moment does.
	videos := e.video.assets()
	photos := func(id int64) bool { return !videos[id] && eligible(id) }
	for _, h := range e.index.Search(vecs[0], floor, searchLimit+1, photos) {
		best[h.ID] = h
	}
	for _, h := range e.video.search(vecs[0], floor, searchLimit+1, eligible) {
		if old, ok := best[h.ID]; !ok || h.Score > old.Score {
			best[h.ID] = h
		}
	}
	for _, h := range best {
		hits = append(hits, h)
	}
	slices.SortFunc(hits, byScore)
	hits = hits[:min(len(hits), searchLimit+1)]
	if len(hits) == 0 {
		return nil, true, nil
	}
	cut := hits[0].Score - float32(math.Log(searchRelative)/e.model.LogitScale)
	n := len(hits)
	for n > 0 && hits[n-1].Score < cut {
		n--
	}
	complete = n <= searchLimit
	return hits[:min(n, searchLimit)], complete, nil
}

// cleanQuery collapses a query's spaces and bounds it (BoundQuery).
func cleanQuery(query string) string {
	return BoundQuery(strings.Join(strings.Fields(query), " "))
}

// BoundQuery cuts query to its first MaxQueryRunes runes: a pasted page
// would take the tokenizer (and the name matching before it) long, and the
// model reads only its start.
func BoundQuery(query string) string {
	if n := 0; len(query) > MaxQueryRunes {
		for i := range query {
			if n == MaxQueryRunes {
				return query[:i]
			}
			n++
		}
	}
	return query
}

// unavailableAssets returns the assets that must not take result slots,
// reusing the set for unavailableTTL.
func (e *Engine) unavailableAssets(ctx context.Context) (map[int64]bool, error) {
	e.unavailableMu.Lock()
	defer e.unavailableMu.Unlock()
	revision, err := e.store.VisibilityRevision(ctx)
	if err != nil {
		return nil, err
	}
	if e.unavailable != nil && e.unavailableRevision == revision && time.Since(e.unavailableAt) < unavailableTTL {
		return e.unavailable, nil
	}
	set, err := e.store.UnavailableAssets(ctx)
	if err != nil {
		return nil, err
	}
	e.unavailable, e.unavailableAt = set, time.Now()
	e.unavailableRevision = revision
	return set, nil
}

// EncodeText embeds texts with the text model, loading it if needed: texts
// Lumen ships the vectors of (see prompts.bin) need no model. Single texts
// (search queries) are cached.
func (e *Engine) EncodeText(ctx context.Context, texts ...string) ([][]float32, error) {
	e.textMu.Lock()
	defer e.textMu.Unlock()
	if e.closed {
		return nil, ErrClosed
	}
	e.textUsed = time.Now()
	out := make([][]float32, len(texts))
	var todo []string
	var at []int
	for i, t := range texts {
		if v, ok := e.queries[t]; ok {
			out[i] = v
		} else if v, ok := e.shipped[t]; ok {
			out[i] = v
		} else {
			todo, at = append(todo, t), append(at, i)
		}
	}
	if len(todo) == 0 {
		return out, nil
	}
	if err := e.openText(); err != nil {
		return nil, err
	}
	for lo := 0; lo < len(todo); lo += textBatch {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		hi := min(lo+textBatch, len(todo))
		vecs, err := e.text.Encode(todo[lo:hi])
		if err != nil {
			return nil, err
		}
		for k, v := range vecs {
			out[at[lo+k]] = v
		}
	}
	if len(texts) == 1 { // a search query, likely repeated; prompts are stored instead
		if len(e.queries) >= queryCache {
			clear(e.queries)
		}
		e.queries[texts[0]] = out[0]
	}
	return out, nil
}

// PrepareSearch loads the text model ahead of a search (the user is about to
// type one): loading takes seconds, which the first search would otherwise
// wait for. The model is freed again after textIdle unused.
func (e *Engine) PrepareSearch() error {
	e.textMu.Lock()
	defer e.textMu.Unlock()
	if e.closed {
		return ErrClosed
	}
	e.textUsed = time.Now()
	return e.openText()
}

// openText loads the text model if needed and restarts its idle timer.
// Call with textMu held.
func (e *Engine) openText() error {
	if e.text == nil {
		t := time.Now()
		te, err := ai.OpenTextEncoder(e.model)
		if err != nil {
			return err
		}
		e.text = te
		slog.Info("model loaded", "model", "text model", "took", time.Since(t).Round(time.Millisecond))
	}
	if e.textIdle == nil {
		e.textIdle = time.AfterFunc(textIdle, e.idleText)
	} else {
		e.textIdle.Reset(textIdle)
	}
	return nil
}

// idleText frees the text model once it has been unused for textIdle. The
// timer can fire while a search holds the lock, so it checks again then.
func (e *Engine) idleText() {
	e.textMu.Lock()
	defer e.textMu.Unlock()
	if e.text == nil {
		return
	}
	if left := textIdle - time.Since(e.textUsed); left > 0 {
		e.textIdle.Reset(left)
		return
	}
	e.closeText()
	slog.Info("model unloaded", "model", "text model")
}

func (e *Engine) closeText() {
	if e.textIdle != nil {
		e.textIdle.Stop()
		e.textIdle = nil
	}
	if e.text != nil {
		e.text.Close()
		e.text = nil
	}
}

func split(flat []float32, dim int) [][]float32 {
	out := make([][]float32, 0, len(flat)/dim)
	for i := 0; i+dim <= len(flat); i += dim {
		out = append(out, flat[i:i+dim:i+dim])
	}
	return out
}
