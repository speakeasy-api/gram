package protectedresource

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// probeOnUseAndWait schedules an on-use probe and waits for it to finish,
// reporting whether one was scheduled at all.
func probeOnUseAndWait(t *testing.T, ctx context.Context, env proberEnv, resourceURL string) bool {
	t.Helper()
	scheduled := make(chan struct{}, 1)
	done := make(chan struct{})
	env.prober.beforeDetached = func() { scheduled <- struct{}{} }
	env.prober.afterDetached = func() { close(done) }
	env.prober.ProbeOnUse(ctx, testenv.NewLogger(t), env.projectID, env.organizationID, resourceURL)
	select {
	case <-scheduled:
	default:
		return false
	}
	select {
	case <-done:
	case <-time.After(probeBudget):
		require.Fail(t, "the on-use probe did not finish")
	}
	return true
}

// A login whose document could not be written keeps the live document for
// itself but leaves no debounce check: the proxy's next use of the server
// reads the row and probes again once writes recover.
func TestResolveForLogin_FailedWriteLeavesProxyFreeToRetry(t *testing.T) {
	t.Parallel()
	ctx, env := newProberEnv(t)
	resource := validResource(t)
	key := env.projectID.String() + " " + resource.URL
	env.prober.record = func(context.Context, repo.DBTX, uuid.UUID, string, string, wellknown.OAuthProtectedResourceMetadata) error {
		return errors.New("injected write failure")
	}

	got := env.prober.ResolveForLogin(ctx, testenv.NewLogger(t), env.projectID, env.organizationID, resource.URL)

	require.Equal(t, ProbeOutcomeFetched, got.Outcome)
	require.True(t, got.Live, "the login still uses the document it read")
	require.Equal(t, []string{"files:read"}, got.ScopesSupported)
	_, err := repo.New(env.conn).GetRemoteProtectedResource(ctx, repo.GetRemoteProtectedResourceParams{ProjectID: env.projectID, ResourceIdentifier: resource.URL})
	require.ErrorIs(t, err, pgx.ErrNoRows, "the write failed")
	_, checked := env.prober.checked.Load(key)
	require.False(t, checked, "a failed write must not debounce the proxy")

	// Writes recover: the proxy's next use probes and records the row.
	env.prober.record = Record
	require.True(t, probeOnUseAndWait(t, ctx, env, resource.URL), "the proxy retries once writes recover")
	row, err := repo.New(env.conn).GetRemoteProtectedResource(ctx, repo.GetRemoteProtectedResourceParams{ProjectID: env.projectID, ResourceIdentifier: resource.URL})
	require.NoError(t, err)
	require.Equal(t, []string{"files:read"}, row.ScopesSupported)
	_, checked = env.prober.checked.Load(key)
	require.True(t, checked, "the recorded read is debounced")
}

// An on-use probe that finds the resource failing records the failure and
// leaves no check, so the next use decides from the row's own backoff rather
// than waiting out the recheck window.
func TestProbeOnUse_FailedProbeLeavesNoCheck(t *testing.T) {
	t.Parallel()
	ctx, env := newProberEnv(t)
	resource := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(resource.Close)
	key := env.projectID.String() + " " + resource.URL

	require.True(t, probeOnUseAndWait(t, ctx, env, resource.URL))

	row, err := repo.New(env.conn).GetRemoteProtectedResource(ctx, repo.GetRemoteProtectedResourceParams{ProjectID: env.projectID, ResourceIdentifier: resource.URL})
	require.NoError(t, err)
	require.True(t, row.MetadataLastErrorAt.Valid, "the failure is recorded on the row")
	require.False(t, row.MetadataFetchedAt.Valid)
	_, checked := env.prober.checked.Load(key)
	require.False(t, checked, "a failed probe holds no check")
}
