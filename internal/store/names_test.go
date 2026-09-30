package store

import (
	"database/sql"
	"errors"
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
