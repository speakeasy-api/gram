package remotesessions

import "time"

// RecheckLease holds a claim for a quarter of the interval, but never less
// than five minutes. The floor covers a full queued batch even when operators
// configure an interval shorter than the time needed to probe that batch.
func RecheckLease(interval time.Duration) time.Duration {
	return max(interval/4, 5*time.Minute)
}
