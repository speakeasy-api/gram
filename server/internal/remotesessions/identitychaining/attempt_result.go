package identitychaining

import "time"

// attemptResult is a lock holder's failure as published to its waiters.
type attemptResult struct {
	// Outcome is the holder's failure.
	Outcome Outcome `json:"outcome"`

	// FinishedAt keeps a waiter from adopting an earlier attempt's result.
	FinishedAt time.Time `json:"finished_at"`
}
