package runner

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
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

	s := newLimiterService(0, 50*time.Millisecond)

	// With limiting disabled, many concurrent handlers must all proceed even
	// though no slots exist.
	release := make(chan struct{})
	entered := make(chan struct{}, 4)
	h := s.limit(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		entered <- struct{}{}
		<-release
		w.WriteHeader(http.StatusOK)
	}))

	for range 4 {
		go h.ServeHTTP(httptest.NewRecorder(), limiterRequest())
	}
	for range 4 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("handler did not run with limiting disabled")
		}
	}
	close(release)
}

func TestLimiterShedsWhenSaturated(t *testing.T) {
	t.Parallel()

	s := newLimiterService(1, 20*time.Millisecond)

	release := make(chan struct{})
	entered := make(chan struct{})
	h := s.limit(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		entered <- struct{}{}
		<-release
		w.WriteHeader(http.StatusOK)
	}))

	// Occupy the only slot.
	go h.ServeHTTP(httptest.NewRecorder(), limiterRequest())
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first request never acquired the slot")
	}

	// A second request finds no slot and is shed after the brief hold.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, limiterRequest())

	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.Equal(t, "1", rec.Header().Get("Retry-After"))
	require.Contains(t, rec.Body.String(), "capacity")

	close(release)
}

func TestLimiterAdmitsWhenSlotFreesDuringHold(t *testing.T) {
	t.Parallel()

	// A generous hold ensures the freed slot is observed before shedding.
	s := newLimiterService(1, 500*time.Millisecond)

	firstRelease := make(chan struct{})
	firstEntered := make(chan struct{})
	first := s.limit(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		firstEntered <- struct{}{}
		<-firstRelease
		w.WriteHeader(http.StatusOK)
	}))

	go first.ServeHTTP(httptest.NewRecorder(), limiterRequest())
	<-firstEntered

	// Free the slot shortly after the second request starts holding.
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(firstRelease)
	}()

	second := s.limit(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	second.ServeHTTP(rec, limiterRequest())

	require.Equal(t, http.StatusOK, rec.Code, "request should be admitted once the slot frees within the hold")
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
