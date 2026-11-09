package proxy

import "time"

// streamAge is the maximum age of one relayed stream, measured from the
// proxy's receipt of the request rather than from the first relayed byte: the
// load balancer's response deadline runs from receipt, and the upstream
// forward that precedes the stream counts against it. A zero deadline means
// the stream has no maximum age.
type streamAge struct {
	deadline time.Time
}

// streamAge returns the maximum age of a stream for a request received at
// start, under the proxy's MaxStreamAge.
func (p *Proxy) streamAge(start time.Time) streamAge {
	if p.MaxStreamAge <= 0 {
		return streamAge{deadline: time.Time{}}
	}
	return streamAge{deadline: start.Add(p.MaxStreamAge)}
}

// reached reports whether the stream has passed its maximum age at now.
func (a streamAge) reached(now time.Time) bool {
	return !a.deadline.IsZero() && !now.Before(a.deadline)
}

// afterDeadline runs f in its own goroutine once the deadline passes, or right
// away when it already has. Calling the returned stop function releases the
// timer; f may still run if the deadline passes at the same moment, so f must
// tolerate a stream that already ended.
func (a streamAge) afterDeadline(f func()) (stop func()) {
	if a.deadline.IsZero() {
		return func() {}
	}
	timer := time.AfterFunc(time.Until(a.deadline), f)
	return func() { timer.Stop() }
}
