package index

import (
	"cmp"
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"lumen/internal/ai"
	"lumen/internal/core"
	"lumen/internal/media"
	"lumen/internal/people"
	"lumen/internal/store"
	"lumen/internal/testenv"
)

// lookalikeRates measures a look-alike threshold on people whose faces are
// known: groups[p] holds the unit vectors of person p. A face is covered by a
// block of another face when their cosine similarity is at least from. recall
// is the share of the same person's other faces that a block covers, over all
// ordered pairs of faces of one person; falseMatch is the share of other
// people's faces it covers, over all ordered pairs of faces of two people.
// same and other count the pairs, and cut is the highest threshold that still
// covers at least want of same: the want-th percentile from the bottom of the
// same-person similarities.
func lookalikeRates(groups [][][]float32, from, want float32) (recall, falseMatch float64, same, other int, cut float32) {
	var sames []float32
	var covered, wrong int
	for p, faces := range groups {
		for i, a := range faces {
			for q, others := range groups {
				for j, b := range others {
					if p == q && i == j {
						continue
					}
					sim := ai.Dot(a, b)
					if p == q {
						sames = append(sames, sim)
						if sim >= from {
							covered++
						}
						continue
					}
					other++
					if sim >= from {
						wrong++
					}
				}
			}
		}
	}
	same = len(sames)
	if same == 0 || other == 0 {
		return 0, 0, same, other, 0
	}
	slices.SortFunc(sames, func(a, b float32) int { return cmp.Compare(b, a) }) // highest first
	keep := max(1, int(float32(same)*want))
	return float64(covered) / float64(same), float64(wrong) / float64(other), same, other, sames[keep-1]
}

// falseMatchCeiling is the share of other people's faces a block may cover at
// core.FaceLookalikeMin on the corpus: what was measured, with a little room.
const falseMatchCeiling = 0.02

func TestLookalikeRatesCountPairsOfOnePersonAndOfTwo(t *testing.T) {
	unit := func(deg float32) []float32 {
		// cos 0 = 1, cos 60 = 0.5, cos 90 = 0: a plane is enough.
		switch deg {
		case 0:
			return []float32{1, 0}
		case 60:
			return []float32{0.5, 0.8660254}
		default:
			return []float32{0, 1}
		}
	}
	groups := [][][]float32{
		{unit(0), unit(60)}, // one person whose two faces are exactly at the threshold
		{unit(90)},          // someone else, 90 degrees from the first and 30 from the 60
	}
	recall, falseMatch, same, other, cut := lookalikeRates(groups, 0.5, 1)
	if same != 2 || other != 4 || recall != 1 {
		t.Fatalf("recall = %v over %d same pairs, %d other", recall, same, other)
	}
	// unit(60) against unit(90): cos 30 = 0.866, so that pair, in both
	// directions, is a false match; the two faces at 0 and 90 are not.
	if falseMatch != 0.5 {
		t.Fatalf("false-match rate = %v, want 0.5", falseMatch)
	}
	if cut < 0.49 || cut > 0.51 {
		t.Fatalf("highest threshold covering all = %v, want 0.5", cut)
	}
}

