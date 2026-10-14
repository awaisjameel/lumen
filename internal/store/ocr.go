package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"

	"lumen/internal/core"
)

// The text in photos (OCR) is read only for photos the AI took for a
// screenshot or a document: the work is slow (half a second a photo on the
// CPU), and a landscape has no text worth finding. Which they are comes from
// the built-in kinds' tags, so it follows the user's tag confidence like
// everything that reads tags.

// ocrKindTags selects the tags that make a photo worth reading: the
// built-in Screenshots and Documents kinds (search/vocabulary.go). It is the
// one place that names them, by their vocabulary names, which stay the same
// when the user renames the Thing.
const ocrKindTags = `SELECT id FROM tags WHERE kind = 'builtin' AND vocab_name IN ('Screenshots', 'Documents')`

// ocrTagged is the condition for the asset_tags row ocr_x to make its photo
// worth reading: one of ocrKindTags at the user's tag confidence.
// tagConfidenceSQL, not an argument, so the setting is the one the
// transaction sees.
var ocrTagged = "ocr_x.tag_id IN (" + ocrKindTags + ") AND " + tagAppliesTo("ocr_x", tagConfidenceSQL)

// ocrEligible is the condition for the assets row alias to be read: a photo
// with a file on disk (a hidden, trashed or blocked one too, as every stage
// does, so it is ready when it comes back) that carries a tag ocrTagged
// counts. It tests the assets one by one, for a few named by id;
// ocrEligibleAll is the same condition for the whole library.
func ocrEligible(asset string) string {
	return ocrPhoto(asset) + " AND EXISTS (SELECT 1 FROM asset_tags ocr_x WHERE ocr_x.asset_id = " + asset + ".id AND " + ocrTagged + ")"
}

// ocrEligibleAll is ocrEligible starting from the two tags' index entries
// (asset_tags_by_tag), a few thousand rows, rather than testing every photo
// of the library: a tenth of the time at 100,000 photos.
func ocrEligibleAll(asset string) string {
	return ocrPhoto(asset) + " AND " + asset + ".id IN (SELECT ocr_x.asset_id FROM asset_tags ocr_x WHERE " + ocrTagged + ")"
}

// ocrPhoto is the part of ocrEligible that reads only the assets row alias.
func ocrPhoto(asset string) string {
	return asset + ".kind = 'photo' AND " + asset + "." + present
}

// SetOCR turns reading text on or off. Turning it on queues every eligible
// photo that has no OCR job yet (a finished one stands, unless its version
// is out of date, which RequeueVersions handles); turning it off removes the
// jobs still to do. Text already read stays, and stays searchable. The
// setting and the queue change in one transaction, so a failure leaves
// neither; the settings cache follows only once it has committed.
func (s *Store) SetOCR(ctx context.Context, on bool) error {
	value := "0"
	if on {
		value = "1"
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value",
			core.SettingOCR, value); err != nil {
			return err
		}
		if !on {
			_, err := tx.ExecContext(ctx, "DELETE FROM asset_jobs WHERE stage = ? AND status IN "+unfinished, core.StageOCR)
			return err
		}
		_, err := enqueueOCR(ctx, tx, nil)
		return err
	})
	if err == nil {
		s.settings.Store(core.SettingOCR, value)
	}
	return err
}

// KeepEligibleOCR returns the fetched OCR jobs among items whose photos are
// still worth reading, and removes the others: a photo re-embedded as nature,
// whose Screenshots tag the user rejected, or that a raised tag confidence no
// longer counts has nothing to read for. The job goes rather than ending
// skipped, so the photo is queued again if it becomes eligible again.
func (s *Store) KeepEligibleOCR(ctx context.Context, items []JobItem) ([]JobItem, error) {
	if len(items) == 0 {
		return items, nil
	}
	ids := make([]int64, len(items))
	for i, it := range items {
		ids[i] = it.AssetID
	}
	list, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	dropped := map[int64]bool{}
	err = s.tx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `DELETE FROM asset_jobs WHERE stage = ?1 AND status = 'pending'
			AND asset_id IN (SELECT value FROM json_each(?2))
			AND NOT EXISTS (SELECT 1 FROM assets a WHERE a.id = asset_jobs.asset_id AND `+ocrEligible("a")+`)
			RETURNING asset_id`, core.StageOCR, string(list))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return err
			}
			dropped[id] = true
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	kept := make([]JobItem, 0, len(items))
	for _, it := range items {
		if !dropped[it.AssetID] {
			kept = append(kept, it)
		}
	}
	return kept, nil
}

// QueueOCR queues the eligible photos that have no OCR job while reading
// text is on: what SetOCR does on turning it on, for the photos that became
// eligible while nothing was watching (a changed tag confidence, a tag the
// user added). It reports how many it queued.
func (s *Store) QueueOCR(ctx context.Context) (n int64, err error) {
	err = s.tx(ctx, func(tx *sql.Tx) error {
		on, err := ocrOn(ctx, tx)
		if err != nil || !on {
			return err
		}
		n, err = enqueueOCR(ctx, tx, nil)
		return err
	})
	return n, err
}

// ocrOn reads whether reading text is on, in tx.
func ocrOn(ctx context.Context, tx *sql.Tx) (bool, error) {
	var v string
	err := tx.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", core.SettingOCR).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return v == "1", err
}

