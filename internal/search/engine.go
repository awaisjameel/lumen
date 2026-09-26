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
	searchRelative = 100    // keep results within this factor of the best one
	searchLimit    = 1000   // most results a search returns
	maxTagsPerItem = 12     // most AI tags stored per asset
	kindShare      = 0.5    // the best kind applies when its softmax share is above this
	textIdle       = 5 * time.Minute
)

// Engine answers searches and scores Things. It owns the in-memory index,
// loads the text model when a search needs it and frees it when idle.
type Engine struct {
	store *store.Store
	model ai.Model
	index *Index

	started atomic.Bool
	loaded  chan struct{} // closed once Start has loaded the index and labels
	loadErr error

	textMu   sync.Mutex
	text     *ai.TextEncoder
	textIdle *time.Timer
	queries  map[string][]float32 // recent query embeddings

	labelsMu sync.RWMutex
	labels   []label
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
	return &Engine{store: st, model: m, index: NewIndex(m.Dim), loaded: make(chan struct{}),
		queries: map[string][]float32{}}
}

// Model is the SigLIP model the engine uses.
func (e *Engine) Model() ai.Model { return e.model }

// Start loads the index and the Things in the background. Cancel ctx before
// Close to stop a load in progress.
func (e *Engine) Start(ctx context.Context) {
	e.started.Store(true)
	go func() {
		defer close(e.loaded)
		t := time.Now()
		if e.loadErr = e.load(ctx); e.loadErr != nil {
			if ctx.Err() == nil {
				slog.Error("search engine failed to load", "err", e.loadErr)
			}
			return
		}
		slog.Info("search ready", "images", e.index.Len(), "things", len(e.labels), "took", time.Since(t).Round(time.Millisecond))
	}()
}

