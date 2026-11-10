package remotesessions_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

// Wait on PostgreSQL's actual lock graph, not scheduling sleeps. The writer owns
// the source row; preparation must be waiting there without owning a registration
// or server lock that the source's synchronization needs next.
func waitForPreparationSourceWait(t *testing.T, ctx context.Context, ti *testInstance, writer pgx.Tx) {
	t.Helper()
	var pid int32
	//nolint:glint // notestingrawsql: observe PostgreSQL transaction lock ownership; this is concurrency instrumentation, not fixture data.
	require.NoError(t, writer.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid))
	require.Eventually(t, func() bool {
		var waiting bool
		//nolint:glint // notestingrawsql: observe PostgreSQL transaction lock ownership; this is concurrency instrumentation, not fixture data.
		err := ti.conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity a WHERE a.datname = current_database() AND $1 = ANY(pg_blocking_pids(a.pid)))`, pid).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond, "preparation never reached the source lock")
}

func lockPreparationHeaderSource(t *testing.T, ctx context.Context, ti *testInstance, serverID uuid.UUID) (pgx.Tx, uuid.UUID) {
	t.Helper()
	var sourceID uuid.UUID
	fixture1, fixtureErr := repo.New(ti.conn).GetPreparationServerSourceFixture(ctx, serverID)
	require.NoError(t, fixtureErr)
	sourceID = fixture1.UUID
	writer := testenv.BeginTx(t, ctx, ti.conn)
	auth, _ := contextvalues.GetAuthContext(ctx)
	require.NoError(t, authclientbackfill.LockRemoteSource(ctx, writer, auth.ActiveOrganizationID, sourceID))
	return writer, sourceID
}

func finishPreparationHeaderWrite(t *testing.T, ctx context.Context, ti *testInstance, writer pgx.Tx, sourceID uuid.UUID) {
	t.Helper()
	auth, _ := contextvalues.GetAuthContext(ctx)
	// This is the source header mutation's actual transactional synchronization
	// path: hold source, mutate header evidence, then synchronize typed copies.
	err := repo.New(writer).InsertPreparationSourceHeaderFixtureLock(ctx, repo.InsertPreparationSourceHeaderFixtureLockParams{SourceID: sourceID, ProjectID: *auth.ProjectID})
	require.NoError(t, err)
	require.NoError(t, authclientbackfill.SyncRemoteSource(ctx, writer, auth.ActiveOrganizationID, sourceID))
	require.NoError(t, writer.Commit(ctx))
}

func TestPrepareEMA_OpposingHeaderWriterCompletes(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	serverID := seedRemoteMCPServerForIssuer(t, ctx, ti, in.UserSessionIssuerID, "ordered-ema", in.Resource)
	in.ConfirmGrants = []string{oauthwire.GrantTypeJWTBearer}
	first, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, remotesessions.PreparationStateReady, first.State)
	writer, sourceID := lockPreparationHeaderSource(t, ctx, ti, serverID)
	done := make(chan error, 1)
	go func() { _, err := ti.service.PrepareIdentityChaining(ctx, in); done <- err }()
	waitForPreparationSourceWait(t, ctx, ti, writer)
	finishPreparationHeaderWrite(t, ctx, ti, writer, sourceID)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	auth, _ := contextvalues.GetAuthContext(ctx)
	typed := assertPreparationTypedClient(t, ctx, ti, in.ClientID, in.Resource)
	var copies int
	fixture2, fixtureErr := repo.New(ti.conn).CountPreparationHeaderCopiesFixture(ctx, repo.CountPreparationHeaderCopiesFixtureParams{ClientID: typed, OrganizationID: auth.ActiveOrganizationID})
	require.NoError(t, fixtureErr)
	copies = int(fixture2)
	require.Equal(t, 1, copies)
}

func TestPreparationDCR_CompletionWaitsForHeaderWriterWithoutReplay(t *testing.T) {
	t.Parallel()
	requested, respond := make(chan struct{}), make(chan struct{})
	var posts atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		close(requested)
		<-respond
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"client_id": "lock-test-client", "client_secret": "lock-test-secret", "token_endpoint_auth_method": "client_secret_basic", "grant_types": []string{oauthwire.GrantTypeJWTBearer}})
	}))
	t.Cleanup(provider.Close)
	t.Cleanup(func() {
		select {
		case <-respond:
		default:
			close(respond)
		}
	})
	ctx, ti, in, _ := preparationDCRFixture(t, provider.URL)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	serverID := seedRemoteMCPServerForIssuer(t, ctx, ti, in.UserSessionIssuerID, "ordered-dcr", in.Resource)
	type outcome struct {
		result *remotesessions.PreparationResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() { result, err := ti.service.PrepareIdentityChaining(ctx, in); done <- outcome{result, err} }()
	select {
	case <-requested:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	writer, sourceID := lockPreparationHeaderSource(t, ctx, ti, serverID)
	close(respond)
	waitForPreparationSourceWait(t, ctx, ti, writer)
	finishPreparationHeaderWrite(t, ctx, ti, writer, sourceID)
	select {
	case out := <-done:
		require.NoError(t, out.err)
		require.Equal(t, remotesessions.PreparationStateReady, out.result.State)
		require.EqualValues(t, 1, posts.Load())
		auth, _ := contextvalues.GetAuthContext(ctx)
		saved, err := repo.New(ti.conn).GetRemoteSessionClientByID(ctx, repo.GetRemoteSessionClientByIDParams{ID: out.result.ClientID, ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID})
		require.NoError(t, err)
		require.Equal(t, "lock-test-client", saved.RemoteSessionClient.ClientID)
		require.True(t, saved.RemoteSessionClient.ClientSecretEncrypted.Valid)
		require.NotEmpty(t, saved.RemoteSessionClient.ClientSecretEncrypted.String)
		assertPreparationTypedClient(t, ctx, ti, out.result.ClientID, in.Resource)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
