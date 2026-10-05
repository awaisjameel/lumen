package services

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"lumen/internal/core"
	"lumen/internal/store"
)

func openThingsStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// codeOf is the code the UI receives for err.
func codeOf(t *testing.T, err error) ErrorCode {
	t.Helper()
	var f Failure
	if err := json.Unmarshal(MarshalError(err), &f); err != nil {
		t.Fatalf("error %v has no code: %v", err, err)
	}
	return f.Code
}

func TestRenameAndPinAThingThroughTheService(t *testing.T) {
	ctx := t.Context()
	st := openThingsStore(t)
	if err := st.SyncVocabulary(ctx, []store.VocabLabel{{Name: "Beaches", Prompts: []string{"a beach"}}, {Name: "Dogs", Prompts: []string{"a dog"}}}); err != nil {
		t.Fatal(err)
	}
	beaches, _, err := st.TagByName(ctx, "Beaches")
	if err != nil {
		t.Fatal(err)
	}
	s := NewSearchService(st, nil, false, nil)

	if err := s.RenameThing(ctx, beaches, "Seaside"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetThingPinned(ctx, beaches, true); err != nil {
		t.Fatal(err)
	}
	things, err := s.Things(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(things) != 2 || things[0].Name != "Seaside" || things[0].Original != "Beaches" || !things[0].Pinned || things[1].Pinned {
		t.Fatalf("Things = %+v, want the renamed, pinned built-in first", things)
	}
	if suggested, err := s.SuggestTags(ctx, "seas"); err != nil || len(suggested) != 1 || suggested[0].ID != beaches {
		t.Fatalf("suggestions for the new name = %+v, %v", suggested, err)
	}
	if suggested, err := s.SuggestTags(ctx, "beach"); err != nil || len(suggested) != 0 {
		t.Fatalf("suggestions for the old name = %+v, %v", suggested, err)
	}
	if err := s.RenameThing(ctx, beaches, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.SetThingPinned(ctx, beaches, false); err != nil {
		t.Fatal(err)
	}
	if things, _ = s.Things(ctx); things[0].Name != "Beaches" || things[0].Original != "" || things[0].Pinned {
		t.Fatalf("Things after the reset = %+v", things)
	}
}

func TestRenamingAThingExplainsTheFailureWithACode(t *testing.T) {
	ctx := t.Context()
	st := openThingsStore(t)
	if err := st.SyncVocabulary(ctx, []store.VocabLabel{{Name: "Beaches", Prompts: []string{"a beach"}}, {Name: "Dogs", Prompts: []string{"a dog"}}}); err != nil {
		t.Fatal(err)
	}
	beaches, _, _ := st.TagByName(ctx, "Beaches")
	dogs, _, _ := st.TagByName(ctx, "Dogs")
	secret, err := st.CreateTag(ctx, "Secret", core.TagManual, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetBlock(ctx, core.BlockTag, secret, "", core.BlockLabel); err != nil {
		t.Fatal(err)
	}
	s := NewSearchService(st, nil, false, nil)
	for _, c := range []struct {
		name string
		call func() error
		want ErrorCode
	}{
		{"a name another Thing has", func() error { return s.RenameThing(ctx, beaches, "dogs") }, CodeTagExists},
		{"a blocked name", func() error { return s.RenameThing(ctx, beaches, "Secret") }, CodeBlockedName},
		{"no name", func() error { return s.RenameThing(ctx, secret, " ") }, CodeInvalidName},
		{"a Thing that is gone", func() error { return s.RenameThing(ctx, 999, "x") }, CodeNotFound},
		{"pinning a Thing that is gone", func() error { return s.SetThingPinned(ctx, 999, true) }, CodeNotFound},
	} {
		if got := codeOf(t, c.call()); got != c.want {
			t.Errorf("%s: code %q, want %q", c.name, got, c.want)
		}
	}
	if err := s.RenameThing(ctx, dogs, "Dogs"); err != nil {
		t.Fatalf("renaming a Thing to its own name: %v", err)
	}
}

func TestPinAnEventAndChangeTheEventGapThroughTheService(t *testing.T) {
	ctx := t.Context()
	st := openThingsStore(t)
	src, err := st.AddSource(ctx, `C:\Photos`)
	if err != nil {
		t.Fatal(err)
	}
	const start = 1_767_261_600
	var ids []int64
	for i, hours := range []int64{0, 2, 40} {
		name := string(rune('a' + i))
		if _, err := st.Ingest(ctx, []store.IngestItem{{SourceID: src.ID, Path: `C:\Photos\` + name + `.jpg`, Size: 1,
			Hash: []byte(name), Kind: core.KindPhoto, Ext: ".jpg", TakenAt: start + hours*3600}}); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, int64(i+1))
	}
	s := NewLibraryService(st, nil, nil, nil)
	if err := s.SetEventName(ctx, ids[0], "Trip"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEventPinned(ctx, ids[0], true); err != nil {
		t.Fatal(err)
	}
	events, err := s.Events(ctx)
	if err != nil || len(events) != 2 || events[0].ID != ids[0] || !events[0].Pinned || events[0].Name != "Trip" || events[1].Pinned {
		t.Fatalf("events = %+v, %v; want the pinned one first, named Trip", events, err)
	}

	// The gap change settles the tags inside the write that stores it, so
	// the halves of the split event are both named and pinned at once.
	if err := s.SetEventGapHours(ctx, 1); err != nil {
		t.Fatal(err)
	}
	events, err = s.Events(ctx)
	if err != nil || len(events) != 3 {
		t.Fatalf("events at a one hour gap = %+v, %v", events, err)
	}
	pinned := 0
	for _, e := range events {
		if e.Pinned {
			pinned++
			if e.Name != "Trip" {
				t.Errorf("a pinned half is named %q", e.Name)
			}
		}
	}
	if pinned != 2 {
		t.Fatalf("%d events pinned after the split, want both halves", pinned)
	}
	if got, _ := st.EventGapHours(ctx); got != 1 {
		t.Fatalf("gap = %d", got)
	}
	if err := s.SetEventPinned(ctx, 999, true); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("pinning an event that is not there: %v", err)
	}
}
