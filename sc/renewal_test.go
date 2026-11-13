package sc_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc"
)

// This file pressure-tests the package against a credential tracker of the
// kind a TLS gateway needs: obtain a certificate, wait out rate limits at the
// time the issuer names, back off on other failures, renew at a time the
// issuer recommends, and keep serving the held certificate while a renewal
// fails.

type credential struct {
	// Serial identifies the certificate.
	Serial int `json:"serial"`

	// RenewAt is when the issuer recommends renewing it.
	RenewAt time.Time `json:"renew_at"`
}

type tracker struct {
	// Held is the certificate served; nil until one is obtained.
	Held *credential `json:"held"`

	// Failures counts consecutive failures that back off.
	Failures int `json:"failures"`

	// RetryAt is when a rate-limited issuer allows the next request.
	RetryAt time.Time `json:"retry_at"`
}

// rateLimitedError is the error an issuer returns with its own retry time.
type rateLimitedError struct {
	// RetryAt is the time the issuer names.
	RetryAt time.Time
}

func (e rateLimitedError) Error() string { return "rate limited until " + e.RetryAt.String() }

type closeTracker struct{}

const (
	// minRenewalBackoff is the first wait after a failed request.
	minRenewalBackoff = 30 * time.Second

	// maxRenewalBackoff caps the wait. Doubling from 30 seconds reaches it
	// after renewalBackoffSteps doublings, so it first applies to the
	// failure after that many.
	maxRenewalBackoff   = 30 * time.Minute
	renewalBackoffSteps = 6
)

func renewalBackoff(c tracker) time.Duration {
	return min(minRenewalBackoff<<min(c.Failures-1, renewalBackoffSteps), maxRenewalBackoff)
}

// issuer is the outside world the tracker talks to.
type issuer interface {
	Obtain(ctx context.Context) (credential, error)
	Renew(ctx context.Context, held credential) (credential, error)
}

func retryAtOf(f sc.Failed) (time.Time, bool) {
	if limited, ok := errors.AsType[rateLimitedError](f.Err); ok {
		return limited.RetryAt, true
	}

	return time.Time{}, false
}

func trackerChart(t *testing.T, iss issuer) *sc.Machine[string, tracker] {
	t.Helper()

	rateLimit := func(c tracker, f sc.Failed) tracker {
		c.RetryAt, _ = retryAtOf(f)
		return c
	}
	countFailure := func(c tracker, _ sc.Failed) tracker {
		c.Failures++
		return c
	}
	hold := func(c tracker, d sc.Done[credential]) tracker {
		c.Held, c.Failures, c.RetryAt = &d.Result, 0, time.Time{}
		return c
	}
	isRateLimited := func(_ tracker, f sc.Failed) bool {
		_, limited := retryAtOf(f)
		return limited
	}

	m, err := sc.New("tracker", func(b *sc.Builder[string, tracker]) {
		b.Initial("running")
		b.State("running", func(b *sc.Builder[string, tracker]) {
			b.Initial("obtaining")

			obtain := b.State("obtaining").Invoke("obtain", func(ctx context.Context, _ tracker) (credential, error) {
				return iss.Obtain(ctx)
			})
			obtain.OnDone().Assign("hold", hold).To("serving")
			obtain.OnError().When("rateLimitedError", isRateLimited).Assign("rateLimit", rateLimit).To("waiting_for_issuer")
			obtain.OnError().Assign("countFailure", countFailure).To("backing_off")

			b.State("waiting_for_issuer").Deadline("issuerRetryAt", func(c tracker) time.Time { return c.RetryAt }).To("obtaining")
			b.State("backing_off").Delay("backoff", renewalBackoff).To("obtaining")

			b.State("serving").Deadline("renewAt", func(c tracker) time.Time { return c.Held.RenewAt }).To("renewing")

			renew := b.State("renewing").Invoke("renew", func(ctx context.Context, c tracker) (credential, error) {
				return iss.Renew(ctx, *c.Held)
			})
			renew.OnDone().Assign("hold", hold).To("serving")
			renew.OnError().When("rateLimitedError", isRateLimited).Assign("rateLimit", rateLimit).To("renewal_waiting_for_issuer")
			renew.OnError().Assign("countFailure", countFailure).To("renewal_backing_off")

			b.State("renewal_waiting_for_issuer").Deadline("issuerRetryAt", func(c tracker) time.Time { return c.RetryAt }).To("renewing")
			b.State("renewal_backing_off").Delay("backoff", renewalBackoff).To("renewing")
		}).On[closeTracker]().To("stopped")
		b.Final("stopped")
	})
	require.NoError(t, err)

	return m
}

