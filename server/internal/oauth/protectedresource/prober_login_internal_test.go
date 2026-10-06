package protectedresource

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// blockingResource is a resource whose metadata endpoint never answers; it
// signals reached once a probe has arrived and holds until the probe gives up.
func blockingResource(t *testing.T) (*httptest.Server, <-chan struct{}) {
	t.Helper()
	reached := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != wellknown.OAuthProtectedResourcePath {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		select {
		case reached <- struct{}{}:
		default:
		}
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	return server, reached
}

// A login whose probe outruns its budget records the failure and reports timeout.
func TestResolveForLogin_TimeoutRecordsError(t *testing.T) {
	t.Parallel()
	ctx, env := newProberEnv(t)
	resource, _ := blockingResource(t)
	env.prober.loginBudget = 100 * time.Millisecond

	got := env.prober.ResolveForLogin(ctx, testenv.NewLogger(t), env.projectID, env.organizationID, resource.URL)

	require.Equal(t, ProbeOutcomeTimeout, got.Outcome)
	require.False(t, got.Live)
	require.Nil(t, got.ScopesSupported)
	require.Positive(t, got.ProbeDuration)
	row, err := repo.New(env.conn).GetRemoteProtectedResource(ctx, repo.GetRemoteProtectedResourceParams{ProjectID: env.projectID, ResourceIdentifier: resource.URL})
	require.NoError(t, err)
	require.True(t, row.MetadataLastErrorAt.Valid)
	require.Contains(t, row.MetadataLastError.String, "Timed out")
	require.False(t, row.MetadataFetchedAt.Valid)
	require.Empty(t, env.prober.loginSlots, "the login slot is released")
}

// A login the user abandons mid-probe records nothing: the resource was not
// seen failing, the caller left.
func TestResolveForLogin_CancelledRecordsNothing(t *testing.T) {
	t.Parallel()
	ctx, env := newProberEnv(t)
	resource, reached := blockingResource(t)
	loginCtx, cancel := context.WithCancel(ctx)
	go func() {
		<-reached
		cancel()
	}()

	got := env.prober.ResolveForLogin(loginCtx, testenv.NewLogger(t), env.projectID, env.organizationID, resource.URL)

	require.Equal(t, ProbeOutcomeCancelled, got.Outcome)
	require.False(t, got.Live)
	_, err := repo.New(env.conn).GetRemoteProtectedResource(ctx, repo.GetRemoteProtectedResourceParams{ProjectID: env.projectID, ResourceIdentifier: resource.URL})
	require.ErrorIs(t, err, pgx.ErrNoRows, "no row is written for an abandoned login")
	require.Empty(t, env.prober.loginSlots)
}

// With every login slot taken the login keeps what is cached and never
// touches the on-use pool.
func TestResolveForLogin_NoSlot(t *testing.T) {
	t.Parallel()
	ctx, env := newProberEnv(t)
	resource, reached := blockingResource(t)
	for range cap(env.prober.loginSlots) {
		env.prober.loginSlots <- struct{}{}
	}

	got := env.prober.ResolveForLogin(ctx, testenv.NewLogger(t), env.projectID, env.organizationID, resource.URL)

	require.Equal(t, ProbeOutcomeNoSlot, got.Outcome)
	require.Nil(t, got.Row)
	require.Empty(t, reached, "no probe was started")
	require.Empty(t, env.prober.slots, "on-use slots are a separate pool")
	require.Len(t, env.prober.loginSlots, cap(env.prober.loginSlots))
}

// A probe that reads a valid document reports it live, records the row, and
// leaves a debounce check the proxy's next use trusts.
func TestResolveForLogin_FetchedRecordsRowAndCheck(t *testing.T) {
	t.Parallel()
	ctx, env := newProberEnv(t)
	var origin string
	resource := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != wellknown.OAuthProtectedResourcePath {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"resource":"` + origin + `","authorization_servers":["https://as.example.test"],"scopes_supported":["files:read"]}`))
	}))
	t.Cleanup(resource.Close)
	origin = resource.URL

	got := env.prober.ResolveForLogin(ctx, testenv.NewLogger(t), env.projectID, env.organizationID, resource.URL)

	require.Equal(t, ProbeOutcomeFetched, got.Outcome)
	require.True(t, got.Live)
	require.Equal(t, []string{"files:read"}, got.ScopesSupported)
	row, err := repo.New(env.conn).GetRemoteProtectedResource(ctx, repo.GetRemoteProtectedResourceParams{ProjectID: env.projectID, ResourceIdentifier: resource.URL})
	require.NoError(t, err)
	require.Equal(t, []string{"files:read"}, row.ScopesSupported)
	_, checked := env.prober.checked.Load(env.projectID.String() + " " + resource.URL)
	require.True(t, checked, "the proxy's next use needs no row read")
}
