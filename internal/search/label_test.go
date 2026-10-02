package search

import (
	"testing"

	"lumen/internal/store"
)

// A built-in the user renamed is still the built-in: its floor and kind come
// from its vocabulary name, not the name it is shown under.
func TestRenamedBuiltinKeepsItsVocabularyFloor(t *testing.T) {
	var sure Label
	for _, l := range Vocabulary {
		if l.Floor > 0 {
			sure = l
			break
		}
	}
	if sure.Name == "" {
		t.Skip("no label in the vocabulary has a floor")
	}
	renamed := newLabel(store.AutoTag{ID: 1, Name: "My name for it", VocabName: sure.Name, Builtin: true}, nil)
	if renamed.floor != sure.Floor {
		t.Fatalf("renamed %q has floor %v, want %v", sure.Name, renamed.floor, sure.Floor)
	}
	userLabel := newLabel(store.AutoTag{ID: 2, Name: sure.Name}, nil) // a Thing the user made, named like it
	if userLabel.floor != 0 {
		t.Fatalf("a user's label took the built-in's floor %v", userLabel.floor)
	}
}
