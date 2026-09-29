package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"lumen/internal/core"
)

// ErrInvalidName means a name is empty or longer than core.MaxNameLen.
var ErrInvalidName = fmt.Errorf("a name must be 1 to %d characters", core.MaxNameLen)

// cleanName trims a name the user typed and checks its length.
func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > core.MaxNameLen {
		return "", ErrInvalidName
	}
	return name, nil
}

// Tables whose rows have user-visible names, unique whatever their case.
const (
	tagNames    = "tags"
	peopleNames = "people"
	albumNames  = "albums"
)

// named returns the id of the row in table (one of the *Names tables) named
// name in any case (core.Fold), other than except, or 0 if there is none.
// The tables' NOCASE UNIQUE constraints fold only A-Z; this is the check
// for every other letter, run in the writer's transaction before a write.
func named(ctx context.Context, q querier, table, name string, except int64) (int64, error) {
	var id int64
	err := q.QueryRowContext(ctx, "SELECT id FROM "+table+" WHERE fold(name) = ? AND id != ? LIMIT 1",
		core.Fold(name), except).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// nameMatch is the condition for a name column to contain the folded text
// ?1, and nameFirst orders names starting with it first: suggestions as the
// user types.
const (
	nameMatch = "instr(fold(name), ?1) > 0"
	nameFirst = "instr(fold(name), ?1) = 1 DESC, length(name), name"
)
