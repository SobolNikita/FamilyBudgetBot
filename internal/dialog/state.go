// Package dialog defines the persisted conversation independently of transports and processing.
package dialog

import "familyexpenses/internal/expenses"

type Turn struct {
	Text  string `json:"text"`
	Reply string `json:"reply"`
	Date  string `json:"date,omitempty"`
}

type Conversation struct {
	LastExpense *expenses.Expense  `json:"last_expense,omitempty"`
	Shown       []expenses.Expense `json:"shown,omitempty"`
	Turns       []Turn             `json:"turns,omitempty"`
	List        *ListState         `json:"list,omitempty"`
}

// ListState survives trimming conversational history and restarting the worker.
// Ownership is always supplied by the server, never by the stored filter.
type ListState struct {
	Filter     expenses.Filter `json:"filter"`
	NextOffset int             `json:"next_offset"`
	HasMore    bool            `json:"has_more"`
	Revision   int64           `json:"revision"`
}

type Pending struct {
	ID      int64 `json:"id,string"`
	Version int   `json:"version"`
}

type State struct {
	Context Conversation
	Pending *Pending
}
