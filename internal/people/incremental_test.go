package people

import (
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"lumen/internal/ai"
	"lumen/internal/core"
	"lumen/internal/store"
)

func TestIncrementalEngineMatchesTheFullEngineThroughCorrectionsAndRestarts(t *testing.T) {
	for _, seed := range []uint64{1, 19, 20261013} {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := st.Close(); err != nil {
					t.Error(err)
				}
			})
			ctx := t.Context()
			src, err := st.AddSource(ctx, `C:\Photos`)
			if err != nil {
				t.Fatal(err)
			}
			inc := NewEngine(st, ai.FaceModel{Version: "test"})
			ref := NewEngine(st, inc.model)
			w := newWorld(seed, 8)
			rng := rand.New(rand.NewPCG(seed, 7))
			assets := map[int64][]store.DetectedFace{}
			var next int
			run := func(step int) {
				t.Helper()
				faces, people, rejected, err := st.ClusterData(ctx, ref.model.Version)
				if err != nil {
					t.Fatal(err)
				}
				links, err := ref.links.Update(ctx, faces)
				if err != nil {
					t.Fatal(err)
				}
				want, err := Cluster(ctx, Input{Faces: faces, People: people, Rejections: rejected, Links: links}, ref.params)
				if err != nil {
					t.Fatal(err)
				}
				got, _, err := inc.decide(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(inc.links.near, ref.links.near) || !reflect.DeepEqual(inc.links.vecOf, ref.links.vecOf) {
					t.Fatalf("step %d: retained linker state differs from full", step)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("step %d: incremental = %+v; full = %+v", step, got, want)
				}
				// Compare the aggregates bit for bit before either run can merge them.
				full := newClustering(faces, people, rejected)
				cached := inc.state.clustering(inc.state.looseFaces(), people)
				if len(full.groups) != len(cached.groups) {
					t.Fatal("group counts differ")
				}
				for i, g := range full.groups {
					other := cached.groups[i]
					if !reflect.DeepEqual(g.sum, other.sum) || g.n != other.n || g.named != other.named || !reflect.DeepEqual(g.assets, other.assets) || !reflect.DeepEqual(g.rejects, other.rejects) {
						t.Fatalf("step %d group %d: cached aggregate differs from full", step, i)
					}
				}
				if err := st.ApplyClusters(ctx, want); err != nil {
					t.Fatal(err)
				}
			}
			add := func() {
				t.Helper()
				var items []store.IngestItem
				for range 3 + rng.IntN(7) {
					next++
					items = append(items, store.IngestItem{SourceID: src.ID, Path: fmt.Sprintf(`C:\Photos\%d.jpg`, next), Hash: []byte(fmt.Sprint(next)), Kind: core.KindPhoto, Ext: ".jpg", TakenAt: 1700000000 + int64(next), Size: 10, MTime: 1, DateFrom: core.DateFromFile})
				}
				if _, err := st.Ingest(ctx, items); err != nil {
					t.Fatal(err)
				}
				jobs, err := st.PendingJobs(ctx, core.StageMetadata, nil, 10000)
				if err != nil {
					t.Fatal(err)
				}
				var results []store.FaceResult
				for _, job := range jobs {
					if _, seen := assets[job.AssetID]; seen {
						continue
					}
					var detected []store.DetectedFace
					for k := range 1 + rng.IntN(3) {
						f := w.face(rng.IntN(8), job.AssetID)
						detected = append(detected, store.DetectedFace{X: 0.1 + float32(k)*0.3, Y: 0.1, W: 0.2, H: 0.2, Score: 0.9, Sharpness: f.Sharpness, Vec: f.Vec, Frame: int64(rng.IntN(2)) * 1000})
					}
					assets[job.AssetID] = detected
					results = append(results, store.FaceResult{AssetID: job.AssetID, Faces: detected})
				}
				if err := st.SaveFaces(ctx, inc.model.Version, "stage1", results); err != nil {
					t.Fatal(err)
				}
			}
			add()
			run(-1)
			coverage := map[int]int{}
			for step := range 180 {
				if step%3 == 0 {
					add()
				}
				faces, people, _, err := st.ClusterData(ctx, inc.model.Version)
				if err != nil {
					t.Fatal(err)
				}
				if len(faces) == 0 {
					add()
					run(step)
					continue
				}
				f := faces[rng.IntN(len(faces))]
				op := step % 12
				if (op == 0 || op == 5) && f.Person == 0 {
					for _, placed := range faces {
						if placed.Person != 0 {
							f = placed
							break
						}
					}
				}
				coverage[op]++
				switch op {
				case 0:
					if f.Person != 0 {
						err = st.RejectFaces(ctx, f.Person, []int64{f.ID})
					} else {
						coverage[op]--
					}
				case 1:
					_, err = st.NameFaces(ctx, []int64{f.ID}, "")
				case 2:
					_, err = st.NameFaces(ctx, []int64{f.ID}, "Named person")
				case 3:
					if len(people) > 0 {
						err = st.NamePerson(ctx, people[rng.IntN(len(people))].ID, fmt.Sprintf("Name %d", step))
					} else {
						coverage[op]--
					}
				case 4:
					if len(people) > 1 {
						err = st.MergePeople(ctx, people[0].ID, people[len(people)-1].ID)
					} else {
						coverage[op]--
					}
				case 5:
					if f.Person != 0 {
						block, berr := st.SetBlock(ctx, core.BlockPerson, f.Person, "", core.BlockPhotos)
						if berr != nil {
							t.Fatal(berr)
						}
						run(step)
						err = st.RemoveBlock(ctx, block)
					} else {
						coverage[op]--
					}
				case 6:
					var block int64
					var berr error
					switch (step / 12) % 3 {
					case 0:
						block, berr = st.SetBlock(ctx, core.BlockAsset, f.AssetID, "", core.BlockPhotos)
					case 1:
						block, _, berr = st.SetFaceBlock(ctx, f.ID, core.BlockPhotos, true, true)
					case 2:
						block, berr = st.SetBlock(ctx, core.BlockFolder, 0, src.Path, core.BlockPhotos)
					}
					if berr != nil {
						t.Fatal(berr)
					}
					run(step)
					err = st.RemoveBlock(ctx, block)
				case 7:
					err = st.TrashAssets(ctx, []int64{f.AssetID})
					if err != nil {
						t.Fatal(err)
					}
					run(step)
					err = st.RestoreAssets(ctx, []int64{f.AssetID})
				case 8:
					err = st.SaveFaces(ctx, inc.model.Version, "stage2", []store.FaceResult{{AssetID: f.AssetID}})
					assets[f.AssetID] = nil
				case 9:
					detected := slices.Clone(assets[f.AssetID])
					if len(detected) > 0 {
						detected[0].Vec = w.unit(nil, 0)
						detected[0].Sharpness, detected[0].Yaw = 2, 0.9
						assets[f.AssetID] = detected
						err = st.SaveFaces(ctx, inc.model.Version, "stage2", []store.FaceResult{{AssetID: f.AssetID, Faces: detected}})
					}
				case 10:
					if step%24 == 10 {
						// A library at rest gives the kept state back; the links stay.
						inc.release()
						break
					}
					// Restart both engines: the reference loses precisely the same links.
					inc, ref = NewEngine(st, inc.model), NewEngine(st, ref.model)
				case 11:
					if step%24 == 11 {
						err = st.SaveFaces(ctx, "other", "stage3", []store.FaceResult{{AssetID: f.AssetID, Faces: assets[f.AssetID]}})
					} else {
						inc.model.Version, ref.model.Version = "other", "other"
					}
				}
				if err != nil {
					t.Fatalf("step %d operation %d: %v", step, op, err)
				}
				run(step)
			}
			t.Logf("completed operations: %v", coverage)
			for op := range 12 {
				if coverage[op] == 0 {
					t.Fatalf("operation %d was not exercised", op)
				}
			}
		})
	}
}

