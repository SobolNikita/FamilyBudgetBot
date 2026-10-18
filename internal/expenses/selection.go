package expenses

// Selection identifies every matching expense and its version, across all pages.
// It is a concurrency precondition, not an authorization token.
type Selection struct {
	Token string `json:"token"`
	Count int    `json:"count"`
}
