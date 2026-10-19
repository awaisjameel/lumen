package services

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"slices"

	"lumen/internal/core"
	"lumen/internal/index"
	"lumen/internal/people"
	"lumen/internal/search"
	"lumen/internal/store"
)

// ErrNoAI means the on-device AI isn't available, so search and Things can't
// work (the progress event's ai.error says why).
var ErrNoAI = errors.New("the on-device AI isn't available")

// suggestions is how many matches the search box suggests of each kind.
const suggestions = 6

// How many photos whose text matches a search it returns: the same cap as
// the semantic results, so merging them keeps the total bounded.
const textHitLimit = 1000

// semanticSearch is what Search needs of the search engine, which tests
// replace with a fake.
type semanticSearch interface {
	Search(ctx context.Context, query string, allow func(id int64) bool) (hits []search.Hit, complete bool, err error)
}

// SearchService is natural-language search and Things (tags).
type SearchService struct {
	store    *store.Store
	engine   *search.Engine // nil when the AI can't run
	semantic semanticSearch // engine, but nil (not a nil engine in an interface) when the AI can't run
	faces    bool           // faces are being recognised (the people engine runs)
	ix       *index.Indexer // tells the UI what changed; nil in tests
}

// NewSearchService returns the search service (engine is nil without the AI;
// faces says whether faces are recognised).
func NewSearchService(st *store.Store, engine *search.Engine, faces bool, ix *index.Indexer) *SearchService {
	s := &SearchService{store: st, engine: engine, faces: faces, ix: ix}
	if engine != nil {
		s.semantic = engine
	}
	return s
}

// Search finds photos matching a description, best match first. Named
// people in it narrow the search to photos of all of them ("Ali at the
// beach"); names alone give their photos, newest first. Photos whose text
// (read from screenshots and documents, when the user has that on) holds
// every word of the description come first, newest first, then the photos
// the description matches by meaning. Without the AI, the description can't
// be searched by meaning (the result says so): named people's photos and
// text are still found.
func (s *SearchService) Search(ctx context.Context, query string) (core.SearchResult, error) {
	query = search.BoundQuery(query) // before the names are matched too
	named, err := s.store.SearchNames(ctx, query)
	if err != nil {
		return core.SearchResult{}, err
	}
	matches := people.MatchNames(query, named)
	if slices.ContainsFunc(matches, func(match people.NameMatch) bool { return match.Target.Blocked }) {
		return core.SearchResult{Items: []core.GridItem{}, People: []core.SearchPerson{}, Complete: true, Blocked: true}, nil
	}
	personMatches := slices.DeleteFunc(matches, func(match people.NameMatch) bool { return match.Target.Kind != core.BlockPerson })
	var who []int64
	spelled := []core.SearchPerson{}
	for _, match := range personMatches {
		if !slices.Contains(who, match.Target.ID) {
			who = append(who, match.Target.ID)
			spelled = append(spelled, core.SearchPerson{ID: match.Target.ID, Name: query[match.Start:match.End]})
		}
	}
	text := people.RemainingQuery(query, personMatches)
	var ids []int64 // the named people's photos, if any are named; then the result
	var moments map[int64]int64
	var waitFor []core.Stage // stages whose photos can't be found yet
	complete, noAI := true, false
	var theirs map[int64]bool // the named people's photos; nil if none are named
	if len(who) > 0 {
		if ids, err = s.store.PeopleAssets(ctx, who); err != nil {
			return core.SearchResult{}, err
		}
		if s.faces {
			waitFor = append(waitFor, core.StageFaces)
		}
		theirs = make(map[int64]bool, len(ids))
		for _, id := range ids {
			theirs[id] = true
		}
	}
	// With text left to search for, unless no photo has all the people named:
	// first the photos whose own text has every word, then those it matches
	// by meaning.
	if text != "" && (len(who) == 0 || len(ids) > 0) {
		withText, err := s.textHits(ctx, text, theirs)
		if err != nil {
			return core.SearchResult{}, err
		}
		complete = len(withText) < textHitLimit
		reading, err := s.store.Bool(ctx, core.SettingOCR, false)
		if err != nil {
			return core.SearchResult{}, err
		}
		if reading {
			waitFor = append(waitFor, core.StageOCR)
		}
		var meaning []search.Hit
		described := false // the description was searched by meaning
		if s.semantic == nil {
			noAI = true
		} else {
			waitFor = append(waitFor, core.StageEmbedding)
			var allow func(int64) bool
			if theirs != nil {
				allow = func(id int64) bool { return theirs[id] }
			}
			hits, all, err := s.semantic.Search(ctx, text, allow)
			switch {
			case errors.Is(err, search.ErrNotLoaded):
				noAI = true
			case err != nil:
				return core.SearchResult{}, err
			default:
				meaning, described = hits, true
				complete = complete && all
			}
		}
		switch {
		case described:
			ids, moments = mergeHits(withText, meaning)
		case len(who) > 0:
			ids = appendUnseen(withText, ids) // the description can't narrow their photos, but its text does
		default:
			ids = withText
		}
	}
	items, err := s.store.GridItems(ctx, ids)
	if err != nil {
		return core.SearchResult{}, err
	}
	for i := range items {
		items[i].At = moments[items[i].ID]
	}
	pending, err := s.store.CountPending(ctx, waitFor...)
	if err != nil {
		return core.SearchResult{}, err
	}
	return core.SearchResult{Items: items, People: spelled, Description: text, Pending: pending, Complete: complete, NoAI: noAI}, nil
}

