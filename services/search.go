package services

import (
	"context"
	"errors"

	"lumen/internal/core"
	"lumen/internal/people"
	"lumen/internal/search"
	"lumen/internal/store"
)

// ErrNoAI means the on-device AI isn't available, so search and Things can't
// work (the progress event's ai.error says why).
var ErrNoAI = errors.New("the on-device AI isn't available")

// suggestions is how many matches the search box suggests of each kind.
const suggestions = 6

// SearchService is natural-language search and Things (tags).
type SearchService struct {
	store  *store.Store
	engine *search.Engine // nil when the AI can't run
	faces  bool           // faces are being recognised (the people engine runs)
}

// NewSearchService returns the search service (engine is nil without the AI;
// faces says whether faces are recognised).
func NewSearchService(st *store.Store, engine *search.Engine, faces bool) *SearchService {
	return &SearchService{store: st, engine: engine, faces: faces}
}

// Search finds photos matching a description, best match first. Named
// people in it narrow the search to photos of all of them ("Ali at the
// beach"); names alone give their photos, newest first. Without the AI,
// the description can't be searched (the result says so): named people's
// photos are still found.
func (s *SearchService) Search(ctx context.Context, query string) (core.SearchResult, error) {
	named, err := s.store.NamedPeople(ctx)
	if err != nil {
		return core.SearchResult{}, err
	}
	who, names, text := people.FindNames(query, named)
	var ids []int64
	var moments map[int64]int64
	var waitFor []core.Stage // stages whose photos can't be found yet
	complete, noAI := true, false
	if len(who) > 0 {
		if ids, err = s.store.PeopleAssets(ctx, who); err != nil {
			return core.SearchResult{}, err
		}
		if s.faces {
			waitFor = append(waitFor, core.StageFaces)
		}
	}
	switch {
	case text == "" || (len(who) > 0 && len(ids) == 0):
		// Nothing to describe, or no photo has all of them.
	case s.engine == nil:
		noAI = true
	default:
		waitFor = append(waitFor, core.StageEmbedding)
		var allow func(int64) bool
		if len(who) > 0 {
			theirs := make(map[int64]bool, len(ids))
			for _, id := range ids {
				theirs[id] = true
			}
			allow = func(id int64) bool { return theirs[id] }
		}
		hits, all, err := s.engine.Search(ctx, text, allow)
		if errors.Is(err, search.ErrNotLoaded) {
			noAI = true
			break
		}
		if err != nil {
			return core.SearchResult{}, err
		}
		complete = all
		ids = make([]int64, len(hits))
		moments = make(map[int64]int64, len(hits))
		for i, h := range hits {
			ids[i] = h.ID
			moments[h.ID] = h.At
		}
	}
	if noAI && len(who) == 0 {
		ids = nil // the description was all there was to search
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
	if names == nil {
		names = []string{}
	}
	return core.SearchResult{Items: items, People: names, Pending: pending, Complete: complete, NoAI: noAI}, nil
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
	return s.store.Things(ctx, float32(minScore))
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

// SetTagHidden hides a Thing from the Things view, or shows it again.
func (s *SearchService) SetTagHidden(ctx context.Context, id int64, hidden bool) error {
	return s.store.SetTagHidden(ctx, id, hidden)
}

// AssetTags lists the tags on one photo.
func (s *SearchService) AssetTags(ctx context.Context, assetID int64) ([]core.AssetTag, error) {
	minScore, err := tagConfidence(ctx, s.store)
	if err != nil {
		return nil, err
	}
	return s.store.AssetTags(ctx, assetID, float32(minScore))
}

// TagAsset puts a tag on a photo by name, creating the tag if it's new.
func (s *SearchService) TagAsset(ctx context.Context, assetID int64, name string) error {
	id, _, err := s.store.TagByName(ctx, name)
	if errors.Is(err, store.ErrNotFound) {
		id, err = s.store.CreateTag(ctx, name, core.TagManual, nil)
	}
	if err != nil {
		return err
	}
	return s.store.TagAsset(ctx, assetID, id)
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
