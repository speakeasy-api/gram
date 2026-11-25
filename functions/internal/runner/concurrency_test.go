package runner

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/semaphore"
)

func newLimiterService(maxConcurrency int, hold time.Duration) *Service {
	var slots *semaphore.Weighted
	if maxConcurrency > 0 {
		slots = semaphore.NewWeighted(int64(maxConcurrency))
	}

	return &Service{
		logger:         slog.New(slog.DiscardHandler),
		encryption:     nil,
		workDir:        "",
		command:        "",
		args:           nil,
		maxConcurrency: maxConcurrency,
		slots:          slots,
		inFlight:       atomic.Int64{},
		holdTimeout:    hold,
		retryAfter:     time.Second,
	}
}

func limiterRequest() *http.Request {
	return httptest.NewRequest(http.MethodPost, "/tool-call", nil)
}

func TestLimiterAllowsWhenSlotAvailable(t *testing.T) {
	t.Parallel()

	s := newLimiterService(2, 50*time.Millisecond)
	h := s.limit(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, limiterRequest())

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestLimiterDisabledWhenMaxConcurrencyZero(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := newLimiterService(0, 50*time.Millisecond)

		// With limiting disabled, many concurrent handlers must all proceed
		// even though no slots exist.
		release := make(chan struct{})
		defer close(release)
		var entered atomic.Int32
		h := s.limit(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			entered.Add(1)
			<-release
			w.WriteHeader(http.StatusOK)
		}))

		for range 4 {
			go h.ServeHTTP(httptest.NewRecorder(), limiterRequest())
		}
		synctest.Wait()

		require.Equal(t, int32(4), entered.Load(), "every handler should run with limiting disabled")
	})
}

func TestLimiterShedsWhenSaturated(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		hold := 20 * time.Millisecond
		s := newLimiterService(1, hold)

		release := make(chan struct{})
		defer close(release)
		h := s.limit(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			<-release
			w.WriteHeader(http.StatusOK)
		}))

		// Occupy the only slot; once the bubble is idle the handler holds it.
		go h.ServeHTTP(httptest.NewRecorder(), limiterRequest())
		synctest.Wait()

		// A second request finds no slot and is shed after the brief hold.
		start := time.Now()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, limiterRequest())

		require.Equal(t, http.StatusTooManyRequests, rec.Code)
		require.Equal(t, "1", rec.Header().Get("Retry-After"))
		require.Contains(t, rec.Body.String(), "capacity")
		require.Equal(t, hold, time.Since(start), "request should be shed exactly when the hold expires")
	})
}

func TestLimiterAdmitsWhenSlotFreesDuringHold(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		freeAfter := 50 * time.Millisecond
		s := newLimiterService(1, 500*time.Millisecond)

		firstRelease := make(chan struct{})
		first := s.limit(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			<-firstRelease
			w.WriteHeader(http.StatusOK)
		}))

		// Occupy the only slot; once the bubble is idle the handler holds it.
		go first.ServeHTTP(httptest.NewRecorder(), limiterRequest())
		synctest.Wait()

		// Free the slot partway through the second request's hold.
		go func() {
			time.Sleep(freeAfter)
			close(firstRelease)
		}()

		second := s.limit(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		start := time.Now()
		rec := httptest.NewRecorder()
		second.ServeHTTP(rec, limiterRequest())

		require.Equal(t, http.StatusOK, rec.Code, "request should be admitted once the slot frees within the hold")
		require.Equal(t, freeAfter, time.Since(start), "request should be admitted as soon as the slot frees")
	})
}

func TestLimiterReleasesSlotAfterCompletion(t *testing.T) {
	t.Parallel()

	s := newLimiterService(1, 20*time.Millisecond)
	h := s.limit(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Sequential requests reuse the single slot since each releases on return.
	for range 3 {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, limiterRequest())
		require.Equal(t, http.StatusOK, rec.Code)
	}
}
