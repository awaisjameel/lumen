package store

import (
	"errors"
	"fmt"
	"testing"

	"lumen/internal/core"
)

func TestCompressionHistoryPagesAndCountsUseCurrentVisibility(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	source, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	check(t, errOf(st.Ingest(ctx, []IngestItem{item(source.ID, `C:\Photos\one.jpg`, "one")})))
	destination, err := st.AddSource(ctx, `C:\Données`)
	check(t, err)
	check(t, errOf(st.Ingest(ctx, []IngestItem{item(destination.ID, `C:\Données\other.jpg`, "other")})))
	jobs := make([]core.CompressionJob, 205)
	for i := range jobs {
		jobs[i] = core.CompressionJob{AssetID: 1, From: fmt.Sprintf(`C:\Photos\copy-%d.jpg`, i), To: fmt.Sprintf(`C:\Données\Child\copy-%d.jpg`, i), State: core.CompressionPlanned}
	}
	id, err := st.SaveCompressionPlan(ctx, core.DefaultCompressionOptions(), jobs)
	check(t, err)
	for _, offset := range []int{0, 100, 200} {
		page, err := st.CompressionPage(ctx, id, offset)
		check(t, err)
		if len(page.Jobs) != min(100, 205-offset) || page.Total != 205 || page.Offset != offset {
			t.Fatalf("page %+v", page)
		}
	}
	runs, err := st.CompressionRuns(ctx)
	check(t, err)
	if len(runs) != 1 || runs[0].Total != 205 {
		t.Fatalf("counts: %+v", runs)
	}
	_, err = st.SetBlock(ctx, core.BlockFolder, 0, `c:\DONNÉES`, core.BlockPhotos)
	check(t, err)
	page, err := st.CompressionPage(ctx, id, 0)
	check(t, err)
	runs, err = st.CompressionRuns(ctx)
	check(t, err)
	if page.Total != 0 || len(page.Jobs) != 0 || len(runs) != 0 {
		t.Fatal("blocked destination disclosed in history")
	}
}

func TestRelatedCompressionOutputsRetainExplicitAnnotationsAndFaceCorrections(t *testing.T) {
	st, ctx, a, _, _ := peopleFixture(t)
	faceID := facesOf(t, st, a)[0].ID
	person, err := st.NameFaces(ctx, []int64{faceID}, "Ali")
	check(t, err)
	rejected := facesOf(t, st, a)[1].ID
	check(t, st.RejectFaces(ctx, person, []int64{rejected}))
	tag, err := st.CreateTag(ctx, "Trip", core.TagManual, nil)
	check(t, err)
	check(t, st.TagAsset(ctx, a, tag))
	album, err := st.CreateAlbum(ctx, "Keepsakes", core.AlbumManual, core.AlbumRule{}, nil)
	check(t, err)
	check(t, st.SetAlbumItem(ctx, a, album, true))
	check(t, st.SetFavourite(ctx, a, true))
	check(t, st.SetTakenAt(ctx, []int64{a}, 0))
	check(t, st.SetLocation(ctx, []int64{a}, 51.5074, -0.1278))
	id, err := st.SaveCompressionPlan(ctx, core.DefaultCompressionOptions(), []core.CompressionJob{{AssetID: a, From: `C:\Photos\a.jpg`, To: `C:\Photos\a-compressed.jpg`, State: core.CompressionCommitting}})
	check(t, err)
	jobs, err := st.CompressionJobs(ctx, id)
	check(t, err)
	check(t, st.FinishCompression(ctx, jobs[0], []byte("new content"), 1))
	check(t, st.FinishCompression(ctx, jobs[0], []byte("new content"), 1))
	jobs, err = st.CompressionJobs(ctx, id)
	check(t, err)
	output := jobs[0].OutputAssetID
	detail, err := st.AssetDetail(ctx, output, false)
	check(t, err)
	if detail.DateFrom != core.DateFromUser || detail.TakenAt != 0 || detail.OriginalDate == nil || detail.OriginalLocation == nil || detail.Lat == nil || *detail.Lat != 51.5074 {
		t.Fatalf("compression lost corrections: %+v", detail)
	}
	tags, err := st.AssetTags(ctx, output, core.TagConfidenceDefault)
	check(t, err)
	albums, err := st.AssetAlbums(ctx, output)
	check(t, err)
	faces := facesOf(t, st, output)
	if !detail.Favourite || len(tags) != 1 || tags[0].ID != tag || len(albums) != 1 || albums[0] != album || len(faces) != 2 {
		t.Fatalf("annotations: %+v %+v %+v %+v", detail, tags, albums, faces)
	}
	var rejections int
	check(t, st.r.QueryRowContext(ctx, "SELECT count(*) FROM face_rejections r JOIN faces f ON f.id = r.face_id WHERE f.asset_id = ?", output).Scan(&rejections))
	if !faces[0].ByUser || faces[0].Person != person || rejections != 1 {
		t.Fatalf("corrections lost: %+v, rejections=%d", faces, rejections)
	}
}

