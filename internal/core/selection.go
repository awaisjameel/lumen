package core

// Selection is an explicit snapshot of resolved ids. Batch actions check its
// library revision before using it, then revalidate each asset at execution.
type Selection struct {
	IDs      []int64 `json:"ids"`
	Revision int64   `json:"revision"`
}
