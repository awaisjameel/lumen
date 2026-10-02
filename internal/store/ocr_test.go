package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"lumen/internal/core"
)

// ocrLibrary is a store with the built-in kinds that OCR cares about and one
// that it doesn't, and one source to add photos to.
type ocrLibrary struct {
	st                  *Store
	src                 core.Source
	shots, docs, nature int64 // tag ids
	nextTime            int64
}

func newOCRLibrary(t *testing.T) *ocrLibrary {
	t.Helper()
	st, ctx := openTest(t), t.Context()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	check(t, st.SyncVocabulary(ctx, []VocabLabel{
		{Name: "Screenshots", Prompts: []string{"a screenshot"}, Exclusive: true},
		{Name: "Documents", Prompts: []string{"a document"}, Exclusive: true},
		{Name: "Nature", Prompts: []string{"nature"}, Exclusive: true},
	}))
	l := &ocrLibrary{st: st, src: src, nextTime: 1_700_000_000}
	for name, id := range map[string]*int64{"Screenshots": &l.shots, "Documents": &l.docs, "Nature": &l.nature} {
		check(t, st.r.QueryRowContext(ctx, "SELECT id FROM tags WHERE name = ?", name).Scan(id))
	}
	return l
}

// add ingests a photo (or a video) with every earlier stage finished, so what
// the test does decides what is runnable. Photos added later are newer.
func (l *ocrLibrary) add(t *testing.T, name string, kind core.Kind) int64 {
	t.Helper()
	it := item(l.src.ID, `C:\Photos\`+name+".jpg", name)
	it.Kind = kind
	l.nextTime += 10
	it.TakenAt = l.nextTime
	check(t, errOf(l.st.Ingest(t.Context(), []IngestItem{it})))
	var id int64
	check(t, l.st.r.QueryRowContext(t.Context(), "SELECT id FROM assets WHERE content_hash = ?", []byte(name)).Scan(&id))
	check(t, errOf(l.st.w.ExecContext(t.Context(), `UPDATE asset_jobs SET status = 'done', model_version = 'v'
		WHERE asset_id = ? AND stage IN ('metadata', 'thumbnail', 'embedding')`, id)))
	return id
}

func (l *ocrLibrary) photo(t *testing.T, name string) int64 {
	t.Helper()
	return l.add(t, name, core.KindPhoto)
}

// tag gives an asset an AI tag with a score, or a user tag (score < 0).
func (l *ocrLibrary) tag(t *testing.T, id, tag int64, score float64) {
	t.Helper()
	if score < 0 {
		check(t, errOf(l.st.w.ExecContext(t.Context(), "INSERT INTO asset_tags (asset_id, tag_id, source) VALUES (?, ?, 'user')", id, tag)))
		return
	}
	check(t, errOf(l.st.w.ExecContext(t.Context(), "INSERT INTO asset_tags (asset_id, tag_id, source, score) VALUES (?, ?, 'ai', ?)", id, tag, score)))
}

// ocrJobs lists the assets that have an OCR job, with its status.
func ocrJobs(t *testing.T, st *Store) map[int64]core.JobStatus {
	t.Helper()
	rows, err := st.r.QueryContext(t.Context(), "SELECT asset_id, status FROM asset_jobs WHERE stage = 'ocr'")
	check(t, err)
	defer rows.Close()
	jobs := map[int64]core.JobStatus{}
	for rows.Next() {
		var id int64
		var status core.JobStatus
		check(t, rows.Scan(&id, &status))
		jobs[id] = status
	}
	check(t, rows.Err())
	return jobs
}

func ocrJobIDs(t *testing.T, st *Store) []int64 {
	t.Helper()
	var ids []int64
	for id := range ocrJobs(t, st) {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

func saveText(t *testing.T, st *Store, id int64, text string) {
	t.Helper()
	check(t, st.SaveOCR(t.Context(), "ocr-1", []OCRResult{{AssetID: id, Text: text}}))
}

func matches(t *testing.T, st *Store, query string) []int64 {
	t.Helper()
	ids, err := st.TextMatches(t.Context(), query, 100)
	check(t, err)
	return ids
}

// checkFTS asks SQLite whether the text index agrees with its content table.
func checkFTS(t *testing.T, st *Store) {
	t.Helper()
	check(t, errOf(st.w.ExecContext(t.Context(), "INSERT INTO ocr_fts (ocr_fts) VALUES ('integrity-check')")))
}

func TestReadingTextIsOffUntilTheUserTurnsItOn(t *testing.T) {
	l := newOCRLibrary(t)
	st, ctx := l.st, t.Context()
	if on, err := st.Bool(ctx, core.SettingOCR, false); err != nil || on {
		t.Fatalf("OCR setting on a new library = %v, %v; want off", on, err)
	}
	shot := l.photo(t, "shot")
	l.tag(t, shot, l.shots, 0.9)
	// Tags written by the AI while it is off queue nothing.
	check(t, st.SaveEmbeddingVersions(ctx, "m", StageVersion{Photo: "v", Video: "v"},
		[]EmbedResult{{AssetID: shot, Vec: []int8{1}, Scale: 1, Tags: []TagScore{{TagID: l.shots, Score: 0.9}}}}))
	if n, err := st.QueueOCR(ctx); err != nil || n != 0 {
		t.Fatalf("QueueOCR while off queued %d, %v", n, err)
	}
	if ids := ocrJobIDs(t, st); len(ids) != 0 {
		t.Fatalf("OCR jobs while off: %v", ids)
	}
}

func TestTurningOCROnQueuesOnlyScreenshotsAndDocuments(t *testing.T) {
	l := newOCRLibrary(t)
	st, ctx := l.st, t.Context()
	shot, doc := l.photo(t, "shot"), l.photo(t, "doc")
	userDoc, weak, nature := l.photo(t, "user-doc"), l.photo(t, "weak"), l.photo(t, "nature")
	video, missing, both := l.add(t, "video", core.KindVideo), l.photo(t, "missing"), l.photo(t, "both")
	l.tag(t, shot, l.shots, 0.5)
	l.tag(t, doc, l.docs, 0.3)
	l.tag(t, userDoc, l.docs, -1) // the user's own tag has no score and always applies
	l.tag(t, weak, l.shots, 0.001)
	l.tag(t, nature, l.nature, 0.9)
	l.tag(t, video, l.shots, 0.9)
	l.tag(t, missing, l.shots, 0.9)
	l.tag(t, both, l.shots, 0.4)
	l.tag(t, both, l.docs, 0.4)
	check(t, errOf(st.w.ExecContext(ctx, "UPDATE assets SET missing_since = 1 WHERE id = ?", missing)))

	check(t, st.SetOCR(ctx, true))
	if on, err := st.Bool(ctx, core.SettingOCR, false); err != nil || !on {
		t.Fatalf("setting after SetOCR(true) = %v, %v", on, err)
	}
	want := []int64{shot, doc, userDoc, both}
	slices.Sort(want)
	if got := ocrJobIDs(t, st); !slices.Equal(got, want) {
		t.Fatalf("OCR jobs = %v, want %v (a screenshot, a document, the user's document and the photo that is both, once)", got, want)
	}
	for id, status := range ocrJobs(t, st) {
		if status != core.JobPending { // every earlier stage is done
			t.Errorf("job of %d is %s, want pending", id, status)
		}
	}
	// A photo whose tag is below the user's confidence is eligible when the
	// user lowers it, for QueueOCR to find.
	check(t, st.SetFloat(ctx, core.SettingTagConfidence, 0.0005))
	if n, err := st.QueueOCR(ctx); err != nil || n != 1 {
		t.Fatalf("QueueOCR after lowering the confidence queued %d, %v; want the weak screenshot", n, err)
	}
	if _, queued := ocrJobs(t, st)[weak]; !queued {
		t.Error("the weak screenshot has no job")
	}
	// Turning it on again changes nothing.
	check(t, st.SetOCR(ctx, true))
	if n, err := st.QueueOCR(ctx); err != nil || n != 0 {
		t.Fatalf("QueueOCR with every photo already queued gave %d, %v", n, err)
	}
}

func TestAnOCRJobWaitsForTheEmbeddingThatTagsThePhoto(t *testing.T) {
	l := newOCRLibrary(t)
	st, ctx := l.st, t.Context()
	shot := l.photo(t, "shot")
	l.tag(t, shot, l.shots, 0.5)
	check(t, errOf(st.w.ExecContext(ctx, "UPDATE asset_jobs SET status = 'pending' WHERE asset_id = ? AND stage = 'embedding'", shot)))
	check(t, st.SetOCR(ctx, true))
	if s := ocrJobs(t, st)[shot]; s != core.JobWaiting {
		t.Fatalf("OCR job before the embedding finished is %s, want waiting", s)
	}
	if got, err := st.PendingJobs(ctx, core.StageOCR, nil, 10); err != nil || len(got) != 0 {
		t.Fatalf("OCR ran before the embedding: %v, %v", got, err)
	}
	check(t, st.SaveEmbeddingVersions(ctx, "m", StageVersion{Photo: "v", Video: "v"},
		[]EmbedResult{{AssetID: shot, Vec: []int8{1}, Scale: 1, Tags: []TagScore{{TagID: l.shots, Score: 0.5}}}}))
	got, err := st.PendingJobs(ctx, core.StageOCR, nil, 10)
	check(t, err)
	if len(got) != 1 || got[0].AssetID != shot || got[0].Path != `C:\Photos\shot.jpg` {
		t.Fatalf("OCR jobs once the embedding is done = %+v", got)
	}
}

func TestAnUpgradeOfTheOtherStagesWaitsOutForOCR(t *testing.T) {
	l := newOCRLibrary(t)
	st, ctx := l.st, t.Context()
	shot := l.photo(t, "shot")
	l.tag(t, shot, l.shots, 0.5)
	check(t, st.SetOCR(ctx, true))
	check(t, st.SaveOCR(ctx, "ocr-1", []OCRResult{{AssetID: shot, Text: "hello"}}))
	// Re-running metadata holds the OCR job back, as it does faces.
	check(t, errOf(st.RequeueOutdated(ctx, map[core.Stage]string{core.StageMetadata: "m2"})))
	if got, err := st.PendingJobs(ctx, core.StageOCR, nil, 10); err != nil || len(got) != 0 {
		t.Fatalf("OCR ran while metadata was being redone: %v, %v", got, err)
	}
}

func TestAnOrientationChangeRedoesTheTextOfThePhoto(t *testing.T) {
	l := newOCRLibrary(t)
	st, ctx := l.st, t.Context()
	shot := l.photo(t, "shot")
	l.tag(t, shot, l.shots, 0.5)
	check(t, st.SetOCR(ctx, true))
	check(t, st.SaveOCR(ctx, "ocr-1", []OCRResult{{AssetID: shot, Text: "sideways"}}))
	check(t, st.SaveMetadata(ctx, "m", []MetadataResult{{AssetID: shot, Orientation: 6}}))
	// It is back in the queue, behind the embedding that is redone too.
	if s := ocrJobs(t, st)[shot]; s != core.JobWaiting {
		t.Fatalf("OCR job after the photo turned out to be sideways is %s, want waiting", s)
	}
	// The text that was read stays searchable until it is read again.
	if got := matches(t, st, "sideways"); len(got) != 1 {
		t.Fatalf("text before it is read again = %v", got)
	}
}

func TestOnlyTheOCRStageIsRequeuedByANewOCRVersion(t *testing.T) {
	l := newOCRLibrary(t)
	st, ctx := l.st, t.Context()
	shot := l.photo(t, "shot")
	l.tag(t, shot, l.shots, 0.5)
	check(t, st.SetOCR(ctx, true))
	check(t, st.SaveOCR(ctx, "ocr-1", []OCRResult{{AssetID: shot, Text: "first"}}))
	n, err := st.RequeueVersions(ctx, map[core.Stage]StageVersion{
		core.StageOCR:       {Photo: "ocr-2", Video: "ocr-2"},
		core.StageEmbedding: {Photo: "v", Video: "v"},
	})
	check(t, err)
	if n != 1 {
		t.Fatalf("requeued %d jobs, want only the OCR one", n)
	}
	if s := ocrJobs(t, st)[shot]; s != core.JobPending {
		t.Fatalf("OCR job after a new version is %s, want pending", s)
	}
	if s, _ := readJob(t, st, shot, core.StageEmbedding); s != core.JobDone {
		t.Fatalf("embedding job is %s; an OCR upgrade must not redo it", s)
	}
	// The stored text stays until the new version replaces it.
	if got := matches(t, st, "first"); len(got) != 1 {
		t.Fatalf("text during the upgrade = %v", got)
	}
}

func TestTurningOCROffRemovesUnfinishedJobsAndKeepsTheText(t *testing.T) {
	l := newOCRLibrary(t)
	st, ctx := l.st, t.Context()
	read, queued := l.photo(t, "read"), l.photo(t, "queued")
	l.tag(t, read, l.shots, 0.5)
	l.tag(t, queued, l.docs, 0.5)
	check(t, st.SetOCR(ctx, true))
	saveText(t, st, read, "quarterly report")

	check(t, st.SetOCR(ctx, false))
	jobs := ocrJobs(t, st)
	if _, ok := jobs[queued]; ok {
		t.Error("the unfinished job survived turning OCR off")
	}
	if jobs[read] != core.JobDone {
		t.Errorf("the finished job is %s, want done", jobs[read])
	}
	if got := matches(t, st, "quarterly"); !slices.Equal(got, []int64{read}) {
		t.Errorf("stored text after turning OCR off = %v, want it searchable", got)
	}
	if on, _ := st.Bool(ctx, core.SettingOCR, true); on {
		t.Error("the setting is still on")
	}
	// New tags while it is off queue nothing; turning it on queues what is
	// missing, but not what was read.
	check(t, st.SaveEmbeddingVersions(ctx, "m", StageVersion{Photo: "v", Video: "v"},
		[]EmbedResult{{AssetID: queued, Vec: []int8{1}, Scale: 1, Tags: []TagScore{{TagID: l.docs, Score: 0.5}}}}))
	if _, ok := ocrJobs(t, st)[queued]; ok {
		t.Error("a save while OCR was off queued a job")
	}
	check(t, st.SetOCR(ctx, true))
	if got := ocrJobs(t, st); len(got) != 2 || got[read] != core.JobDone || got[queued] != core.JobPending {
		t.Errorf("jobs after turning it on again = %v", got)
	}
}

func TestAnEmbeddingThatMakesAPhotoAScreenshotQueuesItsText(t *testing.T) {
	l := newOCRLibrary(t)
	st, ctx := l.st, t.Context()
	check(t, st.SetOCR(ctx, true))
	shot, landscape, weak := l.photo(t, "shot"), l.photo(t, "landscape"), l.photo(t, "weak")
	video := l.add(t, "video", core.KindVideo)
	// The embedding is what tags them; its jobs are closed by the same save.
	check(t, errOf(st.w.ExecContext(ctx, "UPDATE asset_jobs SET status = 'pending' WHERE stage = 'embedding'")))
	vec := func(id int64, tag int64, score float32) EmbedResult {
		return EmbedResult{AssetID: id, Vec: []int8{1}, Scale: 1, Tags: []TagScore{{TagID: tag, Score: score}}}
	}
	check(t, st.SaveEmbeddingVersions(ctx, "m", StageVersion{Photo: "v", Video: "v"}, []EmbedResult{
		vec(shot, l.shots, 0.6), vec(landscape, l.nature, 0.9), vec(weak, l.docs, 0.001), vec(video, l.shots, 0.9),
	}))
	jobs := ocrJobs(t, st)
	if len(jobs) != 1 || jobs[shot] != core.JobPending {
		t.Fatalf("OCR jobs after the embeddings = %v, want a pending one for the screenshot only", jobs)
	}
	// Tagging again does not undo a finished job.
	saveText(t, st, shot, "text")
	check(t, st.SaveEmbeddingVersions(ctx, "m", StageVersion{Photo: "v", Video: "v"}, []EmbedResult{vec(shot, l.shots, 0.7)}))
	if s := ocrJobs(t, st)[shot]; s != core.JobDone {
		t.Fatalf("the finished OCR job is %s after another embedding save", s)
	}
}

func TestReTaggingTheLibraryQueuesPhotosThatBecomeDocuments(t *testing.T) {
	l := newOCRLibrary(t)
	st, ctx := l.st, t.Context()
	check(t, st.SetOCR(ctx, true))
	a, b := l.photo(t, "a"), l.photo(t, "b")
	check(t, st.ReplaceAITags(ctx, []AssetTagSet{
		{AssetID: a, Tags: []TagScore{{TagID: l.docs, Score: 0.5}}},
		{AssetID: b, Tags: []TagScore{{TagID: l.nature, Score: 0.5}}},
	}))
	if got := ocrJobIDs(t, st); !slices.Equal(got, []int64{a}) {
		t.Fatalf("OCR jobs after re-tagging = %v, want only %d", got, a)
	}
	// Re-tagging nobody is not "everybody".
	check(t, st.ReplaceAITags(ctx, nil))
	if got := ocrJobIDs(t, st); !slices.Equal(got, []int64{a}) {
		t.Fatalf("OCR jobs after an empty re-tag = %v", got)
	}
}

func TestSavingEmbeddingsOfNothingQueuesNoText(t *testing.T) {
	l := newOCRLibrary(t)
	st, ctx := l.st, t.Context()
	shot := l.photo(t, "shot")
	l.tag(t, shot, l.shots, 0.9)
	check(t, st.SetBool(ctx, core.SettingOCR, true)) // on, but nothing queued yet
	// Every photo of the batch failed: no tags were written, and the
	// photos tagged before are not this batch's business.
	check(t, st.SaveEmbeddingVersions(ctx, "m", StageVersion{Photo: "v", Video: "v"},
		[]EmbedResult{{AssetID: l.photo(t, "bad"), Err: "decode"}}))
	if got := ocrJobIDs(t, st); len(got) != 0 {
		t.Fatalf("OCR jobs = %v, want none", got)
	}
}

func TestSavedTextReplacesTheOldTextAndErrorsKeepIt(t *testing.T) {
	l := newOCRLibrary(t)
	st, ctx := l.st, t.Context()
	a, b := l.photo(t, "a"), l.photo(t, "b")
	l.tag(t, a, l.shots, 0.5)
	l.tag(t, b, l.docs, 0.5)
	check(t, st.SetOCR(ctx, true))
	check(t, st.SaveOCR(ctx, "ocr-1", []OCRResult{{AssetID: a, Text: "alpha beta"}, {AssetID: b, Err: "unreadable"}}))
	if s, attempts := ocrJobStatus(t, st, b); s != core.JobError || attempts != 1 {
		t.Fatalf("failed job = %s after %d attempts", s, attempts)
	}
	var model string
	check(t, st.r.QueryRowContext(ctx, "SELECT model_version FROM asset_jobs WHERE asset_id = ? AND stage = 'ocr'", a).Scan(&model))
	if model != "ocr-1" || ocrJobs(t, st)[a] != core.JobDone {
		t.Fatalf("finished job has version %q, status %s", model, ocrJobs(t, st)[a])
	}
	if got := matches(t, st, "alpha"); !slices.Equal(got, []int64{a}) {
		t.Fatalf("alpha = %v", got)
	}
	// Read again with another result: the old words go.
	check(t, st.SaveOCR(ctx, "ocr-2", []OCRResult{{AssetID: a, Text: "gamma"}}))
	if got := matches(t, st, "alpha"); len(got) != 0 {
		t.Errorf("old word still found: %v", got)
	}
	if got := matches(t, st, "gamma"); !slices.Equal(got, []int64{a}) {
		t.Errorf("new word = %v", got)
	}
	// A failed re-run keeps what was read; one that finds no text removes it.
	check(t, st.SaveOCR(ctx, "ocr-3", []OCRResult{{AssetID: a, Err: "busy"}}))
	if got := matches(t, st, "gamma"); len(got) != 1 {
		t.Errorf("text after a failed re-read = %v", got)
	}
	check(t, st.SaveOCR(ctx, "ocr-4", []OCRResult{{AssetID: a}}))
	if got := matches(t, st, "gamma"); len(got) != 0 {
		t.Errorf("text after a re-read that found none = %v", got)
	}
	var n int
	check(t, st.r.QueryRowContext(ctx, "SELECT count(*) FROM ocr_text").Scan(&n))
	if n != 0 {
		t.Errorf("%d text rows left", n)
	}
	// Skipped is closed without text, and an asset deleted since the job was
	// fetched does not fail the batch.
	check(t, st.SaveOCR(ctx, "ocr-5", []OCRResult{{AssetID: b, Skipped: true}, {AssetID: 999, Text: "ghost"}}))
	if s, _ := ocrJobStatus(t, st, b); s != core.JobSkipped {
		t.Errorf("skipped job = %s", s)
	}
	if got := matches(t, st, "ghost"); len(got) != 0 {
		t.Errorf("text of a missing asset = %v", got)
	}
	checkFTS(t, st)
}

func ocrJobStatus(t *testing.T, st *Store, id int64) (status core.JobStatus, attempts int) {
	t.Helper()
	return readJob(t, st, id, core.StageOCR)
}

func TestTextMatchesFindsEveryWordAsAPrefixNewestFirst(t *testing.T) {
	l := newOCRLibrary(t)
	st := l.st
	invoice := l.photo(t, "invoice")
	boarding := l.photo(t, "boarding")
	plumber := l.photo(t, "plumber") // the newest
	saveText(t, st, invoice, "Invoice total 2024\nPaid in Café Zürich")
	saveText(t, st, boarding, "Boarding pass gate B12\nFlight LH123")
	saveText(t, st, plumber, "invoice for the plumber")

	for _, c := range []struct {
		query string
		want  []int64
	}{
		{"invoice", []int64{plumber, invoice}}, // newest first
		{"invoi", []int64{plumber, invoice}},   // a prefix, for a word still being typed
		{"INVOICE", []int64{plumber, invoice}},
		{"invoice total", []int64{invoice}},            // every word
		{"total invoice", []int64{invoice}},            // in any order
		{"inv tot 20", []int64{invoice}},               // each a prefix
		{"cafe zurich", []int64{invoice}},              // diacritics fold, both ways
		{"CAFÉ", []int64{invoice}},                     //
		{"invoice flight", nil},                        // no photo has both
		{"lh123", []int64{boarding}},                   // letters and digits are one word
		{"  invoice,   (plumber)! ", []int64{plumber}}, // punctuation and spaces separate
		{"", nil},
		{"   ", nil},
		{"!!! ???", nil},
		{"nothing like this", nil},
	} {
		got := matches(t, st, c.query)
		if !slices.Equal(got, c.want) && (len(got) != 0 || len(c.want) != 0) {
			t.Errorf("TextMatches(%q) = %v, want %v", c.query, got, c.want)
		}
		if got == nil {
			t.Errorf("TextMatches(%q) is nil; the UI wants []", c.query)
		}
	}
	if got, err := st.TextMatches(t.Context(), "invoice", 1); err != nil || !slices.Equal(got, []int64{plumber}) {
		t.Errorf("TextMatches with a limit of 1 = %v, %v; want the newest", got, err)
	}
	if got, err := st.TextMatches(t.Context(), "invoice", 0); err != nil || len(got) != 0 {
		t.Errorf("TextMatches with a limit of 0 = %v, %v", got, err)
	}
	checkFTS(t, st)
}

func TestTextMatchesTakesNoWordForASearchOperator(t *testing.T) {
	l := newOCRLibrary(t)
	st := l.st
	a := l.photo(t, "a")
	saveText(t, st, a, "fish and chips near the pier")
	// Every word is required, "or" and "not" as prefixes like any other, so
	// none acts as an operator; and nothing the user types is a syntax error.
	for _, c := range []struct {
		query string
		found bool
	}{
		{"fish OR nothing", false}, {"fish AND chips", true}, {"NOT fish", false}, {"fish NEAR chips", true},
		{"fish NEAR/2 chips", false}, {"-fish", true}, {"fish*", true}, {`"fish`, true}, {`fish"`, true}, {"(fish", true},
		{"fish)", true}, {"col:fish", false}, {"text:fish", false}, {"^fish", true}, {"fish chips OR (pier", false},
		{"*", false}, {`""`, false}, {"fish\x00chips", true}, {"'fish'", true}, {"near pier", true}, {"fish or", false},
	} {
		got, err := st.TextMatches(t.Context(), c.query, 10)
		if err != nil {
			t.Errorf("TextMatches(%q): %v", c.query, err)
			continue
		}
		if found := len(got) == 1; found != c.found {
			t.Errorf("TextMatches(%q) = %v, want found = %v", c.query, got, c.found)
		}
	}
}

func TestTextOfPhotosTheUserCannotSeeNeverMatches(t *testing.T) {
	l := newOCRLibrary(t)
	st, ctx := l.st, t.Context()
	shown, blocked, trashed, hidden, missing := l.photo(t, "shown"), l.photo(t, "blocked"), l.photo(t, "trashed"),
		l.photo(t, "hidden"), l.photo(t, "missing")
	folder := l.photo(t, "folder")
	for _, id := range []int64{shown, blocked, trashed, hidden, missing, folder} {
		saveText(t, st, id, "secret passphrase")
	}
	if got := matches(t, st, "passphrase"); len(got) != 6 {
		t.Fatalf("before hiding anything: %v", got)
	}
	blockID, err := st.SetBlock(ctx, core.BlockAsset, blocked, "", core.BlockPhotos)
	check(t, err)
	check(t, errOf(st.w.ExecContext(ctx, "UPDATE assets SET trashed_at = 1 WHERE id = ?", trashed)))
	check(t, errOf(st.w.ExecContext(ctx, "UPDATE assets SET hidden = 1 WHERE id = ?", hidden)))
	check(t, errOf(st.w.ExecContext(ctx, "UPDATE assets SET missing_since = 1 WHERE id = ?", missing)))
	_, err = st.SetBlock(ctx, core.BlockFolder, 0, `C:\Photos\folder.jpg`, core.BlockPhotos)
	check(t, err)
	if got := matches(t, st, "passphrase"); !slices.Equal(got, []int64{shown}) {
		t.Fatalf("text matches with a block, a trashed, a hidden and a missing photo = %v, want only %d", got, shown)
	}
	// What comes back, comes back with its photo.
	check(t, st.RemoveBlock(ctx, blockID))
	check(t, errOf(st.w.ExecContext(ctx, "UPDATE assets SET trashed_at = NULL WHERE id = ?", trashed)))
	if got := matches(t, st, "passphrase"); len(got) != 3 {
		t.Fatalf("after unblocking and restoring: %v, want the shown, the unblocked and the restored", got)
	}
}

func TestTextOfADeletedAssetLeavesTheIndex(t *testing.T) {
	l := newOCRLibrary(t)
	st, ctx := l.st, t.Context()
	a, b := l.photo(t, "a"), l.photo(t, "b")
	saveText(t, st, a, "ephemeral words")
	saveText(t, st, b, "lasting words")
	check(t, errOf(st.w.ExecContext(ctx, "DELETE FROM assets WHERE id = ?", a)))
	var rows int
	check(t, st.r.QueryRowContext(ctx, "SELECT count(*) FROM ocr_text").Scan(&rows))
	var indexed int
	check(t, st.r.QueryRowContext(ctx, "SELECT count(*) FROM ocr_fts WHERE ocr_fts MATCH 'ephemeral'").Scan(&indexed))
	if rows != 1 || indexed != 0 {
		t.Fatalf("after deleting the asset: %d text rows, %d index entries for its words", rows, indexed)
	}
	if got := matches(t, st, "words"); !slices.Equal(got, []int64{b}) {
		t.Fatalf("words = %v", got)
	}
	checkFTS(t, st)
}

func TestProgressCountsOnlyVisiblePhotosWaitingForText(t *testing.T) {
	l := newOCRLibrary(t)
	st, ctx := l.st, t.Context()
	var ids []int64
	for _, name := range []string{"shown", "hidden", "trashed", "blocked", "done"} {
		id := l.photo(t, name)
		l.tag(t, id, l.shots, 0.5)
		ids = append(ids, id)
	}
	check(t, st.SetOCR(ctx, true))
	check(t, errOf(st.w.ExecContext(ctx, "UPDATE assets SET hidden = 1 WHERE id = ?", ids[1])))
	check(t, errOf(st.w.ExecContext(ctx, "UPDATE assets SET trashed_at = 1 WHERE id = ?", ids[2])))
	_, err := st.SetBlock(ctx, core.BlockAsset, ids[3], "", core.BlockPhotos)
	check(t, err)
	saveText(t, st, ids[4], "words")
	pending, _, _, err := st.JobCounts(ctx)
	check(t, err)
	if pending[core.StageOCR] != 1 {
		t.Fatalf("JobCounts: %d photos waiting for text, want the one visible photo", pending[core.StageOCR])
	}
	if n, err := st.CountPending(ctx, core.StageOCR); err != nil || n != 1 {
		t.Fatalf("CountPending = %d, %v; want 1", n, err)
	}
}

func TestTheOCRQueueIsReadFromItsOwnIndex(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	q, args := pendingJobsQuery(core.StageOCR, nil, 8)
	plan, err := queryAll(ctx, st.r, func(r *sql.Rows) (d string, err error) {
		var id, parent, unused int
		err = r.Scan(&id, &parent, &unused, &d)
		return d, err
	}, "EXPLAIN QUERY PLAN "+q, args...)
	check(t, err)
	all := strings.Join(plan, "\n")
	if !strings.Contains(all, "asset_jobs_pending") || strings.Contains(all, "TEMP B-TREE") {
		t.Errorf("the OCR queue is not walked in order:\n%s", all)
	}
}

// seedOCRLibrary adds n photos, every earlier stage done, of which eligible
// carry the Screenshots tag, wait for OCR and (every other one of them, so
// half) already have read text. It is written in SQL: Ingest is for scans.
func seedOCRLibrary(tb testing.TB, st *Store, n, eligible int) {
	tb.Helper()
	ctx := context.Background()
	src, err := st.AddSource(ctx, `C:\Bench`)
	if err != nil {
		tb.Fatal(err)
	}
	if err := st.SyncVocabulary(ctx, []VocabLabel{{Name: "Screenshots", Prompts: []string{"a screenshot"}, Exclusive: true}}); err != nil {
		tb.Fatal(err)
	}
	exec := func(q string, args ...any) {
		tb.Helper()
		if _, err := st.w.ExecContext(ctx, q, args...); err != nil {
			tb.Fatal(err)
		}
	}
	exec(`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM n WHERE x < ?1)
		INSERT INTO assets (id, content_hash, kind, ext, taken_at, added_at)
		SELECT x, printf('%016x', x), 'photo', '.png', 1700000000 + x * 60, x FROM n`, n)
	exec(`INSERT INTO files (asset_id, source_id, path, size, mtime) SELECT id, ?, 'C:\Bench\' || id || '.png', 1, 1 FROM assets`, src.ID)
	exec(`INSERT INTO asset_jobs (asset_id, stage, status, model_version, updated_at, taken_at, kind)
		SELECT a.id, s.value, 'done', 'v', 1, a.taken_at, a.kind FROM assets a, json_each('["metadata","thumbnail","embedding","faces"]') s`)
	// The eligible photos are spread through the library, not its newest.
	step := n / eligible
	exec(`INSERT INTO asset_tags (asset_id, tag_id, source, score)
		SELECT a.id, (SELECT id FROM tags WHERE name = 'Screenshots'), 'ai', 0.5 FROM assets a WHERE a.id % ?1 = 0`, step)
	if err := st.SetOCR(ctx, true); err != nil {
		tb.Fatal(err)
	}
	// Half of them are read already, with the words a screenshot has.
	exec(`UPDATE asset_jobs SET status = 'done', model_version = 'ocr' WHERE stage = 'ocr' AND (asset_id / ?1) % 2 = 0`, step)
	exec(`WITH words(i, w) AS (VALUES (0,'invoice'),(1,'settings'),(2,'battery'),(3,'message'),(4,'wireless'),(5,'storage'),
			(6,'account'),(7,'notification'),(8,'download'),(9,'payment'),(10,'calendar'),(11,'password'))
		INSERT INTO ocr_text (asset_id, model, text)
		SELECT j.asset_id, 'ocr', (SELECT COALESCE(group_concat(w, ' '), '') FROM words WHERE (j.asset_id / ?1 / 2 * (7901 + i * 10) + i * 104729) % 1000 < 170)
			|| ' ' || printf('ref%06d', j.asset_id)
		FROM asset_jobs j WHERE j.stage = 'ocr' AND j.status = 'done'`, step)
}

