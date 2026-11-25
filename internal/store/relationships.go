package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"lumen/internal/core"
)

// ErrInvalidRelation means a relationship is not one Lumen offers, or its
// label is empty, too long, or a word for the user or for several
// relationships at once.
var ErrInvalidRelation = fmt.Errorf("a relationship must be one Lumen offers or a label of 1 to %d characters", core.MaxRelationLabelLen)

// ErrRelationConflict means a change would quietly undo another of the
// user's choices: someone else is already Me, the new Me has a relationship,
// Me can't have a relationship, or two people being merged disagree. The UI
// explains which and asks before trying again with the user's answer.
var ErrRelationConflict = errors.New("that conflicts with who is Me or a relationship already set")

// shownPerson is true when the people row p is neither blocked nor covered
// by face blocks: only such a person can be chosen as Me or given a
// relationship, so a direct call can't reach a hidden identity either.
var shownPerson = "NOT " + labelBlocked(core.BlockPerson, "p.id") + " AND NOT " + personCovered("p.id")

// SetMe marks person id as the user. Unless replace, it is
// ErrRelationConflict when someone else shown is Me (they stop being) or id
// has a relationship (it goes: Me has none), so the user confirms either.
// A Me who is hidden is replaced without asking, as asking would name them.
func (s *Store) SetMe(ctx context.Context, id int64, replace bool) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		var related, otherMe bool
		err := tx.QueryRowContext(ctx, `SELECT p.relation IS NOT NULL,
				EXISTS (SELECT 1 FROM people p WHERE p.is_me = 1 AND p.id <> ?1 AND `+shownPerson+`)
			FROM people p WHERE p.id = ?1 AND `+shownPerson, id).Scan(&related, &otherMe)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return ErrNotFound
		case err != nil:
			return err
		case !replace && (related || otherMe):
			return ErrRelationConflict
		}
		if _, err := tx.ExecContext(ctx, "UPDATE people SET is_me = 0 WHERE is_me = 1 AND id <> ?", id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE people SET is_me = 1, relation = NULL, relation_label = NULL WHERE id = ?", id); err != nil {
			return err
		}
		return dropEmptyPeople(ctx, tx) // a faceless, unnamed former Me
	})
}

// RemoveMe says person id is not the user after all. Their relationships to
// everyone else stay: those are relative to the user, not to a face group.
// It is nothing to do when id isn't Me.
func (s *Store) RemoveMe(ctx context.Context, id int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if err := affected(tx.ExecContext(ctx, "UPDATE people SET is_me = 0 WHERE id = ?1 AND EXISTS (SELECT 1 FROM people p WHERE p.id = ?1 AND "+shownPerson+")", id)); err != nil {
			return err
		}
		return dropEmptyPeople(ctx, tx)
	})
}

// SetRelation sets what person id is to the user (RelationNone takes it
// away), as core.CleanRelation keeps it, and returns that. A name is
// separate and stays as it is. The user has no relationship to themself, so
// for Me it is ErrRelationConflict.
func (s *Store) SetRelation(ctx context.Context, id int64, relation core.Relation, label string) (core.Relation, string, error) {
	relation, label, ok := core.CleanRelation(relation, label)
	if !ok {
		return "", "", ErrInvalidRelation
	}
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var me bool
		err := tx.QueryRowContext(ctx, "SELECT p.is_me FROM people p WHERE p.id = ? AND "+shownPerson, id).Scan(&me)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return ErrNotFound
		case err != nil:
			return err
		case me && relation != core.RelationNone:
			return ErrRelationConflict
		}
		if _, err := tx.ExecContext(ctx, "UPDATE people SET relation = NULLIF(?, ''), relation_label = NULLIF(?, '') WHERE id = ?",
			relation, label, id); err != nil || relation != core.RelationNone {
			return err
		}
		return dropEmptyPeople(ctx, tx) // an unnamed, faceless person kept only by it
	})
	if err != nil {
		return "", "", err
	}
	return relation, label, nil
}

// relationsDisagree reports whether merging a and b would lose one of the
// user's choices: both have relationships that differ, or one is the user
// and the other has a relationship.
func relationsDisagree(a, b core.RelatedPerson) bool {
	switch {
	case a.Me && b.Relation != core.RelationNone, b.Me && a.Relation != core.RelationNone:
		return true
	case a.Relation == core.RelationNone || b.Relation == core.RelationNone:
		return false
	}
	return !core.SameRelation(a.Relation, a.Label, b.Relation, b.Label)
}

// relatedTargets returns the phrases ("me", "my mum") that name the user and
// the people with a relationship, blocked ones included so a search can
// refuse them as it refuses blocked names. It reads only those few people,
// through people_related.
func relatedTargets(ctx context.Context, q querier) ([]core.NamedTarget, error) {
	people, err := queryAll(ctx, q, func(r *sql.Rows) (p core.RelatedPerson, err error) {
		err = r.Scan(&p.ID, &p.Me, &p.Relation, &p.Label, &p.Blocked)
		return p, err
	}, `SELECT p.id, p.is_me, COALESCE(p.relation, ''), COALESCE(p.relation_label, ''), NOT (`+shownPerson+`)
		FROM people p INDEXED BY people_related WHERE p.is_me = 1 OR p.relation IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	var targets []core.NamedTarget
	for _, p := range people {
		targets = append(targets, p.Targets()...)
	}
	return targets, nil
}