func TestRelatedCopiesKeepKnownPersonBlocksWhileAwaitingFaceAnalysis(t *testing.T) {
	st, ctx, asset, _, _ := peopleFixture(t)
	face := facesOf(t, st, asset)[0].ID
	person, err := st.NameFaces(ctx, []int64{face}, "Ali")
	check(t, err)
	check(t, errOf(st.w.ExecContext(ctx, "UPDATE faces SET assigned_by = 'auto' WHERE id = ?", face)))
	run, err := st.SaveCompressionPlan(ctx, core.DefaultCompressionOptions(), []core.CompressionJob{{AssetID: asset, From: `C:\Photos\a.jpg`, To: `C:\Photos\related.jpg`, State: core.CompressionCommitting}})
	check(t, err)
	// A block can arrive after publication, before its registration settles.
	_, err = st.SetBlock(ctx, core.BlockPerson, person, "", core.BlockPhotos)
	check(t, err)
	jobs, err := st.CompressionJobs(ctx, run)
	check(t, err)
	check(t, st.FinishCompression(ctx, jobs[0], []byte("related content"), 1))
	jobs, err = st.CompressionJobs(ctx, run)
	check(t, err)
	visible, err := st.AssetVisible(ctx, jobs[0].OutputAssetID, false)
	check(t, err)
	if visible {
		t.Fatal("known blocked person became visible before face analysis")
	}
	var detections, manual int
	check(t, st.r.QueryRowContext(ctx, "SELECT count(*), coalesce(sum(assigned_by = 'user'), 0) FROM faces WHERE asset_id = ?", jobs[0].OutputAssetID).Scan(&detections, &manual))
	if detections != 2 || manual != 0 {
		t.Fatalf("provisional detection ownership: total=%d manual=%d", detections, manual)
	}
}

func TestRelatedCopiesKeepKnownPlaceAndLookalikeBlocksBeforeAnalysis(t *testing.T) {
	for _, kind := range []core.BlockKind{core.BlockPlace, core.BlockFace} {
		t.Run(string(kind), func(t *testing.T) {
			st, ctx, asset, _, _ := peopleFixture(t)
			check(t, errOf(st.w.ExecContext(ctx, "INSERT INTO places(id, name, region, country, lat, lon) VALUES (7, 'Lisbon', '', 'PT', 38.7, -9.1)")))
			check(t, errOf(st.w.ExecContext(ctx, "UPDATE assets SET place_id = 7, place_checked = 1 WHERE id = ?", asset)))
			run, err := st.SaveCompressionPlan(ctx, core.DefaultCompressionOptions(), []core.CompressionJob{{AssetID: asset, From: `C:\Photos\a.jpg`, To: `C:\Photos\related.jpg`, State: core.CompressionCommitting}})
			check(t, err)
			// Publication has happened, but the copy has no derived jobs yet.
			if kind == core.BlockPlace {
				_, err = st.SetBlock(ctx, kind, 7, "", core.BlockPhotos)
			} else {
				_, _, err = st.SetFaceBlock(ctx, facesOf(t, st, asset)[0].ID, core.BlockPhotos, true, true)
			}
			check(t, err)
			jobs, err := st.CompressionJobs(ctx, run)
			check(t, err)
			check(t, st.FinishCompression(ctx, jobs[0], []byte("related content"), 1))
			jobs, err = st.CompressionJobs(ctx, run)
			check(t, err)
			visible, err := st.AssetVisible(ctx, jobs[0].OutputAssetID, false)
			check(t, err)
			if visible {
				t.Fatal("known blocked content became visible while analysis was pending")
			}
		})
	}
}

