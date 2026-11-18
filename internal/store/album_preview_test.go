package store

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mattn/go-sqlite3"

	"lumen/internal/core"
)

type previewAsset struct {
	item          core.GridItem
	day           string
	at            int64
	tagged, shown bool
	person        int64
}

// previewLibrary deliberately varies independent rule inputs, and includes
// rejected/weak tags and unavailable assets beside ordinary visible media.
func previewLibrary(t *testing.T) (st *Store, tag int64, assets []previewAsset) {
	t.Helper()
	st, ctx := openTest(t), t.Context()
	source, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	check(t, st.SyncVocabulary(ctx, []VocabLabel{{Name: "Voyâge", Prompts: []string{"a journey"}}}))
	tag, _, err = st.TagByName(ctx, "Voyâge")
	check(t, err)
	start := time.Date(2025, 3, 14, 0, 0, 0, 0, time.UTC)
	check(t, st.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "INSERT INTO people(id, name, created_at) VALUES (1, 'Ána', 0), (2, 'Éli', 0)"); err != nil {
			return err
		}
		for i := range 20 {
			id := int64(i + 1)
			kind, ext, duration := core.KindPhoto, ".jpg", int64(0)
			if i%3 == 0 {
				kind, ext, duration = core.KindVideo, ".mp4", 1234
			}
			at := start.AddDate(0, 0, i%4).Add(time.Duration(i%2) * time.Hour)
			favourite := i%2 == 0
			if _, err := tx.ExecContext(ctx, `INSERT INTO assets
				(id, content_hash, kind, ext, taken_at, added_at, width, height, thumb_version, favourite, duration_ms, date_from)
				VALUES (?, ?, ?, ?, ?, 0, 400, 300, 3, ?, ?, 'exif')`, id, []byte(fmt.Sprintf("preview-%d", i)), kind, ext, at.Unix(), favourite, duration); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO files(asset_id, source_id, path, size, mtime) VALUES (?, ?, ?, 1, 0)",
				id, source.ID, fmt.Sprintf(`C:\Photos\%d%s`, i, ext)); err != nil {
				return err
			}
			source, score := core.TagByUser, 0.0
			switch i % 4 {
			case 1:
				source, score = core.TagByAI, 0.8
			case 2:
				source, score = core.TagByAI, 0.001
			case 3:
				source = core.TagRejected
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO asset_tags(asset_id, tag_id, source, score) VALUES (?, ?, ?, ?)", id, tag, source, score); err != nil {
				return err
			}
			person := int64(0)
			if i%5 < 3 {
				person = 1
				if i%5 == 2 {
					person = 2
				}
				if _, err := tx.ExecContext(ctx, `INSERT INTO faces(asset_id, x, y, w, h, score, sharpness, yaw, model, vec, person_id, assigned_by)
					VALUES (?, 0.1, 0.1, 0.2, 0.2, 0.9, 20, 0, 'fixture', zeroblob(512), ?, 'user')`, id, person); err != nil {
					return err
				}
			}
			assets = append(assets, previewAsset{
				item: core.GridItem{ID: id, W: 400, H: 300, Video: kind == core.KindVideo, Duration: duration, Favourite: favourite, Thumb: 3},
				day:  at.Format(time.DateOnly), at: at.Unix(), tagged: i%4 < 2, shown: i < 16, person: person,
			})
		}
		for _, q := range []string{
			"UPDATE assets SET hidden = 1 WHERE id = 17",
			"UPDATE assets SET trashed_at = 1 WHERE id = 18",
			"UPDATE assets SET missing_since = 1 WHERE id = 19",
			"UPDATE assets SET display_rotation = 1, display_version = 2 WHERE id = 16",
		} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}))
	_, err = st.SetBlock(ctx, core.BlockAsset, 20, "", core.BlockPhotos)
	check(t, err)
	assets[15].item.W, assets[15].item.H, assets[15].item.Thumb = 300, 400, 5
	slices.SortFunc(assets, func(a, b previewAsset) int {
		if order := cmp.Compare(b.at, a.at); order != 0 {
			return order
		}
		return cmp.Compare(b.item.ID, a.item.ID)
	})
	return st, tag, assets
}