func TestIncrementalClusteringReusesUnchangedSumsAndMeanLinks(t *testing.T) {
	w := newWorld(4, 3)
	var faces []store.ClusterFace
	for k := range 3 {
		for _, f := range w.faces(k, 20) {
			f.Person = int64(k + 1)
			faces = append(faces, f)
		}
	}
	state := newClusterState(DefaultParams)
	if err := state.update(t.Context(), store.ClusterSnapshot{Full: true, Faces: faces}); err != nil {
		t.Fatal(err)
	}
	sum, mean := state.people[1].sum, state.people[1].mean
	fresh := w.face(0, 0)
	if err := state.update(t.Context(), store.ClusterSnapshot{Faces: []store.ClusterFace{fresh}}); err != nil {
		t.Fatal(err)
	}
	if &sum[0] != &state.people[1].sum[0] || &mean[0] != &state.people[1].mean[0] {
		t.Fatal("an unplaced arrival rebuilt an unchanged person's aggregate")
	}
	fresh.Person = 1
	if err := state.update(t.Context(), store.ClusterSnapshot{Faces: []store.ClusterFace{fresh}}); err != nil {
		t.Fatal(err)
	}
	if &sum[0] != &state.people[1].sum[0] {
		t.Fatal("an appended placement rebuilt the existing sum")
	}
	want := newClustering(append(faces, fresh), nil, nil).groups[0].sum
	if !reflect.DeepEqual(state.people[1].sum, want) {
		t.Fatal("appended sum differs from the reference")
	}
}