// Wait blocks until Start has finished loading.
func (e *Engine) Wait(ctx context.Context) error {
	select {
	case <-e.loaded:
		return e.loadErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close waits for a load in progress to end, then frees the text model.
func (e *Engine) Close() {
	if e.started.Load() {
		<-e.loaded
	}
	e.textMu.Lock()
	defer e.textMu.Unlock()
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
	return e.loadLabels(ctx, changed)
}

// syncVocabulary writes the built-in Things into the database when this
// version of Lumen changed them, and reports whether it did.
func (e *Engine) syncVocabulary(ctx context.Context) (bool, error) {
	have, err := e.store.Float(ctx, core.SettingVocabulary, 0)
	if err != nil || int(have) == VocabularyVersion {
		return false, err
	}
	labels := make([]store.VocabLabel, len(Vocabulary))
	for i, l := range Vocabulary {
		labels[i] = store.VocabLabel{Name: l.Name, Prompts: l.Prompts, Exclusive: l.Exclusive, Hidden: l.Sink}
	}
	if err := e.store.SyncVocabulary(ctx, labels); err != nil {
		return false, err
	}
	return true, e.store.SetFloat(ctx, core.SettingVocabulary, VocabularyVersion)
}

// loadLabels reads every AI-applied tag and embeds the prompts of any that
// are new or changed. If the Things changed (vocabChanged, or new prompts),
// the library is re-tagged.
func (e *Engine) loadLabels(ctx context.Context, vocabChanged bool) error {
	tags, err := e.store.AutoTags(ctx)
	if err != nil {
		return err
	}
	var stale []store.AutoTag
	labels := make([]label, 0, len(tags))
	for _, t := range tags {
		if t.Model != e.model.Version || len(t.Vecs) != len(t.Prompts)*e.model.Dim {
			stale = append(stale, t)
			continue
		}
		labels = append(labels, newLabel(t, split(t.Vecs, e.model.Dim)))
	}
	if len(stale) > 0 {
		slog.Info("learning things", "count", len(stale))
		for _, t := range stale {
			vecs, err := e.embedPrompts(ctx, t.ID, t.Prompts)
			if err != nil {
				return fmt.Errorf("thing %q: %w", t.Name, err)
			}
			labels = append(labels, newLabel(t, vecs))
		}
	}
	e.labelsMu.Lock()
	e.labels = labels
	e.labelsMu.Unlock()
	if (vocabChanged || len(stale) > 0) && e.index.Len() > 0 {
		return e.retag(ctx) // re-tag every photo from its stored vector
	}
	return nil
}

// embedPrompts embeds a tag's prompts and stores them.
func (e *Engine) embedPrompts(ctx context.Context, id int64, prompts []string) ([][]float32, error) {
	vecs, err := e.EncodeText(ctx, prompts...)
	if err != nil {
		return nil, err
	}
	if err := e.store.SetTagVectors(ctx, id, e.model.Version, slices.Concat(vecs...)); err != nil {
		return nil, err
	}
	return vecs, nil
}

// retag recomputes every photo's AI tags from the index. Needed when the
// built-in Things change, since kinds are judged against each other.
func (e *Engine) retag(ctx context.Context) error {
	t := time.Now()
	var sets []store.AssetTagSet
	v := make([]float32, e.model.Dim)
	e.index.Each(func(id int64, q []int8, scale float32) {
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
func (e *Engine) rescore(ctx context.Context, l label) error {
	best := map[int64]float32{}
	for _, v := range l.vecs {
		e.index.ScoreAll(v, func(id int64, cos float32) {
			if p := e.model.Probability(cos); p >= max(core.TagConfidenceMin, l.floor) && p > best[id] {
				best[id] = p
			}
		})
	}
	scores := make([]store.AssetScore, 0, len(best))
	for id, p := range best {
		scores = append(scores, store.AssetScore{AssetID: id, Score: p})
	}
	return e.store.SetTagScores(ctx, l.id, scores)
}

// Add puts a new image embedding into the index (after it is saved).
func (e *Engine) Add(id int64, q []int8, scale float32) { e.index.Put(id, q, scale) }

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
			cos[i] = max(cos[i], dot(v, p))
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
	slices.SortFunc(out, func(a, b store.TagScore) int { return cmp.Compare(b.Score, a.Score) })
	return out[:min(len(out), maxTagsPerItem)]
}

// CreateLabel makes a new Thing the AI looks for (e.g. "Eiffel Tower") and
// finds it across the library straight away.
func (e *Engine) CreateLabel(ctx context.Context, name string) (int64, error) {
	if err := e.Wait(ctx); err != nil {
		return 0, err
	}
	name = strings.TrimSpace(name)
	prompts := []string{strings.ToLower(name), "a photo of " + strings.ToLower(name)}
	id, err := e.store.CreateTag(ctx, name, core.TagLabel, prompts)
	if err != nil {
		return 0, err
	}
	vecs, err := e.embedPrompts(ctx, id, prompts)
	l := label{id: id, vecs: vecs}
	if err == nil {
		err = e.rescore(ctx, l)
	}
	if err != nil {
		_ = e.store.DeleteTag(ctx, id) // don't leave a half-made Thing behind
		return 0, err
	}
	e.labelsMu.Lock()
	e.labels = append(e.labels, l)
	e.labelsMu.Unlock()
	return id, nil
}

// ForgetLabel stops scoring a deleted tag.
func (e *Engine) ForgetLabel(id int64) {
	e.labelsMu.Lock()
	defer e.labelsMu.Unlock()
	e.labels = slices.DeleteFunc(e.labels, func(l label) bool { return l.id == id })
}

// Search returns the assets matching a natural-language query, best first.
// complete is false when more matched than are returned.
func (e *Engine) Search(ctx context.Context, query string) (hits []Hit, complete bool, err error) {
	query = strings.Join(strings.Fields(query), " ")
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
	hits = e.index.Search(vecs[0], e.model.Cosine(searchFloor), searchLimit+1)
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

// EncodeText embeds texts with the text model, loading it if needed.
// Single texts (search queries) are cached.
func (e *Engine) EncodeText(ctx context.Context, texts ...string) ([][]float32, error) {
	e.textMu.Lock()
	defer e.textMu.Unlock()
	out := make([][]float32, len(texts))
	var todo []string
	var at []int
	for i, t := range texts {
		if v, ok := e.queries[t]; ok {
			out[i] = v
		} else {
			todo, at = append(todo, t), append(at, i)
		}
	}
	if len(todo) == 0 {
		return out, nil
	}
	if e.text == nil {
		t := time.Now()
		te, err := ai.OpenTextEncoder(e.model)
		if err != nil {
			return nil, err
		}
		e.text = te
		slog.Info("text model loaded", "took", time.Since(t).Round(time.Millisecond))
	}
	if e.textIdle == nil {
		e.textIdle = time.AfterFunc(textIdle, e.idleText)
	} else {
		e.textIdle.Reset(textIdle)
	}
	const batch = 32
	for lo := 0; lo < len(todo); lo += batch {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		hi := min(lo+batch, len(todo))
		vecs, err := e.text.Encode(todo[lo:hi])
		if err != nil {
			return nil, err
		}
		for k, v := range vecs {
			out[at[lo+k]] = v
		}
	}
	if len(texts) == 1 { // a search query, likely repeated; prompts are stored instead
		if len(e.queries) >= 64 {
			clear(e.queries)
		}
		e.queries[texts[0]] = out[0]
	}
	return out, nil
}

func (e *Engine) idleText() {
	e.textMu.Lock()
	defer e.textMu.Unlock()
	e.closeText()
	slog.Info("text model unloaded (idle)")
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

// ErrNoModel means the SigLIP model files are missing, so search and Things
// are unavailable.
var ErrNoModel = errors.New("the AI model is not installed")

func split(flat []float32, dim int) [][]float32 {
	out := make([][]float32, 0, len(flat)/dim)
	for i := 0; i+dim <= len(flat); i += dim {
		out = append(out, flat[i:i+dim:i+dim])
	}
	return out
}

func dot(a, b []float32) float32 {
	var s float32
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}
