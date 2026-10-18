package expenses

// Selection identifies every matching expense and its version, across all pages.
// It is a concurrency precondition, not an authorization token.
type Selection struct {
	Token string `json:"token"`
	Count int    `json:"count"`
}

const MaxSelectionItems = 1000

type ExpenseRef struct {
	ID      int64 `json:"id,string"`
	Version int   `json:"version"`
}

// DeleteSelectionRequest supports either a filter snapshot with exclusions,
// or explicitly selected versions. The two modes must not be mixed.
type DeleteSelectionRequest struct {
	Token   string       `json:"token,omitempty"`
	Items   []ExpenseRef `json:"items,omitempty"`
	Exclude []string     `json:"exclude,omitempty"`
}
