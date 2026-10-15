package store

import (
	"database/sql"
	"reflect"
	"testing"

	"lumen/internal/core"
)

func TestDuplicateRebuildPreservesEveryFieldAndTheReadSnapshot(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	check(t, st.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO sources(id, path, added_at) VALUES (1, 'C:\Photos', 0);
			INSERT INTO assets(id, content_hash, kind, ext, taken_at, added_at, width, height, thumb_version, phash, camera, date_from, lat, lon, hidden)
			VALUES
			(1, 'a', 'photo', '.jpg', 100, 11, 2000, 1000, 7, -1, 'Camera', 'exif', 0, 0, 0),
			(2, 'b', 'photo', '.jpg', 200, 22, 4000, 2000, 8, -2, '', 'filename', 1, NULL, 0),
			(3, 'c', 'photo', '.ARW', 300, 33, 6000, 3000, 9, -1, NULL, 'exif', 1, 2, 0),
			(4, 'd', 'photo', '.arw', 400, 44, 3000, 1500, 10, -2, NULL, 'file', NULL, NULL, 0),
			(5, 'e', 'video', '.mp4', 500, 55, NULL, NULL, 0, NULL, NULL, 'file', NULL, 2, 0),
			(6, 'f', 'photo', '.jpg', 600, 66, 2000, 1000, 1, -1, NULL, 'file', NULL, NULL, 1),
			(7, 'g', 'photo', '.jpg', 700, 77, 2000, 1000, 1, -1, NULL, 'file', NULL, NULL, 0),
			(8, 'h', 'photo', '.jpg', 800, 88, 2000, 1000, 1, 0, NULL, 'file', NULL, NULL, 0),
			(9, 'i', 'photo', '.png', 900, 99, 2000, 1000, 2, 1, NULL, 'exif', NULL, 2, 0);
			INSERT INTO files(asset_id, source_id, path, size, mtime) VALUES
			(1, 1, 'C:\Photos\a-copy.jpg', 101, 9000000001), (1, 1, 'C:\Photos\a.jpg', 101, 4000000001),
			(2, 1, 'C:\Photos\b.jpg', 202, 5000000001),
			(3, 1, 'C:\Photos\c.ARW', 303, 6000000001), (4, 1, 'C:\Photos\d.arw', 404, 7000000001),
			(5, 1, 'C:\Photos\e-copy.mp4', 505, 8000000001), (5, 1, 'C:\Photos\e.mp4', 505, 0),
			(6, 1, 'C:\Photos\f.jpg', 606, 0), (6, 1, 'C:\Photos\f-copy.jpg', 606, 0),
			(7, 1, 'C:\Photos\g.jpg', 707, 0), (7, 1, 'C:\Photos\g-copy.jpg', 707, 0),
			(8, 1, 'C:\Photos\h.jpg', 808, 0), (9, 1, 'C:\Photos\i.png', 909, 0)`)
		return err
	}))
	_, err := st.SetBlock(ctx, core.BlockAsset, 7, "", core.BlockPhotos)
	check(t, err)
	a := core.DuplicateItem{ID: 1, Paths: []string{`C:\Photos\a-copy.jpg`, `C:\Photos\a.jpg`}, Mtimes: []int64{9, 4},
		Size: 101, Width: 2000, Height: 1000, Thumb: 7, TakenAt: 100, Ext: ".jpg", Camera: true, ExifDate: true, GPS: true, AddedAt: 11}
	b := core.DuplicateItem{ID: 2, Paths: []string{`C:\Photos\b.jpg`}, Mtimes: []int64{5},
		Size: 202, Width: 4000, Height: 2000, Thumb: 8, TakenAt: 200, Ext: ".jpg", Camera: true, AddedAt: 22, Best: true}
	c := core.DuplicateItem{ID: 3, Paths: []string{`C:\Photos\c.ARW`}, Mtimes: []int64{6},
		Size: 303, Width: 6000, Height: 3000, Thumb: 9, TakenAt: 300, Ext: ".ARW", ExifDate: true, GPS: true, AddedAt: 33, Best: true}
	d := core.DuplicateItem{ID: 4, Paths: []string{`C:\Photos\d.arw`}, Mtimes: []int64{7},
		Size: 404, Width: 3000, Height: 1500, Thumb: 10, TakenAt: 400, Ext: ".arw", AddedAt: 44}
	e := core.DuplicateItem{ID: 5, Paths: []string{`C:\Photos\e-copy.mp4`, `C:\Photos\e.mp4`}, Mtimes: []int64{8, 0},
		Size: 505, TakenAt: 500, Ext: ".mp4", AddedAt: 55, Best: true, Keep: `C:\Photos\e-copy.mp4`}
	h := core.DuplicateItem{ID: 8, Paths: []string{`C:\Photos\h.jpg`}, Mtimes: []int64{0},
		Size: 808, Width: 2000, Height: 1000, Thumb: 1, TakenAt: 800, Ext: ".jpg", AddedAt: 88}
	i := core.DuplicateItem{ID: 9, Paths: []string{`C:\Photos\i.png`}, Mtimes: []int64{0},
		Size: 909, Width: 2000, Height: 1000, Thumb: 2, TakenAt: 900, Ext: ".png", ExifDate: true, AddedAt: 99, Best: true}
	exactA := a
	exactA.Best, exactA.Keep = true, `C:\Photos\a.jpg`
	want := []core.DuplicateGroup{
		{Kind: core.DuplicateExact, Items: []core.DuplicateItem{e}},
		{Kind: core.DuplicateExact, Items: []core.DuplicateItem{exactA}},
		{Kind: core.DuplicateSimilar, Items: []core.DuplicateItem{i, h}},
		{Kind: core.DuplicateSimilar, Items: []core.DuplicateItem{d, c}},
		{Kind: core.DuplicateSimilar, Items: []core.DuplicateItem{b, a}},
	}
	got, err := st.DuplicateGroups(ctx)
	check(t, err)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("groups = %#v, want %#v", got, want)
	}
	check(t, st.read(ctx, func(tx *sql.Tx) error {
		var revision int64
		check(t, tx.QueryRowContext(ctx, `SELECT duplicate_value FROM event_revision WHERE id = 1`).Scan(&revision))
		// A concurrent write must not mix new display fields or paths into these candidates.
		check(t, st.tx(ctx, func(writer *sql.Tx) error {
			_, err := writer.ExecContext(ctx, `UPDATE assets SET taken_at = 999, camera = NULL, thumb_version = 20 WHERE id = 1;
				UPDATE files SET path = 'C:\Photos\renamed.jpg', size = 999, mtime = 1000000000 WHERE asset_id = 2`)
			return err
		}))
		got, err := duplicateGroups(ctx, tx)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("snapshot groups = %#v, want %#v", got, want)
		}
		return nil
	}))
}