func TestSmartAlbumPreviewAgreesWithIndependentMembershipAndTheSavedAlbum(t *testing.T) {
	st, tag, assets := previewLibrary(t)
	ctx := t.Context()
	album, err := st.CreateAlbum(ctx, "Saved", core.AlbumSmart, core.AlbumRule{}, nil)
	check(t, err)
	for _, kind := range []core.Kind{"", core.KindPhoto, core.KindVideo} {
		for _, favourite := range []bool{false, true} {
			for _, from := range []string{"", "2025-03-15", "2025-03-16"} {
				for _, to := range []string{"", "2025-03-15", "2025-03-16"} {
					if from != "" && to != "" && from > to {
						continue
					}
					for _, thing := range []int64{0, tag} {
						for _, person := range []int64{0, 1, 2} {
							rule := core.AlbumRule{Kind: kind, Favourites: favourite, From: from, To: to, Tag: thing, Person: person}
							want := []core.GridItem{}
							for _, a := range assets {
								if !a.shown || (kind == core.KindPhoto && a.item.Video) || (kind == core.KindVideo && !a.item.Video) ||
									(favourite && !a.item.Favourite) || (from != "" && a.day < from) || (to != "" && a.day > to) ||
									(thing != 0 && !a.tagged) || (person != 0 && a.person != person) {
									continue
								}
								want = append(want, a.item)
							}
							preview, err := st.PreviewAlbum(ctx, rule, 0.3)
							check(t, err)
							if preview.Count != len(want) || !reflect.DeepEqual(preview.Items, want[:min(len(want), core.AlbumPreviewSize)]) {
								t.Fatalf("rule %+v: preview %+v, independent members %+v", rule, preview, want)
							}
							check(t, st.UpdateAlbum(ctx, album, "Saved", rule))
							saved, err := st.Albums(ctx, 0.3)
							check(t, err)
							days, err := st.DayItems(ctx, "2025-03-14", "2025-03-17", core.View{Album: album}, 0.3)
							check(t, err)
							members := []core.GridItem{}
							for _, day := range days {
								members = append(members, day.Items...)
							}
							if len(saved) != 1 || saved[0].Count != len(want) || !reflect.DeepEqual(members, want) ||
								(len(want) > 0 && saved[0].Cover != want[0].ID) {
								t.Fatalf("rule %+v: saved %+v, members %+v, want %+v", rule, saved, members, want)
							}
						}
					}
				}
			}
		}
	}
}