// The look-alike threshold, measured on real photos: index the corpus, take
// the people the indexer finds as labels (they were checked by eye to hold
// one person each), and compare every face with every other. At
// core.FaceLookalikeMin a block must cover at least 95% of the same person's
// other faces (decision 7 leans to hiding more), and the share of other
// people's faces it covers is the price, asserted here as a ceiling so that a
// change of model or threshold has to look at it.
func TestFaceLookalikeThresholdOnTheFaceCorpus(t *testing.T) {
	photos := testenv.FacePhotos(t, 100000)
	rt := testenv.Runtime(t)
	if err := ai.Init(rt); err != nil {
		t.Fatal(err)
	}
	fm, err := ai.LoadFaceModel(filepath.Join(rt, "models", "faces"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, p := range photos {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(p)), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	paths := core.PathsIn(t.TempDir())
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(paths.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := st.SetBool(ctx, core.SettingAIOnBattery, true); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddSource(ctx, dir); err != nil {
		t.Fatal(err)
	}
	engine := people.NewEngine(st, fm)
	ix := New(st, paths, media.NewCache(paths.ThumbDir, media.Decoders{}), Models{People: engine}, &fakeEvents{})
	ix.Start(ctx)
	defer func() {
		cancel()
		ix.Stop()
	}()

	// Wait for every face to be found, then for the people to stop changing.
	deadline := time.Now().Add(20 * time.Minute)
	for ; time.Now().Before(deadline); time.Sleep(time.Second) {
		p := ix.Progress(ctx)
		if !p.Scanning && p.Total == len(photos) && p.Metadata == 0 && p.Thumbnail == 0 && p.Faces == 0 && ix.pendingScans() == 0 {
			break
		}
	}
	if p := ix.Progress(ctx); p.Faces != 0 || p.Total != len(photos) {
		t.Fatalf("faces were not all found: %+v", p)
	}
	var groups [][][]float32
	var ids []int64
	for stable, last := 0, ""; stable < 5 && time.Now().Before(deadline); time.Sleep(time.Second) {
		found, err := st.People(ctx)
		if err != nil {
			t.Fatal(err)
		}
		sig := ""
		ids = ids[:0]
		for _, p := range found {
			sig += string(rune(p.ID)) + ":" + string(rune(p.Count)) + ","
			ids = append(ids, p.ID)
		}
		if sig == last {
			stable++
		} else {
			stable, last = 0, sig
		}
	}
	faces := 0
	for _, id := range ids {
		_, vecs, err := st.PersonFaces(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		faces += len(vecs)
		if len(vecs) > 0 {
			groups = append(groups, vecs)
		}
	}
	recall, falseMatch, same, other, cut := lookalikeRates(groups, core.FaceLookalikeMin, 0.95)
	_, cutFalse, _, _, _ := lookalikeRates(groups, cut, 0.95)
	t.Logf("%d photos, %d people with %d faces: %d same-person pairs, %d pairs of different people", len(photos), len(groups), faces, same, other)
	t.Logf("at core.FaceLookalikeMin %.2f: covers %.2f%% of the same person's other faces, %.3f%% of other people's", core.FaceLookalikeMin, 100*recall, 100*falseMatch)
	t.Logf("the highest threshold covering 95%% of the same person's faces is %.4f, with %.3f%% false matches", cut, 100*cutFalse)
	for _, m := range []float32{0.30, 0.35, 0.38, 0.40, 0.42, 0.44, 0.46, 0.50, 0.55, 0.60} {
		r, f, _, _, _ := lookalikeRates(groups, m, 0.95)
		t.Logf("threshold %.2f: same-person recall %.2f%%, false matches %.3f%%", m, 100*r, 100*f)
	}
	var perPerson []float64
	for _, g := range groups {
		if len(g) < 5 {
			continue
		}
		hit := 0
		for i, a := range g {
			for j, b := range g {
				if i != j && ai.Dot(a, b) >= core.FaceLookalikeMin {
					hit++
				}
			}
		}
		perPerson = append(perPerson, float64(hit)/float64(len(g)*(len(g)-1)))
	}
	slices.Sort(perPerson)
	if len(perPerson) > 0 {
		t.Logf("per person (%d people with 5 or more faces) the share covered is at least %.1f%%, median %.1f%%",
			len(perPerson), 100*perPerson[0], 100*perPerson[len(perPerson)/2])
	}
	if same < 100 || other < 1000 {
		t.Fatalf("the corpus gave too few pairs to measure: %d same, %d other", same, other)
	}
	if recall < 0.95 {
		t.Errorf("a block covers %.2f%% of the same person's other faces, want at least 95%%", 100*recall)
	}
	if falseMatch > falseMatchCeiling {
		t.Errorf("a block covers %.3f%% of other people's faces, want at most %.3f%%", 100*falseMatch, 100*falseMatchCeiling)
	}
}