// RecentSearches lists the searches the user ran last, newest first. One that
// names something blocked is left out: the list must not say what the
// Blocked screen hides.
func (s *SearchService) RecentSearches(ctx context.Context) ([]string, error) {
	list, err := s.store.RecentSearches(ctx)
	if err != nil {
		return nil, err
	}
	return s.shownSearches(ctx, list)
}

// RememberSearch keeps query as the most recent search the user ran and
// returns the recent searches as RecentSearches does.
func (s *SearchService) RememberSearch(ctx context.Context, query string) ([]string, error) {
	list, err := s.store.RememberSearch(ctx, search.BoundQuery(query))
	if err != nil {
		return nil, err
	}
	return s.shownSearches(ctx, list)
}

// ClearRecentSearches forgets the recent searches.
func (s *SearchService) ClearRecentSearches(ctx context.Context) error {
	return s.store.ClearRecentSearches(ctx)
}

// shownSearches is list without the searches that name something blocked.
func (s *SearchService) shownSearches(ctx context.Context, list []string) ([]string, error) {
	shown := []string{}
	for _, query := range list {
		named, err := s.store.SearchNames(ctx, query)
		if err != nil {
			return nil, err
		}
		if !slices.ContainsFunc(people.MatchNames(query, named), func(match people.NameMatch) bool { return match.Target.Blocked }) {
			shown = append(shown, query)
		}
	}
	return shown, nil
}

// Hits gives current search results again as they are now, in the order
// given, without running the search again: hits no longer visible are left
// out, and the others carry their current thumbnail and favourite.
func (s *SearchService) Hits(ctx context.Context, ids []int64) ([]core.GridItem, error) {
	items, err := s.store.GridItems(ctx, ids)
	if items == nil {
		items = []core.GridItem{}
	}
	return items, err
}

// PrepareSearch readies the AI for a search the user is starting to type
// (the search box got focus). It does nothing without the AI.
func (s *SearchService) PrepareSearch(ctx context.Context) error {
	if s.engine == nil {
		return nil
	}
	if err := s.engine.Wait(ctx); err != nil {
		return nil //nolint:nilerr // the search itself reports why the AI can't run
	}
	return s.engine.PrepareSearch()
}

// SuggestTags returns Things whose names contain text, for the search box
// (their id, name and kind; no counts).
func (s *SearchService) SuggestTags(ctx context.Context, text string) ([]core.Tag, error) {
	return s.store.SearchTags(ctx, text, suggestions)
}

// Things lists every tag with its count and cover, most used first.
func (s *SearchService) Things(ctx context.Context) ([]core.Tag, error) {
	minScore, err := tagConfidence(ctx, s.store)
	if err != nil {
		return nil, err
	}
	return s.store.Things(ctx, minScore)
}

// CreateLabel adds a Thing for the AI to look for, and finds it across the
// library straight away. It returns the tag's id; if a Thing by that name
// already exists, that one's id (the caller shows it either way).
func (s *SearchService) CreateLabel(ctx context.Context, name string) (int64, error) {
	if s.engine == nil {
		return 0, ErrNoAI
	}
	return s.engine.CreateLabel(ctx, name)
}

