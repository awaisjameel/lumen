package services

import (
	"context"
	"errors"

	"lumen/internal/core"
	"lumen/internal/search"
	"lumen/internal/store"
)

// ErrNoAI means the on-device AI isn't available, so search and Things can't
// work (the progress event's ai.error says why).
var ErrNoAI = errors.New("the on-device AI isn't available")

// SearchService is natural-language search and Things (tags).
type SearchService struct {
	store  *store.Store
	engine *search.Engine // nil when the AI can't run
}

func NewSearchService(st *store.Store, engine *search.Engine) *SearchService {
	return &SearchService{store: st, engine: engine}
}

// Search finds photos matching a description, best match first.
func (s *SearchService) Search(ctx context.Context, query string) (core.SearchResult, error) {
	if s.engine == nil {
		return core.SearchResult{}, ErrNoAI
	}
	hits, complete, err := s.engine.Search(ctx, query)
	if err != nil {
		return core.SearchResult{}, err
	}
	ids := make([]int64, len(hits))
	for i, h := range hits {
		ids[i] = h.ID
	}
	items, err := s.store.GridItems(ctx, ids)
	if err != nil {
		return core.SearchResult{}, err
	}
	pending, err := s.store.CountAwaitingAI(ctx)
	if err != nil {
		return core.SearchResult{}, err
	}
	if items == nil {
		items = []core.GridItem{}
	}
	return core.SearchResult{Items: items, Pending: pending, Complete: complete}, nil
}

// SuggestTags returns Things whose names contain text, for the search box.
func (s *SearchService) SuggestTags(ctx context.Context, text string) ([]core.Tag, error) {
	return s.store.SearchTags(ctx, text, 6)
}

// Things lists every tag with its count and cover, most used first.
func (s *SearchService) Things(ctx context.Context) ([]core.Tag, error) {
	min, err := tagConfidence(ctx, s.store)
	if err != nil {
		return nil, err
	}
	return s.store.Things(ctx, min)
}

// CreateLabel adds a Thing for the AI to look for, and finds it across the
// library straight away. It returns the tag's id; if a Thing by that name
// already exists, that one's id (the caller shows it either way).
func (s *SearchService) CreateLabel(ctx context.Context, name string) (int64, error) {
	if s.engine == nil {
		return 0, ErrNoAI
	}
	id, err := s.engine.CreateLabel(ctx, name)
	if errors.Is(err, store.ErrTagExists) {
		id, _, err = s.store.TagByName(ctx, name)
	}
	return id, err
}

// DeleteTag removes a tag the user made. Photos are untouched.
func (s *SearchService) DeleteTag(ctx context.Context, id int64) error {
	if err := s.store.DeleteTag(ctx, id); err != nil {
		return err
	}
	if s.engine != nil {
		s.engine.ForgetLabel(id)
	}
	return nil
}

// SetTagHidden hides a Thing from the Things view, or shows it again.
func (s *SearchService) SetTagHidden(ctx context.Context, id int64, hidden bool) error {
	return s.store.SetTagHidden(ctx, id, hidden)
}

// AssetTags lists the tags on one photo.
func (s *SearchService) AssetTags(ctx context.Context, assetID int64) ([]core.AssetTag, error) {
	min, err := tagConfidence(ctx, s.store)
	if err != nil {
		return nil, err
	}
	return s.store.AssetTags(ctx, assetID, min)
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

// tagConfidence is the user's minimum AI probability for a tag to apply.
func tagConfidence(ctx context.Context, st *store.Store) (float32, error) {
	v, err := st.Float(ctx, core.SettingTagConfidence, core.TagConfidenceDefault)
	return float32(min(max(v, core.TagConfidenceMin), core.TagConfidenceMax)), err
}