// The scanner stores a file's extension in lower case; a copy registered at
// publication is the same kind of row, whatever the case of the name it took.
func TestACompressedCopyIsRegisteredWithItsExtensionInLowerCase(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	source, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	check(t, errOf(st.Ingest(ctx, []IngestItem{item(source.ID, `C:\Photos\ONE.JPG`, "one")})))
	run, err := st.SaveCompressionPlan(ctx, core.DefaultCompressionOptions(), []core.CompressionJob{{AssetID: 1, From: `C:\Photos\ONE.JPG`, To: `C:\Photos\ONE-compressed.JPG`, State: core.CompressionCommitting}})
	check(t, err)
	jobs, err := st.CompressionJobs(ctx, run)
	check(t, err)
	check(t, st.FinishCompression(ctx, jobs[0], []byte("smaller content"), 1))
	var scanned, registered string
	check(t, st.r.QueryRowContext(ctx, "SELECT ext FROM assets WHERE id = 1").Scan(&scanned))
	check(t, st.r.QueryRowContext(ctx, "SELECT a.ext FROM assets a JOIN compression_jobs j ON j.output_asset_id = a.id WHERE j.run_id = ?", run).Scan(&registered))
	if registered != scanned || registered != ".jpg" {
		t.Fatalf("extension %q, a scanned file's is %q", registered, scanned)
	}
}

func TestOnlyRunsBeyondHistoryAreExpiredAndNeverOneStillPublishing(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	source, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	check(t, errOf(st.Ingest(ctx, []IngestItem{item(source.ID, `C:\Photos\one.jpg`, "one")})))
	plan := func(state core.CompressionState, temp string) int64 {
		t.Helper()
		run, err := st.SaveCompressionPlan(ctx, core.DefaultCompressionOptions(), []core.CompressionJob{{AssetID: 1, From: `C:\Photos\one.jpg`, To: `C:\Photos\one-compressed.jpg`, State: core.CompressionPlanned}})
		check(t, err)
		jobs, err := st.CompressionJobs(ctx, run)
		check(t, err)
		jobs[0].State, jobs[0].Temp = state, temp
		check(t, st.SetCompressionJob(ctx, jobs[0]))
		return run
	}
	reviewed := plan(core.CompressionReady, `C:\Data\compression\run-1-job-1-a\candidate.jpg`)
	publishing := plan(core.CompressionCommitting, `C:\Data\compression\run-2-job-2-b\candidate.jpg`)
	for range core.CompressionHistoryRuns - 2 {
		plan(core.CompressionPlanned, "")
	}
	expired, err := st.ExpiredCompressionCandidates(ctx)
	check(t, err)
	if len(expired) != 0 {
		t.Fatalf("history still reaches every run, yet these expired: %v", expired)
	}
	newest := plan(core.CompressionReady, `C:\Data\compression\run-9-job-9-c\candidate.jpg`)
	plan(core.CompressionPlanned, "")
	expired, err = st.ExpiredCompressionCandidates(ctx)
	check(t, err)
	if len(expired) != 1 || expired[0] != `C:\Data\compression\run-1-job-1-a\candidate.jpg` {
		t.Fatalf("expired candidates: %v", expired)
	}
	check(t, st.PruneCompressionRuns(ctx))
	if _, err := st.CompressionRun(ctx, reviewed); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a run beyond history was kept: %v", err)
	}
	for _, kept := range []int64{publishing, newest} {
		if _, err := st.CompressionRun(ctx, kept); err != nil {
			t.Fatalf("run %d was pruned: %v", kept, err)
		}
	}
	jobs, err := st.CompressionJobs(ctx, reviewed)
	check(t, err)
	if len(jobs) != 0 {
		t.Fatalf("a pruned run left its outcomes: %+v", jobs)
	}
}