func TestSmartAlbumPreviewRefusesInvalidMissingAndBlockedReferencesWithoutPartialData(t *testing.T) {
	st, tag, _ := previewLibrary(t)
	for _, test := range []struct {
		rule core.AlbumRule
		err  error
	}{
		{core.AlbumRule{Kind: "unknown"}, ErrAlbumMediaKind},
		{core.AlbumRule{Tag: -1}, ErrAlbumSelection},
		{core.AlbumRule{From: "2025-02-30"}, ErrAlbumDate},
		{core.AlbumRule{From: "2025-03-16", To: "2025-03-15"}, ErrAlbumDateOrder},
		{core.AlbumRule{Tag: 999}, ErrAlbumReference},
		{core.AlbumRule{Person: 999}, ErrAlbumReference},
	} {
		got, err := st.PreviewAlbum(t.Context(), test.rule, 0.3)
		if !errors.Is(err, test.err) || got.Count != 0 || len(got.Items) != 0 {
			t.Fatalf("rule %+v: %+v, %v; want %v and no data", test.rule, got, err, test.err)
		}
	}
	for _, target := range []struct {
		kind core.BlockKind
		id   int64
		rule core.AlbumRule
	}{{core.BlockTag, tag, core.AlbumRule{Tag: tag}}, {core.BlockPerson, 1, core.AlbumRule{Person: 1}}} {
		_, err := st.SetBlock(t.Context(), target.kind, target.id, "", core.BlockLabel)
		check(t, err)
		got, err := st.PreviewAlbum(t.Context(), target.rule, 0.3)
		if !errors.Is(err, ErrNotFound) || got.Count != 0 || len(got.Items) != 0 {
			t.Fatalf("blocked rule %+v: %+v, %v", target.rule, got, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := st.PreviewAlbum(ctx, core.AlbumRule{}, 0.3); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled preview = %v", err)
	}
}

func TestSmartAlbumPreviewBoundsItsPayloadAcrossOneHundredDays(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	_, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	check(t, st.tx(ctx, func(tx *sql.Tx) error {
		for _, q := range []string{
			`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x < 1000)
			INSERT INTO assets(id, content_hash, kind, ext, taken_at, added_at, width, height, thumb_version)
			SELECT x, printf('%016x', x), 'photo', '.jpg', 1704067200 + ((x-1)/10)*86400, 0, 400, 300, 1 FROM n`,
			`INSERT INTO files(asset_id, source_id, path, size, mtime)
			SELECT id, 1, 'C:\Photos\' || id || '.jpg', 1, 0 FROM assets`,
		} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}))
	got, err := st.PreviewAlbum(ctx, core.AlbumRule{Kind: core.KindPhoto}, 0.3)
	check(t, err)
	body, err := json.Marshal(got)
	check(t, err)
	if got.Count != 1000 || len(got.Items) != core.AlbumPreviewSize || len(body) > 1024 {
		t.Fatalf("unbounded or inaccurate preview: count=%d, samples=%d, bytes=%d", got.Count, len(got.Items), len(body))
	}
	for i, item := range got.Items {
		if item.ID != int64(1000-i) {
			t.Fatalf("sample order = %+v", got.Items)
		}
	}
}

func TestSmartAlbumPreviewKeepsTheCountSnapshotWhileFavouritesChange(t *testing.T) {
	st, _, _ := previewLibrary(t)
	ctx := t.Context()
	st.r.SetMaxOpenConns(1)
	conn, err := st.r.Conn(ctx)
	check(t, err)
	defer func() { _ = conn.Close() }()
	var changed sync.Once
	var changeErr error
	check(t, conn.Raw(func(raw any) error {
		sqlite, ok := raw.(*sqlite3.SQLiteConn)
		if !ok {
			return errors.New("unexpected SQLite connection")
		}
		return sqlite.RegisterFunc("read_preview_favourite", func(value int64) (int64, error) {
			changed.Do(func() {
				changeErr = st.tx(ctx, func(tx *sql.Tx) error {
					_, err := tx.ExecContext(ctx, "UPDATE assets SET favourite = 0")
					return err
				})
			})
			return value, changeErr
		}, false)
	}))
	columns, err := queryAll(ctx, conn, func(rows *sql.Rows) (string, error) {
		var column string
		err := rows.Scan(&column)
		return column, err
	}, "SELECT name FROM pragma_table_info('assets') ORDER BY cid")
	check(t, err)
	for i, column := range columns {
		columns[i] = `"` + strings.ReplaceAll(column, `"`, `""`) + `"`
		if column == "favourite" {
			columns[i] = "read_preview_favourite(favourite) AS favourite"
		}
	}
	// The writer commits during the count; the following sample must retain
	// its earlier snapshot. This follows the metadata snapshot regression's
	// connection-local view seam, without a production test hook.
	_, err = conn.ExecContext(ctx, "PRAGMA query_only = OFF; CREATE TEMP VIEW assets AS SELECT "+
		strings.Join(columns, ", ")+" FROM main.assets; PRAGMA query_only = ON;")
	check(t, err)
	check(t, conn.Close())
	got, err := st.PreviewAlbum(ctx, core.AlbumRule{Favourites: true}, 0.3)
	check(t, err)
	check(t, changeErr)
	if got.Count != 8 || len(got.Items) != core.AlbumPreviewSize {
		t.Fatalf("mixed count/sample snapshots: %+v", got)
	}
	for _, item := range got.Items {
		if !item.Favourite {
			t.Fatalf("sample lost the count's favourite state: %+v", got)
		}
	}
	after, err := st.PreviewAlbum(ctx, core.AlbumRule{Favourites: true}, 0.3)
	check(t, err)
	if after.Count != 0 || len(after.Items) != 0 {
		t.Fatalf("concurrent edit did not commit: %+v", after)
	}
}
