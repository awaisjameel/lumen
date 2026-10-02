// Package fileops makes every change to files in the user's folders. Nothing
// else in Lumen writes there, so the rules that keep a library safe live in
// one place:
//
//   - A file is only ever deleted by moving it to the Recycle Bin (Recycle).
//     The shell is asked to recycle and told to refuse anything it would
//     destroy instead (a network share, a drive without a bin, a file too big
//     for it), so a file is recycled or left alone, never destroyed. os.Remove
//     is never called on a user's file.
//   - The Windows code is in _windows.go files, with an _other.go stub that
//     returns errors.ErrUnsupported, as internal/scanner does.
//   - fileops holds no SQL: callers persist what it did through store.
package fileops

// Status is what happened to one path given to Recycle.
type Status int

// The outcomes of a path.
const (
	// Failed means the file could not be recycled for a reason the Outcome
	// gives. It was not changed.
	Failed Status = iota
	// Recycled means the file is now in the Recycle Bin.
	Recycled
	// Kept means the shell would have destroyed the file rather than recycle
	// it, so it was left exactly where it was.
	Kept
	// Gone means there was no file at the path to begin with. That is what
	// the caller wanted, so it is not a failure.
	Gone
)

// Outcome is the result for one path, in the order the paths were given.
type Outcome struct {
	Path   string
	Status Status
	Err    error // why, when Status is Failed
}

// tsfDeleteRecycleIfPossible is TSF_DELETE_RECYCLE_IF_POSSIBLE, the flag the
// shell sets on an item it will put in the Recycle Bin.
const tsfDeleteRecycleIfPossible = 0x80

// recyclable is the progress sink's decision about one item about to be
// deleted: go ahead only if the shell says it will recycle it. Without the
// flag the shell would delete it for good.
func recyclable(flags uint32) bool { return flags&tsfDeleteRecycleIfPossible != 0 }
