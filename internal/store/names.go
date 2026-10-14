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
	err := q.QueryRowContext(ctx, "SELECT id FROM "+table+" WHERE "+foldName+" = ? AND id != ? LIMIT 1",
		core.Fold(name), except).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// foldName is the name column's core.Fold key (see foldKey).
var foldName = foldKey("name")

// nameMatch is the condition for a name column to contain the folded text
// ?1, and nameFirst orders names starting with it first: suggestions as the
// user types.
var (
	nameMatch = "instr(" + foldName + ", ?1) > 0"
	nameFirst = "instr(" + foldName + ", ?1) = 1 DESC, length(name), " + byName("name")
)

// SearchNames finds candidate names, including blocked labels, in one snapshot.
func (s *Store) SearchNames(ctx context.Context, query string) ([]core.NamedTarget, error) {
	var queries []string
	for _, target := range []struct {
		table string
		kind  core.BlockKind
	}{
		{peopleNames, core.BlockPerson}, {tagNames, core.BlockTag}, {"places", core.BlockPlace}, {albumNames, core.BlockAlbum},
	} {
		name := target.table + ".name"
		q := "SELECT id, name, '" + string(target.kind) + "', " + labelBlocked(target.kind, target.table+".id") +
			" FROM " + target.table + " WHERE name != '' AND instr(?1, " + foldKey(name) + ") > 0"
		if target.kind == core.BlockTag {
			q += " AND is_sink = 0"
		}
		queries = append(queries, q)
	}
	// An event has no row to name: its block holds the name it had when it was
	// blocked, so a search for that name finds the block, whatever its mode.
	queries = append(queries, "SELECT target_id, name, '"+string(core.BlockEvent)+"', 1 FROM blocks WHERE kind = '"+
		string(core.BlockEvent)+"' AND name != '' AND instr(?1, "+foldKey("blocks.name")+") > 0")
	return queryAll(ctx, s.r, func(r *sql.Rows) (target core.NamedTarget, err error) {
		err = r.Scan(&target.ID, &target.Name, &target.Kind, &target.Blocked)
		return target, err
	}, strings.Join(queries, " UNION ALL "), core.Fold(query))
}
