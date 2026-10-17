// Package dialog defines the persisted conversation independently of transports and processing.
package dialog

import "familyexpenses/internal/expenses"

type Turn struct {
	Text  string `json:"text"`
	Reply string `json:"reply"`
}

type Conversation struct {
	LastExpense *expenses.Expense  `json:"last_expense,omitempty"`
	Shown       []expenses.Expense `json:"shown,omitempty"`
	Turns       []Turn             `json:"turns,omitempty"`
}

type Pending struct {
	ID      int64 `json:"id,string"`
	Version int   `json:"version"`
}

type State struct {
	Context Conversation
	Pending *Pending
}
