package guardian

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

type scriptedLimiter struct {
	mu      sync.Mutex
	results []RateLimitResult
	keys    []Partition
}

var _ Limiter = (*scriptedLimiter)(nil)

func (l *scriptedLimiter) AllowN(_ context.Context, key Partition, _ Limit, _ uint32) (RateLimitResult, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	call := len(l.keys)
	l.keys = append(l.keys, key)
	if call >= len(l.results) {
		return RateLimitResult{}, fmt.Errorf("unexpected rate limit check %d", call+1)
	}

	return l.results[call], nil
}

func (l *scriptedLimiter) observedKeys() []Partition {
	l.mu.Lock()
	defer l.mu.Unlock()

	return append([]Partition(nil), l.keys...)
}

type recordingRoundTripper struct {
	calls atomic.Int64
}

var _ http.RoundTripper = (*recordingRoundTripper)(nil)

func (t *recordingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	t.calls.Add(1)

	return &http.Response{
		StatusCode: http.StatusNoContent,
		Header:     make(http.Header),
		Body:       http.NoBody,
		Request:    req,
	}, nil
}

func pacingTransport(limiter Limiter, next http.RoundTripper) *resilienceTransport {
	return &resilienceTransport{
		next: next,
		name: "test-upstream",
		config: ResilienceConfig{
			Partition:       nil,
			Limit:           PerSecond(1),
			WaitForCapacity: true,
			Breaker:         NoBreaker(),
		},
		limiter: limiter,
		breaker: NoopBreaker{partitions: nil},
	}
}

func deniedAdmission(wait time.Duration) RateLimitResult {
	return RateLimitResult{
		Limit:      PerSecond(1),
		Allowed:    0,
		Remaining:  0,
		RetryAfter: wait,
		ResetAfter: wait,
	}
}

func allowedAdmission() RateLimitResult {
	return RateLimitResult{
		Limit:      PerSecond(1),
		Allowed:    1,
		Remaining:  0,
		RetryAfter: -1,
		ResetAfter: time.Second,
	}
}

type roundTripResult struct {
	response *http.Response
	err      error
}

func TestResilienceTransport_WaitForCapacityRechecksAfterContention(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		limiter := &scriptedLimiter{
			mu: sync.Mutex{},
			results: []RateLimitResult{
				deniedAdmission(time.Second),
				deniedAdmission(time.Second),
				allowedAdmission(),
			},
			keys: nil,
		}
		next := &recordingRoundTripper{calls: atomic.Int64{}}
		transport := pacingTransport(limiter, next)
		req, err := http.NewRequestWithContext(WithSubset(t.Context(), "org-1"), http.MethodPost, "https://example.com/events", nil)
		require.NoError(t, err)

		done := make(chan roundTripResult, 1)
		go func() {
			response, err := transport.RoundTrip(req)
			if response != nil {
				err = errors.Join(err, response.Body.Close())
			}
			done <- roundTripResult{response: response, err: err}
		}()

		synctest.Wait()
		require.Len(t, limiter.observedKeys(), 1)
		require.Equal(t, int64(0), next.calls.Load())

		<-time.After(time.Second)
		synctest.Wait()
		require.Len(t, limiter.observedKeys(), 2, "a caller that loses capacity to contention must check again")
		require.Equal(t, int64(0), next.calls.Load(), "a second denial must still prevent network I/O")

		<-time.After(time.Second)
		synctest.Wait()
		keys := limiter.observedKeys()
		require.Len(t, keys, 3)
		require.Equal(t, keys[0], keys[1])
		require.Equal(t, keys[1], keys[2])
		require.Equal(t, NewPartition("test-upstream", "example.com", "443").WithSubset("org-1"), keys[0])
		require.Equal(t, int64(1), next.calls.Load())

		result := <-done
		require.NoError(t, result.err)
	})
}

func TestResilienceTransport_WaitForCapacityCancellationPreventsNetworkIO(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		limiter := &scriptedLimiter{
			mu:      sync.Mutex{},
			results: []RateLimitResult{deniedAdmission(time.Hour)},
			keys:    nil,
		}
		next := &recordingRoundTripper{calls: atomic.Int64{}}
		transport := pacingTransport(limiter, next)
		ctx, cancel := context.WithCancel(t.Context())
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.com/events", nil)
		require.NoError(t, err)

		done := make(chan roundTripResult, 1)
		go func() {
			response, err := transport.RoundTrip(req)
			if response != nil {
				err = errors.Join(err, response.Body.Close())
			}
			done <- roundTripResult{response: response, err: err}
		}()

		synctest.Wait()
		require.Len(t, limiter.observedKeys(), 1)
		require.Equal(t, int64(0), next.calls.Load())

		cancel()
		synctest.Wait()
		result := <-done
		require.Nil(t, result.response)
		require.ErrorIs(t, result.err, context.Canceled)
		require.Len(t, limiter.observedKeys(), 1)
		require.Equal(t, int64(0), next.calls.Load())
	})
}

func TestResilienceTransport_WaitForCapacityDeadlinePreventsNetworkIO(t *testing.T) {
	t.Parallel()

	limiter := &scriptedLimiter{
		mu:      sync.Mutex{},
		results: []RateLimitResult{deniedAdmission(time.Hour)},
		keys:    nil,
	}
	next := &recordingRoundTripper{calls: atomic.Int64{}}
	transport := pacingTransport(limiter, next)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.com/events", nil)
	require.NoError(t, err)

	response, err := transport.RoundTrip(req)
	if response != nil {
		require.NoError(t, response.Body.Close())
	}
	require.Nil(t, response)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Len(t, limiter.observedKeys(), 1)
	require.Equal(t, int64(0), next.calls.Load())
}

func TestResilienceTransport_WaitForCapacityPermanentDenialDoesNotRecheck(t *testing.T) {
	t.Parallel()

	limiter := &scriptedLimiter{
		mu:      sync.Mutex{},
		results: []RateLimitResult{deniedAdmission(rate.InfDuration)},
		keys:    nil,
	}
	next := &recordingRoundTripper{calls: atomic.Int64{}}
	transport := pacingTransport(limiter, next)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://example.com/events", nil)
	require.NoError(t, err)

	response, err := transport.RoundTrip(req)
	if response != nil {
		require.NoError(t, response.Body.Close())
	}
	require.Nil(t, response)
	require.Error(t, err)
	require.Len(t, limiter.observedKeys(), 1)
	require.Equal(t, int64(0), next.calls.Load())
}