// trackerKey is what tells tracker states apart for the exploration: whether
// a certificate is held, the backoff step, and whether a retry time is set.
// Serials and times take endlessly many values without changing what the
// chart does next.
func trackerKey(c tracker, _ time.Time) any {
	return struct {
		Held     bool `json:"held"`
		Failures int  `json:"failures"`
		Limited  bool `json:"limited"`
	}{c.Held != nil, min(c.Failures, renewalBackoffSteps+1), !c.RetryAt.IsZero()}
}

func TestTrackerChartKeepsServingWhileRenewalFails(t *testing.T) {
	t.Parallel()

	issued := credential{Serial: 1, RenewAt: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)}
	limited := rateLimitedError{RetryAt: time.Date(2026, 11, 13, 1, 0, 0, 0, time.UTC)}
	outcomes := []sc.Outcome{sc.Succeeds(issued), sc.Fails(limited), sc.Fails(errWorkFailed)}

	report, err := trackerChart(t, nil).Explore(tracker{}, sc.Exploration[string, tracker]{
		Events:   []any{closeTracker{}},
		Outcomes: map[string][]sc.Outcome{"obtain": outcomes, "renew": outcomes},
		Key:      trackerKey,
		Invariants: []sc.Invariant[string, tracker]{{
			Name: "a renewing tracker holds a certificate",
			Holds: func(s sc.Snapshot[string, tracker]) bool {
				renewal := s.In("serving") || s.In("renewing") || s.In("renewal_waiting_for_issuer") || s.In("renewal_backing_off")
				return !renewal || s.Context.Held != nil
			},
		}},
		Steps: []sc.StepInvariant[string, tracker]{{
			Name: "a held certificate is never dropped",
			Holds: func(st sc.Step[string, tracker]) bool {
				return st.From.Context.Held == nil || st.To.Context.Held != nil
			},
		}},
	})
	require.NoError(t, err)

	require.Empty(t, report.Violations)
	require.Empty(t, report.Stuck)
	require.Empty(t, report.Trapped)
	require.False(t, report.Truncated)
}

// scriptedIssuer answers each request with the next of its answers.
type scriptedIssuer struct {
	mu      sync.Mutex
	answers []func() (credential, error)
	asked   []string
}

func (s *scriptedIssuer) next(kind string) (credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.asked = append(s.asked, fmt.Sprintf("%s at %s", kind, time.Now().UTC().Format(time.TimeOnly)))
	answer := s.answers[0]
	s.answers = s.answers[1:]

	return answer()
}

func (s *scriptedIssuer) Obtain(context.Context) (credential, error) { return s.next("obtain") }

func (s *scriptedIssuer) Renew(context.Context, credential) (credential, error) {
	return s.next("renew")
}

func TestTrackerWaitsForTheIssuersTimeAndRenewsWhenRecommended(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		iss := &scriptedIssuer{
			mu: sync.Mutex{},
			answers: []func() (credential, error){
				func() (credential, error) { return credential{}, rateLimitedError{RetryAt: start.Add(time.Hour)} },
				func() (credential, error) { return credential{Serial: 1, RenewAt: start.Add(3 * time.Hour)}, nil },
				func() (credential, error) { return credential{}, errWorkFailed },
				func() (credential, error) { return credential{Serial: 2, RenewAt: start.Add(48 * time.Hour)}, nil },
			},
			asked: nil,
		}
		a := trackerChart(t, iss).Actor(tracker{})
		wait := runActor(t.Context(), a)

		time.Sleep(4 * time.Hour) //nolint:forbidigo // GG013: advances the synctest fake clock instantly (the only way to move past a timer inside a synctest.Test bubble); this exemption is valid ONLY within synctest.Test — do not copy it to a real-time time.Sleep
		synctest.Wait()

		require.Equal(t, 2, a.Snapshot().Context.Held.Serial)
		require.Equal(t, []string{
			"obtain at 00:00:00",
			"obtain at 01:00:00", // the time the issuer named
			"renew at 03:00:00",  // the time the issuer recommended
			"renew at 03:00:30",  // the first backoff
		}, iss.asked)

		require.NoError(t, a.Send(t.Context(), closeTracker{}))
		require.NoError(t, wait())
	})
}