// BenchmarkOCRPendingJobs measures the OCR stage's batch fetch at 100,000
// assets, 10,000 of them waiting for text.
func BenchmarkOCRPendingJobs(b *testing.B) {
	st, err := Open(filepath.Join(b.TempDir(), "library.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer st.Close()
	seedOCRLibrary(b, st, 100_000, 10_000)
	ctx := context.Background()
	if jobs, err := st.PendingJobs(ctx, core.StageOCR, nil, 8); err != nil || len(jobs) != 8 {
		b.Fatalf("fixture: %d jobs, %v", len(jobs), err)
	}
	b.ResetTimer()
	for range b.N {
		if jobs, err := st.PendingJobs(ctx, core.StageOCR, nil, 8); err != nil || len(jobs) != 8 {
			b.Fatalf("%d jobs, %v", len(jobs), err)
		}
	}
}

// BenchmarkTextMatches measures a text search at 100,000 assets with 10,000
// photos that have text: a word in a few of them, a word in many, and a word
// prefix.
func BenchmarkTextMatches(b *testing.B) {
	st, err := Open(filepath.Join(b.TempDir(), "library.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer st.Close()
	seedOCRLibrary(b, st, 100_000, 20_000) // half of the 20,000 are read: 10,000 with text
	ctx := context.Background()
	for _, c := range []struct{ name, query string }{
		{"rare word", "ref004000"},
		{"one in twelve", "invoice"},
		{"several words", "invoice settings"},
		{"a prefix of many", "not"},
	} {
		b.Run(c.name, func(b *testing.B) {
			hits, err := st.TextMatches(ctx, c.query, 1000)
			if err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			for range b.N {
				if _, err := st.TextMatches(ctx, c.query, 1000); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(len(hits)), "hits")
		})
	}
}
