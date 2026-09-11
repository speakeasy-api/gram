package remotesessions

import "time"

// RecheckLease is how long a claimed keepalive re-check row is held before it is due again: a quarter of the interval.
func RecheckLease(interval time.Duration) time.Duration {
	return interval / 4
}
