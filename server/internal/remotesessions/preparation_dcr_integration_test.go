package remotesessions_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/stretchr/testify/require"
)

// Rebuild the service with the same database and no process-local preparation
// state. Claims/results must survive losing the original service instance.
func restartPreparationService(t *testing.T, ti *testInstance) *remotesessions.Service {
	t.Helper()
	logger := testenv.NewLogger(t)
	tracer := testenv.NewTracerProvider(t)
	meter := testenv.NewMeterProvider(t)
	policy, err := guardian.NewUnsafePolicy(tracer, []string{})
	require.NoError(t, err)
	origin, err := url.Parse(testServerURL)
	require.NoError(t, err)
	enc := testenv.NewEncryptionClient(t)
	return remotesessions.NewService(logger, tracer, meter, ti.conn, ti.sessionManager, authz.NewEngine(logger, ti.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient()), enc, ti.envEntries, policy, audit.NewLogger(), origin, remotesessions.NewRefreshService(logger, meter, ti.conn, enc, policy, ti.redisCache), ti.features)
}
func preparationDCRFixture(t *testing.T, endpoint string) (context.Context, *testInstance, remotesessions.PreparationInput, uuid.UUID) {
	t.Helper()
	ctx, ti, in := preparationFixture(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, err := ti.conn.Exec(ctx, `UPDATE remote_session_issuers SET registration_endpoint=$1, token_endpoint_auth_methods_supported=ARRAY['client_secret_basic'] WHERE id=$2 AND project_id=$3`, endpoint, in.RemoteSessionIssuerID, *auth.ProjectID)
	require.NoError(t, err)
	// The existing interactive attachment and credentials are deliberately distinct.
	interactive := in.ClientID
	_, err = ti.conn.Exec(ctx, `UPDATE remote_session_clients SET client_secret_encrypted='interactive-ciphertext', token_endpoint_auth_method='client_secret_basic', grant_types=ARRAY['authorization_code','refresh_token'], scope=ARRAY['openid'] WHERE id=$1 AND project_id=$2`, interactive, *auth.ProjectID)
	require.NoError(t, err)
	_, err = ti.conn.Exec(ctx, `INSERT INTO remote_session_client_user_session_issuers (remote_session_client_id,user_session_issuer_id) SELECT id,$2 FROM remote_session_clients WHERE id=$1 AND project_id=$3`, interactive, in.UserSessionIssuerID, *auth.ProjectID)
	require.NoError(t, err)
	in.ClientID = uuid.Nil
	in.Mechanism = "dcr"
	in.TokenEndpointAuthMethod = "client_secret_basic"
	in.Scopes = []string{"read", "write"}
	return ctx, ti, in, interactive
}
func assertPreparationInteractiveUntouched(t *testing.T, ctx context.Context, ti *testInstance, id, user uuid.UUID) {
	t.Helper()
	auth, _ := contextvalues.GetAuthContext(ctx)
	var external, secret string
	var grants, scopes []string
	require.NoError(t, ti.conn.QueryRow(ctx, `SELECT client_id,client_secret_encrypted,grant_types,scope FROM remote_session_clients WHERE id=$1 AND project_id=$2`, id, *auth.ProjectID).Scan(&external, &secret, &grants, &scopes))
	require.Equal(t, "downstream-client", external)
	require.Equal(t, "interactive-ciphertext", secret)
	require.Equal(t, []string{"authorization_code", "refresh_token"}, grants)
	require.Equal(t, []string{"openid"}, scopes)
	var attached int
	require.NoError(t, ti.conn.QueryRow(ctx, `SELECT count(*) FROM remote_session_client_user_session_issuers l JOIN remote_session_clients c ON c.id=l.remote_session_client_id WHERE c.project_id=$1 AND l.remote_session_client_id=$2 AND l.user_session_issuer_id=$3`, *auth.ProjectID, id, user).Scan(&attached))
	require.Equal(t, 1, attached)
}
func TestPreparationDCRIntegration_TimeoutRestartDoesNotReplay(t *testing.T) {
	t.Parallel()
	var posts atomic.Int32
	// An unread HTTP/1 request body can delay server-side disconnect detection.
	// Explicit release keeps fixture shutdown independent of that behavior.
	release := make(chan struct{})
	defer close(release)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(server.Close)
	ctx, ti, in, interactive := preparationDCRFixture(t, server.URL)
	timed, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	result, err := ti.service.PrepareIdentityChaining(timed, in)
	require.NoError(t, err)
	require.Equal(t, "indeterminate", result.State)
	require.Equal(t, int32(1), posts.Load())
	require.NotEqual(t, uuid.Nil, result.BindingID)
	restarted := restartPreparationService(t, ti)
	status, err := restarted.ReadIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "indeterminate", status.State)
	require.Equal(t, result.BindingID, status.BindingID)
	for range 3 {
		retry, err := restarted.PrepareIdentityChaining(ctx, in)
		require.NoError(t, err)
		require.Equal(t, "indeterminate", retry.State)
		require.Equal(t, result.Generation, retry.Generation)
	}
	require.Equal(t, int32(1), posts.Load())
	assertPreparationInteractiveUntouched(t, ctx, ti, interactive, in.UserSessionIssuerID)
	auth, _ := contextvalues.GetAuthContext(ctx)
	var count int
	require.NoError(t, ti.conn.QueryRow(ctx, `SELECT count(*) FROM remote_session_clients WHERE project_id=$1 AND remote_session_issuer_id=$2`, *auth.ProjectID, in.RemoteSessionIssuerID).Scan(&count))
	require.Equal(t, 1, count)
	// Simulate a process dying after the claim commit but before result commit.
	_, err = ti.conn.Exec(ctx, `UPDATE remote_session_ema_bindings SET state='in_progress', claimed_at=clock_timestamp()-interval '2 minutes' WHERE id=$1 AND project_id=$2`, result.BindingID, *auth.ProjectID)
	require.NoError(t, err)
	afterCrash, err := restartPreparationService(t, ti).PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "indeterminate", afterCrash.State)
	require.Equal(t, int32(1), posts.Load())
}
func TestPreparationDCRIntegration_EffectiveGrantsAndNarrowedScopePersist(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, grants, state string }{
		{"confirmed", `["urn:ietf:params:oauth:grant-type:jwt-bearer"]`, "ready"},
		{"unconfirmed", `null`, "unknown_grants"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var posts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				posts.Add(1)
				var request map[string]json.RawMessage
				require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
				require.JSONEq(t, `["urn:ietf:params:oauth:grant-type:jwt-bearer"]`, string(request["grant_types"]))
				require.JSONEq(t, `"client_secret_basic"`, string(request["token_endpoint_auth_method"]))
				require.NotContains(t, request, "client_secret")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"client_id":"chaining-client","client_secret":"downstream-secret","token_endpoint_auth_method":"client_secret_basic","scope":"read","grant_types":` + tc.grants + `}`))
			}))
			defer server.Close()
			ctx, ti, in, interactive := preparationDCRFixture(t, server.URL)
			result, err := ti.service.PrepareIdentityChaining(ctx, in)
			require.NoError(t, err)
			require.Equal(t, tc.state, result.State)
			require.Equal(t, "provider_returned", result.GrantSource)
			require.NotEqual(t, interactive, result.ClientID)
			require.NotEqual(t, uuid.Nil, result.ClientID)
			require.Equal(t, "chaining-client", result.ExternalClientID)
			require.Equal(t, []string{"read"}, result.Scopes)
			if tc.state == "ready" {
				require.Equal(t, []string{preparationJWTGrant}, result.GrantTypes)
			} else {
				require.Nil(t, result.GrantTypes)
			}
			wire, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(wire), "downstream-secret")
			auth, _ := contextvalues.GetAuthContext(ctx)
			var selected uuid.UUID
			var scopes, grants []string
			var ciphertext string
			require.NoError(t, ti.conn.QueryRow(ctx, `SELECT b.remote_session_client_id,b.requested_scopes,c.grant_types,c.client_secret_encrypted FROM remote_session_ema_bindings b JOIN remote_session_clients c ON c.id=b.remote_session_client_id WHERE b.id=$1 AND b.project_id=$2 AND b.organization_id=$3`, result.BindingID, *auth.ProjectID, auth.ActiveOrganizationID).Scan(&selected, &scopes, &grants, &ciphertext))
			require.Equal(t, result.ClientID, selected)
			require.Equal(t, result.Scopes, scopes)
			require.Equal(t, result.GrantTypes, grants)
			require.NotEmpty(t, ciphertext)
			require.NotEqual(t, "downstream-secret", ciphertext)
			restarted := restartPreparationService(t, ti)
			for range 2 {
				again, err := restarted.PrepareIdentityChaining(ctx, in)
				require.NoError(t, err)
				require.Equal(t, result, again)
			}
			require.Equal(t, int32(1), posts.Load())
			assertPreparationInteractiveUntouched(t, ctx, ti, interactive, in.UserSessionIssuerID)
		})
	}
}

func TestPreparationDCRIntegration_LifecycleTriggersRequireUnlink(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, err := ti.conn.Exec(ctx, `UPDATE user_session_issuers SET trusted_remote_session_issuer_id=$1 WHERE id=$2 AND project_id=$3`, in.RemoteSessionIssuerID, in.UserSessionIssuerID, *auth.ProjectID)
	require.NoError(t, err)
	// Unknown grants still install an explicit reference that lifecycle guards must honor.
	result, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "unknown_grants", result.State)
	statements := []struct {
		name, sql string
		id        uuid.UUID
	}{
		{"issuer tier move", `UPDATE remote_session_issuers SET project_id=NULL WHERE id=$1 AND project_id=$2`, in.RemoteSessionIssuerID},
		{"issuer identity", `UPDATE remote_session_issuers SET issuer='https://replacement.example.com' WHERE id=$1 AND project_id=$2`, in.RemoteSessionIssuerID},
		{"human issuer trust disconnect", `UPDATE user_session_issuers SET trusted_remote_session_issuer_id=NULL WHERE id=$1 AND project_id=$2`, in.UserSessionIssuerID},
		{"human issuer deletion", `UPDATE user_session_issuers SET deleted_at=clock_timestamp() WHERE id=$1 AND project_id=$2`, in.UserSessionIssuerID},
		{"client disconnect", `UPDATE remote_session_clients SET deleted_at=clock_timestamp() WHERE id=$1 AND project_id=$2`, in.ClientID},
	}
	for _, tc := range statements {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ti.conn.Exec(ctx, tc.sql, tc.id, *auth.ProjectID)
			var pgerr *pgconn.PgError
			require.ErrorAs(t, err, &pgerr)
			require.Equal(t, "23503", pgerr.Code)
		})
	}
	in.ExpectedGeneration = result.Generation
	unlinked, err := ti.service.UnlinkIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "unlinked", unlinked.State)
	require.Greater(t, unlinked.Generation, result.Generation)
	for _, tc := range statements {
		_, err := ti.conn.Exec(ctx, tc.sql, tc.id, *auth.ProjectID)
		require.NoError(t, err, tc.name)
	}
}

func TestPreparationDCRIntegration_UnlinkedTombstoneAllowsHardDelete(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"remote_session_issuers", "user_session_issuers", "remote_session_clients"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			ctx, ti, in := preparationFixture(t)
			auth, _ := contextvalues.GetAuthContext(ctx)
			result, err := ti.service.PrepareIdentityChaining(ctx, in)
			require.NoError(t, err)
			require.Equal(t, "unknown_grants", result.State)
			in.ExpectedGeneration = result.Generation
			_, err = ti.service.UnlinkIdentityChaining(ctx, in)
			require.NoError(t, err)
			// Delete the now-unlinked selected client first when deleting its issuer;
			// unrelated historical client/issuer FK rules remain unchanged by EMA.
			if kind == "remote_session_issuers" {
				_, err = ti.conn.Exec(ctx, `DELETE FROM remote_session_clients WHERE id=$1 AND project_id=$2`, in.ClientID, *auth.ProjectID)
				require.NoError(t, err)
			}
			id := in.ClientID
			if kind == "remote_session_issuers" {
				id = in.RemoteSessionIssuerID
			}
			if kind == "user_session_issuers" {
				id = in.UserSessionIssuerID
			}
			_, err = ti.conn.Exec(ctx, `DELETE FROM `+kind+` WHERE id=$1 AND project_id=$2`, id, *auth.ProjectID)
			require.NoError(t, err)
			var count int
			require.NoError(t, ti.conn.QueryRow(ctx, `SELECT count(*) FROM remote_session_ema_bindings WHERE id=$1 AND project_id=$2`, result.BindingID, *auth.ProjectID).Scan(&count))
			if kind != "remote_session_clients" {
				require.Zero(t, count)
			}
		})
	}
}
func TestPreparationDCRIntegration_StaleRebindIdentityAndConfirmationRetry(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, err := ti.conn.Exec(ctx, `UPDATE remote_session_clients SET token_endpoint_auth_method='client_secret_basic',client_secret_encrypted='test-ciphertext' WHERE id=$1 AND project_id=$2`, in.ClientID, *auth.ProjectID)
	require.NoError(t, err)
	in.ConfirmGrants = []string{preparationJWTGrant, "refresh_token"}
	first, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "ready", first.State)
	in.ConfirmGrants = []string{"refresh_token", preparationJWTGrant, preparationJWTGrant}
	repeated, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, first, repeated, "repeating equivalent administrator confirmation must not need or bump generation")
	replacement := seedProjectRemoteClientNoOrg(t, ctx, ti.conn, *auth.ProjectID, in.RemoteSessionIssuerID, "replacement-client")
	in.ClientID = replacement
	in.ConfirmGrants = nil
	in.ExpectedGeneration = 0
	stale, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "configuration_required", stale.State)
	require.Equal(t, uuid.Nil, stale.ClientID)
	require.NotEqual(t, "replacement-client", stale.ExternalClientID)
	require.Empty(t, stale.ExternalClientID, "a stale rebind must not pair old row identity with new external identity")
}

func TestPreparationDCRIntegration_CompletionRevalidatesReadiness(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, want string }{
		{"expired response", "manual_setup_required"},
		{"expires during lock wait", "manual_setup_required"},
		{"authentication method removed", "manual_setup_required"},
		{"discovery failed", "transient_failure"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var posts atomic.Int32
			var expires atomic.Int64
			requested, respond, responded := make(chan struct{}), make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				posts.Add(1)
				close(requested)
				<-respond
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"client_id": "recorded-resource-client", "client_secret": "recorded-resource-secret",
					"token_endpoint_auth_method": "client_secret_basic", "grant_types": []string{preparationJWTGrant},
					"client_secret_expires_at": expires.Load(),
				})
				w.(http.Flusher).Flush()
				close(responded)
			}))
			t.Cleanup(server.Close)
			t.Cleanup(func() {
				select {
				case <-respond:
				default:
					close(respond)
				}
			})
			ctx, ti, in, interactive := preparationDCRFixture(t, server.URL)
			ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			type outcome struct {
				result *remotesessions.PreparationResult
				err    error
			}
			completed := make(chan outcome, 1)
			go func() { result, err := ti.service.PrepareIdentityChaining(ctx, in); completed <- outcome{result, err} }()
			select {
			case <-requested:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			auth, _ := contextvalues.GetAuthContext(ctx)
			tx, err := ti.conn.Begin(ctx)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback(context.Background()) }()
			// Hold the row required by completion, after the durable claim and POST.
			_, err = tx.Exec(ctx, `SELECT id FROM remote_session_issuers WHERE id=$1 AND project_id=$2 FOR UPDATE`, in.RemoteSessionIssuerID, *auth.ProjectID)
			require.NoError(t, err)
			switch tc.name {
			case "expired response":
				expires.Store(time.Now().Add(-time.Minute).Unix())
			case "expires during lock wait":
				expires.Store(time.Now().Add(2 * time.Second).Unix())
			case "authentication method removed":
				_, err = tx.Exec(ctx, `UPDATE remote_session_issuers SET token_endpoint_auth_methods_supported=ARRAY['client_secret_post'] WHERE id=$1 AND project_id=$2`, in.RemoteSessionIssuerID, *auth.ProjectID)
			case "discovery failed":
				_, err = tx.Exec(ctx, `UPDATE remote_session_issuers SET metadata_last_error='discovery unavailable', metadata_last_error_at=clock_timestamp() WHERE id=$1 AND project_id=$2`, in.RemoteSessionIssuerID, *auth.ProjectID)
			}
			require.NoError(t, err)
			close(respond)
			select {
			case <-responded:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if tc.name == "expires during lock wait" {
				time.Sleep(time.Until(time.Unix(expires.Load(), 0)) + 50*time.Millisecond)
			}
			require.NoError(t, tx.Commit(ctx))
			var out outcome
			select {
			case out = <-completed:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			require.NoError(t, out.err)
			require.Equal(t, tc.want, out.result.State)
			require.Equal(t, "provider_returned", out.result.GrantSource)
			require.NotEqual(t, uuid.Nil, out.result.ClientID)
			require.Equal(t, "recorded-resource-client", out.result.ExternalClientID)
			require.Equal(t, []string{preparationJWTGrant}, out.result.GrantTypes)
			restarted := restartPreparationService(t, ti)
			read, err := restarted.ReadIdentityChaining(ctx, in)
			require.NoError(t, err)
			require.Equal(t, tc.want, read.State)
			require.Equal(t, out.result.ClientID, read.ClientID)
			retry, err := restarted.PrepareIdentityChaining(ctx, in)
			require.NoError(t, err)
			require.Equal(t, tc.want, retry.State)
			require.Equal(t, out.result.ClientID, retry.ClientID)
			require.Equal(t, int32(1), posts.Load(), "recorded registrations must never be replayed")
			assertPreparationInteractiveUntouched(t, ctx, ti, interactive, in.UserSessionIssuerID)
		})
	}
}
