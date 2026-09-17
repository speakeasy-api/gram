package remotesessions

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// RunRegistrationAdmissionRegression uses the external package's existing
// database fixture, exercising real session locks and blocked upstream HTTP.
func RunRegistrationAdmissionRegression(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	entered := make(chan struct{}, 4)
	unblock := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		select {
		case <-unblock:
		case <-r.Context().Done():
		}
	}))
	defer upstream.Close()
	defer close(unblock)
	register := func(ctx context.Context) error {
		release, err := admitRegistration(ctx, pool)
		if err != nil {
			return fmt.Errorf("registration attempt: %w", err)
		}
		defer release()
		conn, err := pool.Acquire(ctx)
		if err != nil {
			return fmt.Errorf("registration attempt: %w", err)
		}
		defer conn.Release()
		unlock, err := lockRegistrationIssuer(ctx, conn, uuid.New())
		if err != nil {
			return fmt.Errorf("registration attempt: %w", err)
		}
		defer unlock()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstream.URL, nil)
		if err != nil {
			return fmt.Errorf("registration attempt: %w", err)
		}
		resp, err := upstream.Client().Do(req)
		if err != nil {
			return fmt.Errorf("registration attempt: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if _, err := io.Copy(io.Discard, resp.Body); err != nil {
			return fmt.Errorf("read registration response: %w", err)
		}
		return nil
	}
	done := make(chan error, 4)
	activeCtx, cancelActive := context.WithCancel(ctx)
	defer cancelActive()
	for range 2 {
		go func() { done <- register(activeCtx) }()
	}
	for range 2 {
		select {
		case <-entered:
		case err := <-done:
			t.Fatalf("registration worker exited before entering upstream: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	// Both available registration slots are occupied by different issuers, not
	// serialized by an issuer lock. More registrations must wait outside the pool.
	queuedCtx, cancelQueued := context.WithCancel(ctx)
	defer cancelQueued()
	queued := make(chan error, 2)
	for range 2 {
		go func() { queued <- register(queuedCtx) }()
	}
	require.Eventually(t, func() bool {
		registrationAdmissions.Lock()
		defer registrationAdmissions.Unlock()
		a := registrationAdmissions.pools[pool]
		return a != nil && a.users == 4
	}, time.Second, time.Millisecond)
	require.EqualValues(t, 2, pool.Stat().AcquiredConns())
	ordinary, err := pool.Acquire(ctx)
	require.NoError(t, err, "ordinary DB work must not wait for upstream HTTP")
	ordinary.Release()
	cancelQueued()
	for range 2 {
		require.ErrorIs(t, <-queued, context.Canceled)
	}
	cancelActive()
	for range 2 {
		require.ErrorIs(t, <-done, context.Canceled)
	}
	// Canceled waiters and canceled HTTP holders must return their entire budget.
	releases := make([]func(), 0, 2)
	for range 2 {
		release, err := admitRegistration(ctx, pool)
		require.NoError(t, err)
		releases = append(releases, release)
	}
	for _, release := range releases {
		release()
		release()
	}
	registrationAdmissions.Lock()
	_, retained := registrationAdmissions.pools[pool]
	registrationAdmissions.Unlock()
	require.False(t, retained, "idle pools must not be retained by the admission registry")
}
