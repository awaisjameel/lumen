package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"time"

	"lumen/internal/core"
	"lumen/internal/places"
)

// Correction errors distinguish an invalid selection from invalid field values.
var (
	ErrCorrectionSelection = errors.New("select at least one asset")
	ErrCorrectionDate      = errors.New("capture date must be between 1800 and 2200")
	ErrCorrectionLocation  = errors.New("coordinates are outside their valid ranges")
)

// SetTakenAt corrects the wall-clock capture time without changing a file.
// Seconds are used so there is no time zone conversion or normalisation of an
// invalid calendar string at the engine boundary. The supported years are
// 1800..2199, including pre-epoch dates and the epoch itself.
func (s *Store) SetTakenAt(ctx context.Context, ids []int64, takenAt int64) error {
	year := time.Unix(takenAt, 0).UTC().Year()
	if year < 1800 || year >= 2200 {
		return ErrCorrectionDate
	}
	return s.correctAssets(ctx, ids, `UPDATE assets SET
		original_taken_at = COALESCE(original_taken_at, taken_at),
		original_date_from = COALESCE(original_date_from, date_from),
		taken_at = ?, date_from = 'user'`, takenAt)
}

// RestoreTakenAt removes a date correction, using the latest metadata date.
func (s *Store) RestoreTakenAt(ctx context.Context, ids []int64) error {
	return s.correctAssets(ctx, ids, `UPDATE assets SET
		taken_at = COALESCE(original_taken_at, taken_at),
		date_from = COALESCE(original_date_from, date_from),
		original_taken_at = NULL, original_date_from = NULL`)
}

// SetLocation corrects coordinates, using the same offline city lookup as
// indexing. Changing place_id uses the existing visibility and block triggers;
// Store.tx settles coverage and kept counts before readers see the change.
func (s *Store) SetLocation(ctx context.Context, ids []int64, lat, lon float64) error {
	if math.IsNaN(lat) || math.IsNaN(lon) || math.IsInf(lat, 0) || math.IsInf(lon, 0) || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return ErrCorrectionLocation
	}
	city, _, err := places.Lookup(lat, lon)
	if err != nil {
		return err
	}
	return s.editAssets(ctx, ids, func(tx *sql.Tx, list string) error {
		if err := savePlace(ctx, tx, city); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE assets SET
			original_lat = CASE WHEN location_corrected = 0 THEN lat ELSE original_lat END,
			original_lon = CASE WHEN location_corrected = 0 THEN lon ELSE original_lon END,
			original_place_id = CASE WHEN location_corrected = 0 THEN place_id ELSE original_place_id END,
			lat = ?, lon = ?, place_id = ?, place_checked = 1, location_corrected = 1
			WHERE id IN (SELECT value FROM json_each(?))`, lat, lon, sql.NullInt64{Int64: city.ID, Valid: city.ID != 0}, list)
		return err
	})
}

// RestoreLocation removes a location correction, including restoring no GPS.
func (s *Store) RestoreLocation(ctx context.Context, ids []int64) error {
	return s.correctAssets(ctx, ids, `UPDATE assets SET
		lat = CASE WHEN location_corrected = 1 THEN original_lat ELSE lat END,
		lon = CASE WHEN location_corrected = 1 THEN original_lon ELSE lon END,
		place_id = CASE WHEN location_corrected = 1 THEN original_place_id ELSE place_id END,
		original_lat = NULL, original_lon = NULL, original_place_id = NULL, location_corrected = 0`)
}

func (s *Store) correctAssets(ctx context.Context, ids []int64, update string, args ...any) error {
	return s.editAssets(ctx, ids, func(tx *sql.Tx, list string) error {
		_, err := tx.ExecContext(ctx, update+" WHERE id IN (SELECT value FROM json_each(?))", append(args, list)...)
		return err
	})
}

// editAssets validates the entire selection in the write snapshot. A correction
// may itself put an asset inside a place or smart-album block, so the inverse
// must still accept that known ID. No blocked metadata is returned by editing.
func (s *Store) editAssets(ctx context.Context, ids []int64, edit func(*sql.Tx, string) error) error {
	if len(ids) == 0 {
		return ErrCorrectionSelection
	}
	list, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		if err := requireAll(ctx, tx, baseVisibleOf(""), string(list)); err != nil {
			return err
		}
		if err := markKeptCountsForChange(ctx, tx, int64(len(ids))); err != nil {
			return err
		}
		return edit(tx, string(list))
	})
}

// Counts reads aggregate tables in one snapshot. No asset scan or duplicate
// similarity grouping runs while the sidebar asks for its totals.
func (s *Store) Counts(ctx context.Context) (core.LibraryCounts, error) {
	var c core.LibraryCounts
	err := s.r.QueryRowContext(ctx, `SELECT COALESCE(sum(total - videos), 0), COALESCE(sum(videos), 0),
		COALESCE(sum(favourites), 0),
		(SELECT count(*) FROM people p JOIN person_counts c ON c.person_id = p.id
		 WHERE c.count > 0 AND p.name IS NOT NULL AND p.name != '' AND NOT `+labelBlocked(core.BlockPerson, "p.id")+`),
		(SELECT n FROM trash_count WHERE id = 1) FROM day_counts`).
		Scan(&c.Photos, &c.Videos, &c.Favourites, &c.People, &c.Trash)
	return c, err
}
