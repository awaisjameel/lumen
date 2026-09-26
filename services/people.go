package services

import (
	"context"
	"errors"

	"lumen/internal/core"
	"lumen/internal/people"
	"lumen/internal/store"
)

// PeopleService is the people in the photos: who they are, and the user's
// corrections, which always win over the AI.
type PeopleService struct {
	store  *store.Store
	engine *people.Engine // nil when faces can't be recognised
}

func NewPeopleService(st *store.Store, engine *people.Engine) *PeopleService {
	return &PeopleService{store: st, engine: engine}
}

// People lists everyone in the photos: named people first, then by how
// often they appear.
func (s *PeopleService) People(ctx context.Context) ([]core.Person, error) {
	return s.store.People(ctx)
}

// Person returns one person.
func (s *PeopleService) Person(ctx context.Context, id int64) (core.Person, error) {
	return s.store.Person(ctx, id)
}

// PersonFaces lists a person's faces for review, the least alike first, so
// a face that isn't them is easy to spot.
func (s *PeopleService) PersonFaces(ctx context.Context, id int64) ([]core.Face, error) {
	faces, vecs, err := s.store.PersonFaces(ctx, id)
	if err != nil {
		return nil, err
	}
	return people.OddOnesFirst(faces, vecs), nil
}

// AssetFaces lists the faces in one photo, left to right.
func (s *PeopleService) AssetFaces(ctx context.Context, assetID int64) ([]core.Face, error) {
	return s.store.AssetFaces(ctx, assetID)
}

// SuggestPeople returns named people whose names contain text.
func (s *PeopleService) SuggestPeople(ctx context.Context, text string) ([]core.Person, error) {
	return s.store.SearchPeople(ctx, text, 6)
}

// PersonByName returns the id of whoever has that name, or 0. The UI asks
// before naming someone with a name already taken: they may be one person.
func (s *PeopleService) PersonByName(ctx context.Context, name string) (int64, error) {
	id, err := s.store.PersonByName(ctx, name)
	if errors.Is(err, store.ErrNotFound) {
		return 0, nil
	}
	return id, err
}

// NamePerson names someone ("" takes the name away).
func (s *PeopleService) NamePerson(ctx context.Context, id int64, name string) error {
	return s.edited(s.store.NamePerson(ctx, id, name))
}

// MergePeople says two people are one: everything of from moves to into.
func (s *PeopleService) MergePeople(ctx context.Context, from, into int64) error {
	return s.edited(s.store.MergePeople(ctx, from, into))
}

// SetPersonHidden hides someone from People, or shows them again.
func (s *PeopleService) SetPersonHidden(ctx context.Context, id int64, hidden bool) error {
	return s.store.SetPersonHidden(ctx, id, hidden)
}

// SetPersonCover shows one of a person's faces for them.
func (s *PeopleService) SetPersonCover(ctx context.Context, id, face int64) error {
	return s.store.SetPersonCover(ctx, id, face)
}

// RejectFaces says faces are not this person. The AI never puts them back.
func (s *PeopleService) RejectFaces(ctx context.Context, person int64, faces []int64) error {
	return s.edited(s.store.RejectFaces(ctx, person, faces))
}

// MoveFaces puts faces with another person.
func (s *PeopleService) MoveFaces(ctx context.Context, faces []int64, person int64) error {
	return s.edited(s.store.MoveFaces(ctx, faces, person))
}

// NameFaces puts faces with the person of that name, who is created if new
// ("" makes a new unnamed person: splitting someone in two). It returns
// their id.
func (s *PeopleService) NameFaces(ctx context.Context, faces []int64, name string) (int64, error) {
	id, err := s.store.PersonByName(ctx, name)
	if errors.Is(err, store.ErrNotFound) || name == "" {
		id, err = s.store.CreatePerson(ctx, name)
	}
	if err != nil {
		return 0, err
	}
	return id, s.edited(s.store.MoveFaces(ctx, faces, id))
}

// edited asks for clustering after a correction succeeded (the faces it
// freed may belong to someone else).
func (s *PeopleService) edited(err error) error {
	if err == nil && s.engine != nil {
		s.engine.Nudge()
	}
	return err
}