// DeleteTag removes a tag the user made. Photos are untouched.
func (s *SearchService) DeleteTag(ctx context.Context, id int64) error {
	if err := s.store.DeleteTag(ctx, id); err != nil {
		return err
	}
	if s.engine != nil {
		s.engine.ForgetLabel(ctx, id)
	}
	return nil
}

// AssetTags lists the tags on one photo.
func (s *SearchService) AssetTags(ctx context.Context, assetID int64) ([]core.AssetTag, error) {
	minScore, err := tagConfidence(ctx, s.store)
	if err != nil {
		return nil, err
	}
	return s.store.AssetTags(ctx, assetID, minScore)
}

// TagAsset puts a tag on a photo by name, creating the tag if it's new.
func (s *SearchService) TagAsset(ctx context.Context, assetID int64, name string) error {
	id, err := s.tagNamed(ctx, name)
	if err != nil {
		return err
	}
	if err := s.store.TagAsset(ctx, assetID, id); err != nil {
		return err
	}
	return s.queueOCR(ctx)
}

// TagAssets is TagAsset for a selection: the tag is found or made once, and
// each photo has its own outcome (see LibraryService.ApplyBatch).
func (s *SearchService) TagAssets(ctx context.Context, ids []int64, name string) ([]core.BatchOutcome, error) {
	id, err := s.tagNamed(ctx, name)
	if err != nil {
		return nil, err
	}
	results, err := s.store.TagAssets(ctx, ids, id)
	if err != nil {
		return nil, err
	}
	// Tagging has committed. A background queue failure must not discard its
	// per-photo outcomes or tell the UI that those edits failed.
	if err := s.queueOCR(ctx); err != nil && ctx.Err() == nil {
		slog.Error("queue ocr after batch tagging", "err", err)
	}
	return batchOutcomes(results), nil
}

// tagNamed is the tag of that name, made if there is none.
func (s *SearchService) tagNamed(ctx context.Context, name string) (int64, error) {
	id, _, err := s.store.TagByName(ctx, name)
	if errors.Is(err, store.ErrNotFound) {
		id, err = s.store.CreateTag(ctx, name, core.TagManual, nil)
	}
	return id, err
}

// queueOCR follows a tag put by hand: a photo the user calls a screenshot
// is one to read, if that is on.
func (s *SearchService) queueOCR(ctx context.Context) error {
	if s.ix != nil {
		return s.ix.QueueOCR(ctx) // and wakes the stage
	}
	_, err := s.store.QueueOCR(ctx)
	return err
}

// UntagAsset takes a tag off a photo. The AI won't put it back.
func (s *SearchService) UntagAsset(ctx context.Context, assetID, tagID int64) error {
	return s.store.UntagAsset(ctx, assetID, tagID)
}

// tagConfidence is the user's minimum AI probability for a tag to apply
// (SetTagConfidence keeps it in range; the store caches it).
func tagConfidence(ctx context.Context, st *store.Store) (float64, error) {
	return st.Float(ctx, core.SettingTagConfidence, core.TagConfidenceDefault)
}

// textHits returns the photos whose text holds every word of query, newest
// first, among theirs if it is not nil (chosen before the limit).
func (s *SearchService) textHits(ctx context.Context, query string, theirs map[int64]bool) ([]int64, error) {
	if theirs == nil {
		return s.store.TextMatches(ctx, query, textHitLimit)
	}
	return s.store.TextMatchesAmong(ctx, query, textHitLimit, slices.Collect(maps.Keys(theirs)))
}

// mergeHits lists the photos found by their text, then those found by their
// meaning that the text did not find, and the moments of the video matches.
func mergeHits(withText []int64, meaning []search.Hit) (ids []int64, moments map[int64]int64) {
	ids = slices.Clone(withText)
	seen := make(map[int64]bool, len(withText)+len(meaning))
	for _, id := range withText {
		seen[id] = true
	}
	moments = make(map[int64]int64, len(meaning))
	for _, h := range meaning {
		if !seen[h.ID] {
			seen[h.ID] = true
			ids = append(ids, h.ID)
			moments[h.ID] = h.At
		}
	}
	return ids, moments
}

// appendUnseen is first followed by the photos of more that it does not
// hold.
func appendUnseen(first, more []int64) []int64 {
	ids := slices.Clone(first)
	seen := make(map[int64]bool, len(first))
	for _, id := range first {
		seen[id] = true
	}
	for _, id := range more {
		if !seen[id] {
			ids = append(ids, id)
		}
	}
	return ids
}
