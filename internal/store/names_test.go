package store

import (
	"database/sql"
	"errors"
	"slices"
	"strings"
	"testing"

	"lumen/internal/core"
)

// Names are one name whatever their case, in any alphabet (SQLite's NOCASE
// knows only A-Z), and have one length limit.
func TestNamesAreUniqueInAnyCase(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	if _, err := st.CreateTag(ctx, "Été", core.TagManual, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateTag(ctx, "été", core.TagManual, nil); !errors.Is(err, ErrTagExists) {
		t.Errorf("second tag été: %v, want ErrTagExists", err)
	}
	if id, _, err := st.TagByName(ctx, " ÉTÉ "); err != nil || id == 0 {
		t.Errorf("TagByName(ÉTÉ) = %d, %v", id, err)
	}
	if got, err := st.SearchTags(ctx, "ÉT", 5); err != nil || len(got) != 1 {
		t.Errorf("SearchTags(ÉT) = %v, %v", got, err)
	}

	if _, err := st.CreateAlbum(ctx, "Иван", core.AlbumManual, core.AlbumRule{}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAlbum(ctx, "иван", core.AlbumManual, core.AlbumRule{}, nil); !errors.Is(err, ErrAlbumExists) {
		t.Errorf("second album иван: %v, want ErrAlbumExists", err)
	}

	// People: two unnamed people exist (NULL names), then one is named.
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	check(t, errOf(st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\a.jpg`, "a")})))
	jobs, err := st.PendingJobs(ctx, core.StageMetadata, nil, 1)
	check(t, err)
	a := jobs[0].AssetID
	check(t, st.SaveFaces(ctx, "f", "f", []FaceResult{{AssetID: a, Faces: []DetectedFace{
		{X: 0.1, Y: 0.1, W: 0.1, H: 0.1, Vec: []float32{1, 0}},
		{X: 0.6, Y: 0.1, W: 0.1, H: 0.1, Vec: []float32{0, 1}},
	}}}))
	faces, err := st.AssetFaces(ctx, a)
	check(t, err)
	one, err := st.NameFaces(ctx, []int64{faces[0].ID}, "")
	check(t, err)
	two, err := st.NameFaces(ctx, []int64{faces[1].ID}, "")
	check(t, err)
	check(t, st.NamePerson(ctx, one, "Émile"))
	if err := st.NamePerson(ctx, two, "ÉMILE"); !errors.Is(err, ErrPersonExists) {
		t.Errorf("naming someone else ÉMILE: %v, want ErrPersonExists", err)
	}
	if id, err := st.PersonByName(ctx, "émile"); err != nil || id != one {
		t.Errorf("PersonByName(émile) = %d, %v; want %d", id, err, one)
	}
	if got, err := st.NameFaces(ctx, []int64{faces[1].ID}, "emile"); err != nil || got == one {
		t.Errorf("emile (no accent) is someone else: got %d, %v", got, err)
	}

	long := strings.Repeat("é", core.MaxNameLen+1)
	if _, err := st.CreateTag(ctx, long, core.TagManual, nil); !errors.Is(err, ErrInvalidName) {
		t.Errorf("a %d-letter tag: %v, want ErrInvalidName", core.MaxNameLen+1, err)
	}
	if err := st.NamePerson(ctx, two, long); !errors.Is(err, ErrInvalidName) {
		t.Errorf("a %d-letter name: %v, want ErrInvalidName", core.MaxNameLen+1, err)
	}
}

// foldName is fold(name) for every name, whichever branch it takes, and
// NULL for an unnamed person.
func TestFoldNameIsFold(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	for _, name := range []string{"Summer", "ÉTÉ", "\u212Aelvin", "\u017Fun", "Иван", ""} {
		var got, want string
		check(t, st.r.QueryRowContext(ctx, "SELECT "+foldName+", fold(name) FROM (SELECT ? AS name)", name).Scan(&got, &want))
		if got != want {
			t.Errorf("foldName(%q) = %q, fold = %q", name, got, want)
		}
	}
	var null sql.NullString
	check(t, st.r.QueryRowContext(ctx, "SELECT "+foldName+" FROM (SELECT NULL AS name)").Scan(&null))
	if null.Valid {
		t.Errorf("foldName(NULL) = %q, want NULL", null.String)
	}
}

// Every list of names sorts as the user reads (core.CompareNames), where
// bytes put "Émile" after "Zoë" and "Album 10" before "Album 2" (and NOCASE,
// which albums used, folds only A-Z). The counts all tie, so only the names
// order each list.
func TestNameListsSortAsTheUserReads(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	want := []string{"Album 2", "Album 10", "Émile", "Zoë"}
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	var items []IngestItem
	for i := range want {
		items = append(items, item(src.ID, `C:\Photos\`+string(rune('a'+i))+".jpg", string(rune('a'+i))))
	}
	check(t, errOf(st.Ingest(ctx, items)))
	jobs, err := st.PendingJobs(ctx, core.StageMetadata, nil, len(want))
	check(t, err)
	ids := make([]int64, len(jobs))
	for i, job := range jobs {
		ids[i] = job.AssetID
	}
	var faces []DetectedFace
	for i := range want {
		faces = append(faces, face(0.05+0.25*float32(i), i))
	}
	check(t, st.SaveFaces(ctx, "f", "f", []FaceResult{{AssetID: ids[0], Faces: faces}}))
	detected := facesOf(t, st, ids[0])
	for i := len(want) - 1; i >= 0; i-- {
		name := want[i]
		check(t, errOf(st.CreateAlbum(ctx, name, core.AlbumManual, core.AlbumRule{}, nil)))
		tag, err := st.CreateTag(ctx, name, core.TagManual, nil)
		check(t, err)
		check(t, errOf(st.TagAssets(ctx, ids, tag)))
		check(t, errOf(st.NameFaces(ctx, []int64{detected[i].ID}, name)))
		// Each place holds one photo.
		res, err := st.w.ExecContext(ctx, "INSERT INTO places(name, region, country, lat, lon) VALUES (?, '', '', 0, 0)", name)
		check(t, err)
		place, err := res.LastInsertId()
		check(t, err)
		check(t, errOf(st.w.ExecContext(ctx, "UPDATE assets SET place_id = ? WHERE id = ?", place, ids[i])))
	}

	albums, err := st.Albums(ctx, core.TagConfidenceDefault)
	check(t, err)
	people, err := st.People(ctx)
	check(t, err)
	things, err := st.Things(ctx, core.TagConfidenceDefault)
	check(t, err)
	counted, err := st.Things(ctx, core.TagConfidenceDefault/2) // counted live, not kept
	check(t, err)
	places, err := st.Places(ctx)
	check(t, err)
	tags, err := st.AssetTags(ctx, ids[0], core.TagConfidenceDefault)
	check(t, err)
	for _, list := range []struct {
		name string
		got  []string
	}{
		{"albums", namesOf(albums, func(a core.Album) string { return a.Name })},
		{"people", namesOf(people, func(p core.Person) string { return p.Name })},
		{"Things", namesOf(things, func(t core.Tag) string { return t.Name })},
		{"Things at another confidence", namesOf(counted, func(t core.Tag) string { return t.Name })},
		{"places", namesOf(places, func(p core.Place) string { return p.Name })},
		{"a photo's tags", namesOf(tags, func(t core.AssetTag) string { return t.Name })},
	} {
		if !slices.Equal(list.got, want) {
			t.Errorf("%s: %q, want %q", list.name, list.got, want)
		}
	}

	// Suggestions put names starting with the text first, then shorter
	// names; names alike in both are in name order.
	for _, name := range []string{"Flan", "Élan"} {
		check(t, errOf(st.CreateTag(ctx, name, core.TagManual, nil)))
	}
	suggested, err := st.SearchTags(ctx, "lan", 5)
	check(t, err)
	if got := namesOf(suggested, func(t core.Tag) string { return t.Name }); !slices.Equal(got, []string{"Élan", "Flan"}) {
		t.Errorf("suggestions for lan: %q, want [Élan Flan]", got)
	}
}

func namesOf[T any](list []T, name func(T) string) []string {
	names := make([]string, len(list))
	for i, v := range list {
		names[i] = name(v)
	}
	return names
}
