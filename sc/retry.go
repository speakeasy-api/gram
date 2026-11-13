package sc

import "time"

// Forever is the [Retry.Max] of a service that is run again however many
// times it fails.
const Forever = -1

// Retry is a policy for running a failed service again, set with
// [Invocation.Retry]. The wait starts at First and doubles after each
// failure until it reaches Cap.
type Retry struct {
	// Max is how many times the service is run again before its failure
	// reaches the chart: 3 allows four runs in all. [Forever] sets no
	// limit.
	Max int

	// First is the wait before the first retry.
	First time.Duration

	// Cap is the longest wait. Zero keeps every wait at First.
	Cap time.Duration

	// Jitter is the fraction, between 0 and 1, by which the driver varies
	// each wait, as [Transition.Jitter] does for a delayed transition.
	Jitter float64

	// If decides whether a failure is worth retrying; nil retries every
	// failure. It must be a pure function of the error. Under a driver
	// that persists events the error it sees keeps only its message and
	// its [Coder] code, so decide on [ErrorCode].
	If func(err error) bool
}

// wait is how long to wait after the failures-th failure, counting from 1.
func (p Retry) wait(failures int) time.Duration {
	if p.Cap <= p.First {
		return p.First
	}

	// Doubling stops at the cap, and before it could overflow.
	wait := p.First
	for range failures - 1 {
		if wait >= p.Cap/2 {
			return p.Cap
		}
		wait *= 2
	}

	return min(wait, p.Cap)
}

// steps is the number of failures after which the wait no longer grows.
func (p Retry) steps() int {
	n := 1
	for p.First > 0 && p.wait(n) < p.Cap {
		n++
	}

	return n
}
