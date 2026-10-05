package core

// Selection is an explicit snapshot of resolved ids. Batch actions check its
// library revision before using it, then revalidate each asset at execution.
type Selection struct {
	IDs      []int64 `json:"ids"`
	Revision int64   `json:"revision"`
}

// BatchAction is what a selection action does to each of its assets.
type BatchAction string

// The actions a selection can take in one call.
const (
	BatchFavourite   BatchAction = "favourite"
	BatchUnfavourite BatchAction = "unfavourite"
	BatchAlbum       BatchAction = "album" // into a manual album
	BatchRemoveAlbum BatchAction = "removeAlbum"
	BatchTrash       BatchAction = "trash"
	BatchRestore     BatchAction = "restore"
)

// BatchOutcome is what became of one asset of a selection action. Code is
// the failure's error code when it has one; Error is "" when it worked.
type BatchOutcome struct {
	ID    int64  `json:"id"`
	Code  string `json:"code"`
	Error string `json:"error"`
}
