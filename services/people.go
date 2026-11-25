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

// NewPeopleService returns the people service (engine is nil without face models).
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

// companions is how many people a person's page names as most often with them.
const companions = 3

// PersonFacts returns how many faces a person has and who is most often in
// photos with them.
func (s *PeopleService) PersonFacts(ctx context.Context, id int64) (core.PersonFacts, error) {
	return s.store.PersonFacts(ctx, id, companions)
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
	return s.store.SearchPeople(ctx, text, suggestions)
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
// If they have different relationships, or one is Me and the other has a
// relationship, keep says whose relationship stays; MergeKeepAsk refuses
// (relation_conflict) so the user can be asked.
func (s *PeopleService) MergePeople(ctx context.Context, from, into int64, keep core.MergeKeep) error {
	return s.edited(s.store.MergePeople(ctx, from, into, keep))
}

// SetMe marks a person as the user. Unless replace, it refuses
// (relation_conflict) when someone else is Me or the person has a
// relationship, so the user confirms replacing either.
func (s *PeopleService) SetMe(ctx context.Context, id int64, replace bool) error {
	return s.store.SetMe(ctx, id, replace)
}

// RemoveMe says a person is not the user. Relationships stay: they are to
// the user, not to that person.
func (s *PeopleService) RemoveMe(ctx context.Context, id int64) error {
	return s.store.RemoveMe(ctx, id)
}

// SetRelation sets what a person is to the user (RelationNone removes it)
// and returns the person as they are now: a custom label that is a built-in
// word comes back as that relationship. Neither faces nor clustering are
// touched.
func (s *PeopleService) SetRelation(ctx context.Context, id int64, relation core.Relation, label string) (core.Person, error) {
	if _, _, err := s.store.SetRelation(ctx, id, relation, label); err != nil {
		return core.Person{}, err
	}
	return s.store.Person(ctx, id)
}

// SetPersonCover shows one of a person's faces for them.
func (s *PeopleService) SetPersonCover(ctx context.Context, id, face int64) error {
	return s.store.SetPersonCover(ctx, id, face)
}

// RejectFaces says faces are not this person. The AI never puts them back.
func (s *PeopleService) RejectFaces(ctx context.Context, person int64, faces []int64) error {
	return s.edited(s.store.RejectFaces(ctx, person, faces))
}

// NameFaces puts faces with the person of that name, who is created if new
// ("" makes a new unnamed person: splitting someone in two). It returns
// their id.
func (s *PeopleService) NameFaces(ctx context.Context, faces []int64, name string) (int64, error) {
	id, err := s.store.NameFaces(ctx, faces, name)
	return id, s.edited(err)
}

// edited asks for clustering after a correction succeeded (the faces it
// freed may belong to someone else).
func (s *PeopleService) edited(err error) error {
	if err == nil && s.engine != nil {
		s.engine.Nudge()
	}
	return err
}