// queueOCR queues the photos among ids that are eligible, while reading text
// is on, for a save of tags in tx: the photos its tags just made eligible
// are queued in the same transaction, so none is missed between the save and
// a later look.
func queueOCR(ctx context.Context, tx *sql.Tx, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	on, err := ocrOn(ctx, tx)
	if err != nil || !on {
		return err
	}
	_, err = enqueueOCR(ctx, tx, ids)
	return err
}

// enqueueOCR gives every eligible photo that has no OCR job one, waiting or
// pending as its earlier stages allow: among ids, or all of them if ids is
// nil. It returns how many it queued.
func enqueueOCR(ctx context.Context, tx *sql.Tx, ids []int64) (int64, error) {
	q := `INSERT OR IGNORE INTO asset_jobs (asset_id, stage, status, updated_at, taken_at, kind)
		SELECT a.id, ?1, 'waiting', ?2, a.taken_at, a.kind FROM assets a WHERE `
	args := []any{core.StageOCR, time.Now().Unix()}
	settleWhere, settleArgs := "stage = ?", []any{core.StageOCR}
	eligible := ocrEligibleAll("a")
	if ids != nil {
		list, err := json.Marshal(ids)
		if err != nil {
			return 0, err
		}
		q += "a.id IN (SELECT value FROM json_each(?3)) AND "
		args = append(args, string(list))
		settleWhere += " AND asset_id IN (SELECT value FROM json_each(?))"
		settleArgs = append(settleArgs, string(list))
		eligible = ocrEligible("a")
	}
	res, err := tx.ExecContext(ctx, q+eligible, args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return 0, nil
	}
	// The new jobs wait for the stages before them (see waitsFor).
	return n, settle(ctx, tx, settleWhere, settleArgs...)
}

// OCRResult is the outcome of the OCR stage for one asset.
type OCRResult struct {
	AssetID int64
	Text    string // the lines read, one per line; "" if there was none
	Err     string
	Skipped bool
}

// SaveOCR stores a batch of text and closes its jobs at version, in one
// transaction. A photo with no text has none stored, which also removes what
// an older version read.
func (s *Store) SaveOCR(ctx context.Context, version string, rs []OCRResult) error {
	if len(rs) == 0 {
		return nil
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		now := time.Now().Unix()
		// An asset deleted since the job was fetched is skipped, not a
		// failure of the batch.
		put, err := tx.PrepareContext(ctx, `INSERT INTO ocr_text (asset_id, model, text)
			SELECT id, ?2, ?3 FROM assets WHERE id = ?1
			ON CONFLICT (asset_id) DO UPDATE SET model = excluded.model, text = excluded.text`)
		if err != nil {
			return err
		}
		defer put.Close()
		drop, err := tx.PrepareContext(ctx, "DELETE FROM ocr_text WHERE asset_id = ?")
		if err != nil {
			return err
		}
		defer drop.Close()
		job, err := prepareJobUpdate(ctx, tx)
		if err != nil {
			return err
		}
		defer job.Close()
		for _, r := range rs {
			if r.Err == "" && !r.Skipped {
				if r.Text == "" {
					_, err = drop.ExecContext(ctx, r.AssetID)
				} else {
					_, err = put.ExecContext(ctx, r.AssetID, version, r.Text)
				}
				if err != nil {
					return err
				}
			}
			if err := execJob(ctx, job, r.AssetID, core.StageOCR, version, jobStatus(r.Err, r.Skipped), r.Err, now); err != nil {
				return err
			}
		}
		return nil
	})
}

// TextMatches returns up to limit visible assets whose text holds every word
// of query, each as a prefix (so a search finds a word while it is still
// being typed), newest first. Matching goes through visible, so text
// of a trashed, hidden or blocked photo is never returned.
func (s *Store) TextMatches(ctx context.Context, query string, limit int) ([]int64, error) {
	return s.textMatches(ctx, query, limit, nil)
}

// TextMatchesAmong is TextMatches among the assets in among only (a named
// person's photos): they are chosen before the limit, so an older photo of
// them is not lost behind newer matches of other photos.
func (s *Store) TextMatchesAmong(ctx context.Context, query string, limit int, among []int64) ([]int64, error) {
	if len(among) == 0 {
		return []int64{}, nil
	}
	return s.textMatches(ctx, query, limit, among)
}

func (s *Store) textMatches(ctx context.Context, query string, limit int, among []int64) ([]int64, error) {
	match := textQuery(query)
	if match == "" || limit <= 0 {
		return []int64{}, nil
	}
	// The matching documents are looked up by id, rather than the assets
	// walked for a match.
	q := `SELECT a.id FROM (SELECT docid FROM ocr_fts WHERE ocr_fts MATCH ?) m
		CROSS JOIN assets a ON a.id = m.docid WHERE ` + visibleA
	args := []any{match}
	if among != nil {
		list, err := json.Marshal(among)
		if err != nil {
			return nil, err
		}
		q += " AND a.id IN (SELECT value FROM json_each(?))"
		args = append(args, string(list))
	}
	return queryAll(ctx, s.r, scanID, q+" ORDER BY a.taken_at DESC, a.id DESC LIMIT ?", append(args, limit)...)
}

// textQuery is an FTS4 query for photos whose text holds every word of
// query, each a prefix: "" if it has none. Every word is quoted, so none can
// be taken for an operator (OR, NEAR, a parenthesis); a word is a run of
// letters, digits and marks, as the tokenizer sees it.
func textQuery(query string) string {
	words := strings.FieldsFunc(query, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsMark(r)
	})
	for i, w := range words {
		words[i] = `"` + w + `*"`
	}
	return strings.Join(words, " ")
}
