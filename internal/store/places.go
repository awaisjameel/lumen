package store

import (
	"context"
	"database/sql"
	"errors"

	"lumen/internal/core"
	"lumen/internal/places"
)

func savePlace(ctx context.Context, tx *sql.Tx, city places.City) error {
	if city.ID == 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO places(id, name, region, country, lat, lon)
		VALUES (?, ?, ?, ?, ?, ?)`, city.ID, city.Name, city.Region, city.Country, city.Lat, city.Lon)
	return err
}

const pendingPlacesSQL = `SELECT id, lat, lon FROM assets
	WHERE place_checked = 0 AND lat IS NOT NULL AND lon IS NOT NULL ORDER BY id LIMIT 500`

// BackfillPlaces resolves coordinates indexed before the offline place index
// was introduced. Each small batch commits independently so indexing can run
// alongside it. It is safe to resume after shutdown.
func (s *Store) BackfillPlaces(ctx context.Context) (int, error) {
	total := 0
	for {
		type point struct {
			id       int64
			lat, lon float64
			city     places.City
		}
		batch, err := queryAll(ctx, s.r, func(r *sql.Rows) (p point, err error) {
			err = r.Scan(&p.id, &p.lat, &p.lon)
			return p, err
		}, pendingPlacesSQL)
		if err != nil || len(batch) == 0 {
			return total, err
		}
		for i := range batch {
			batch[i].city, _, err = places.Lookup(batch[i].lat, batch[i].lon)
			if err != nil {
				return total, err
			}
		}
		updated := 0
		err = s.tx(ctx, func(tx *sql.Tx) error {
			update, err := tx.PrepareContext(ctx, `UPDATE assets SET place_id = ?, place_checked = 1
				WHERE id = ? AND place_checked = 0`)
			if err != nil {
				return err
			}
			defer update.Close()
			saved := map[int64]bool{}
			for _, p := range batch {
				if !saved[p.city.ID] {
					if err := savePlace(ctx, tx, p.city); err != nil {
						return err
					}
					saved[p.city.ID] = true
				}
				id := sql.NullInt64{Int64: p.city.ID, Valid: p.city.ID != 0}
				result, err := update.ExecContext(ctx, id, p.id)
				if err != nil {
					return err
				}
				n, err := result.RowsAffected()
				if err != nil {
					return err
				}
				updated += int(n)
			}
			return nil
		})
		if err != nil {
			return total, err
		}
		total += updated
	}
}

// Places lists cities with visible media, with live counts and covers.
func (s *Store) Places(ctx context.Context) ([]core.Place, error) {
	return queryAll(ctx, s.r, func(r *sql.Rows) (p core.Place, err error) {
		err = r.Scan(&p.ID, &p.Name, &p.Region, &p.Country, &p.Lat, &p.Lon, &p.Count, &p.Cover, &p.CoverThumb)
		return p, err
	}, `SELECT g.id, g.name, g.region, g.country, g.lat, g.lon, g.n, COALESCE(c.id, 0), COALESCE(c.thumb_version, 0)
		FROM (SELECT p.id, p.name, p.region, p.country, p.lat, p.lon, count(a.id) AS n,
				(SELECT x.id FROM assets x WHERE x.place_id = p.id AND `+visibleX+` AND x.thumb_version > 0
					ORDER BY x.taken_at DESC, x.id DESC LIMIT 1) AS cover
			FROM places p JOIN assets a ON a.place_id = p.id AND `+visibleA+`
			WHERE NOT `+placeBlocked("p.id")+`
			GROUP BY p.id) g
		LEFT JOIN assets c ON c.id = g.cover
		ORDER BY g.n DESC, `+byName("g.name")+`, g.id`)
}

// Place returns a visible city, or ErrNotFound for an unknown or empty one.
func (s *Store) Place(ctx context.Context, id int64) (core.Place, error) {
	var p core.Place
	err := s.r.QueryRowContext(ctx, `SELECT p.id, p.name, p.region, p.country, p.lat, p.lon, count(a.id)
		FROM places p JOIN assets a ON a.place_id = p.id AND `+visibleA+` WHERE p.id = ?
		AND NOT `+placeBlocked("p.id")+` GROUP BY p.id`, id).
		Scan(&p.ID, &p.Name, &p.Region, &p.Country, &p.Lat, &p.Lon, &p.Count)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}
