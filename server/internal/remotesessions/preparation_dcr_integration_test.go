package remotesessions_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/stretchr/testify/assert"
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
	err := repo.New(ti.conn).SetPreparationFixtureDCREndpoint(ctx, repo.SetPreparationFixtureDCREndpointParams{Endpoint: conv.ToPGText(endpoint), ID: in.RemoteSessionIssuerID, ProjectID: conv.ToNullUUID(*auth.ProjectID)})
	require.NoError(t, err)
	// The existing interactive attachment and credentials are deliberately distinct.
	interactive := in.ClientID
	err = repo.New(ti.conn).SetPreparationFixtureInteractiveClient(ctx, repo.SetPreparationFixtureInteractiveClientParams{ID: interactive, ProjectID: conv.ToNullUUID(*auth.ProjectID)})
	require.NoError(t, err)
	err = repo.New(ti.conn).AttachPreparationFixtureInteractiveClient(ctx, repo.AttachPreparationFixtureInteractiveClientParams{ID: interactive, UserID: in.UserSessionIssuerID, ProjectID: conv.ToNullUUID(*auth.ProjectID)})
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
	record, err := repo.New(ti.conn).GetPreparationFixtureInteractiveClient(ctx, repo.GetPreparationFixtureInteractiveClientParams{ID: id, ProjectID: conv.ToNullUUID(*auth.ProjectID)})
	require.NoError(t, err)
	external := record.ClientID
	secret := record.ClientSecretEncrypted.String
	grants := record.GrantTypes
	scopes := record.Scope
	require.Equal(t, "downstream-client", external)
	require.Equal(t, "interactive-ciphertext", secret)
	require.Equal(t, []string{"authorization_code", "refresh_token"}, grants)
	require.Equal(t, []string{"openid"}, scopes)
	attached, err := repo.New(ti.conn).CountPreparationFixtureAttachments(ctx, repo.CountPreparationFixtureAttachmentsParams{ProjectID: conv.ToNullUUID(*auth.ProjectID), ID: id, UserID: user})
	require.NoError(t, err)
	require.EqualValues(t, 1, attached)
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
	count, err := repo.New(ti.conn).CountPreparationFixtureIssuerClients(ctx, repo.CountPreparationFixtureIssuerClientsParams{ProjectID: conv.ToNullUUID(*auth.ProjectID), IssuerID: in.RemoteSessionIssuerID})
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	// Simulate a process dying after the claim commit but before result commit.
	err = repo.New(ti.conn).AgePreparationFixtureClaim(ctx, repo.AgePreparationFixtureClaimParams{ID: result.BindingID, ProjectID: *auth.ProjectID})
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
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&request))
				assert.JSONEq(t, `["urn:ietf:params:oauth:grant-type:jwt-bearer"]`, string(request["grant_types"]))
				assert.JSONEq(t, `"client_secret_basic"`, string(request["token_endpoint_auth_method"]))
				assert.NotContains(t, request, "client_secret")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"client_id":"chaining-client","client_secret":"downstream-secret","token_endpoint_auth_method":"client_secret_basic","scope":"read","grant_types":` + tc.grants + `}`))
			}))
			t.Cleanup(server.Close)
			ctx, ti, in, interactive := preparationDCRFixture(t, server.URL)
			beforeAudit, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientCreate)
			require.NoError(t, err)
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
			wire, err := json.Marshal(result) //nolint:musttag // Verify the internal diagnostic cannot serialize credentials.
			require.NoError(t, err)
			require.NotContains(t, string(wire), "downstream-secret")
			auth, _ := contextvalues.GetAuthContext(ctx)
			record, err := repo.New(ti.conn).GetPreparationFixtureRegistration(ctx, repo.GetPreparationFixtureRegistrationParams{ID: result.BindingID, ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID})
			require.NoError(t, err)
			selected := record.RemoteSessionClientID.UUID
			scopes := record.RequestedScopes
			grants := record.GrantTypes
			ciphertext := record.ClientSecretEncrypted.String
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
			afterAudit, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientCreate)
			require.NoError(t, err)
			require.Equal(t, beforeAudit+1, afterAudit, "one durable DCR registration emits one create audit, including after idempotent retries")
			assertPreparationInteractiveUntouched(t, ctx, ti, interactive, in.UserSessionIssuerID)
		})
	}
}

func TestPreparationDCRIntegration_LifecycleTriggersRequireUnlink(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	err := repo.New(ti.conn).SetPreparationFixtureTrust(ctx, repo.SetPreparationFixtureTrustParams{IssuerID: conv.ToNullUUID(in.RemoteSessionIssuerID), ID: in.UserSessionIssuerID, ProjectID: conv.ToNullUUID(*auth.ProjectID)})
	require.NoError(t, err)
	// Unknown grants still install an explicit reference that lifecycle guards must honor.
	result, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "unknown_grants", result.State)
	statements := []struct {
		name string
		run  func(*repo.Queries) (int64, error)
	}{
		{"issuer tier move", func(q *repo.Queries) (int64, error) {
			return q.MovePreparationFixtureIssuerTier(ctx, repo.MovePreparationFixtureIssuerTierParams{ID: in.RemoteSessionIssuerID, ProjectID: conv.ToNullUUID(*auth.ProjectID)})
		}},
		{"issuer identity", func(q *repo.Queries) (int64, error) {
			return q.ChangePreparationFixtureIssuerIdentity(ctx, repo.ChangePreparationFixtureIssuerIdentityParams{ID: in.RemoteSessionIssuerID, ProjectID: conv.ToNullUUID(*auth.ProjectID)})
		}},
		{"human issuer trust disconnect", func(q *repo.Queries) (int64, error) {
			return q.DisconnectPreparationFixtureTrust(ctx, repo.DisconnectPreparationFixtureTrustParams{ID: in.UserSessionIssuerID, ProjectID: conv.ToNullUUID(*auth.ProjectID)})
		}},
		{"human issuer deletion", func(q *repo.Queries) (int64, error) {
			return q.SoftDeletePreparationFixtureUserIssuer(ctx, repo.SoftDeletePreparationFixtureUserIssuerParams{ID: in.UserSessionIssuerID, ProjectID: conv.ToNullUUID(*auth.ProjectID)})
		}},
		{"client disconnect", func(q *repo.Queries) (int64, error) {
			return q.SoftDeletePreparationFixtureClient(ctx, repo.SoftDeletePreparationFixtureClientParams{ID: in.ClientID, ProjectID: conv.ToNullUUID(*auth.ProjectID)})
		}},
	}
	for _, tc := range statements {
		_, err := tc.run(repo.New(ti.conn))
		var pgerr *pgconn.PgError
		require.ErrorAs(t, err, &pgerr, tc.name)
		require.Equal(t, "23503", pgerr.Code, tc.name)
	}
	in.ExpectedGeneration = result.Generation
	unlinked, err := ti.service.UnlinkIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "unlinked", unlinked.State)
	require.Greater(t, unlinked.Generation, result.Generation)
	for _, tc := range statements {
		tx := testenv.BeginTx(t, ctx, ti.conn)
		tag, err := tc.run(repo.New(tx))
		require.NoError(t, err, tc.name)
		require.EqualValues(t, 1, tag, tc.name)
		require.NoError(t, tx.Rollback(ctx))
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
				err = repo.New(ti.conn).DeletePreparationFixtureClient(ctx, repo.DeletePreparationFixtureClientParams{ID: in.ClientID, ProjectID: conv.ToNullUUID(*auth.ProjectID)})
				require.NoError(t, err)
			}
			id := in.ClientID
			if kind == "remote_session_issuers" {
				id = in.RemoteSessionIssuerID
			}
			if kind == "user_session_issuers" {
				id = in.UserSessionIssuerID
			}
			switch kind {
			case "remote_session_issuers":
				err = repo.New(ti.conn).DeletePreparationFixtureIssuer(ctx, repo.DeletePreparationFixtureIssuerParams{ID: id, ProjectID: conv.ToNullUUID(*auth.ProjectID)})
			case "user_session_issuers":
				err = repo.New(ti.conn).DeletePreparationFixtureUserIssuer(ctx, repo.DeletePreparationFixtureUserIssuerParams{ID: id, ProjectID: conv.ToNullUUID(*auth.ProjectID)})
			case "remote_session_clients":
				err = repo.New(ti.conn).DeletePreparationFixtureClient(ctx, repo.DeletePreparationFixtureClientParams{ID: id, ProjectID: conv.ToNullUUID(*auth.ProjectID)})
			}
			require.NoError(t, err)
			count, err := repo.New(ti.conn).CountPreparationFixtureBindingByID(ctx, repo.CountPreparationFixtureBindingByIDParams{ID: result.BindingID, ProjectID: *auth.ProjectID})
			require.NoError(t, err)
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
	err := repo.New(ti.conn).SetPreparationFixtureTestClientSecret(ctx, repo.SetPreparationFixtureTestClientSecretParams{ID: in.ClientID, ProjectID: conv.ToNullUUID(*auth.ProjectID)})
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
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
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
			tx := testenv.BeginTx(t, ctx, ti.conn)
			defer func() { _ = tx.Rollback(context.Background()) }()
			// Hold the row required by completion, after the durable claim and POST.
			_, err := repo.New(tx).LockPreparationFixtureIssuer(ctx, repo.LockPreparationFixtureIssuerParams{ID: in.RemoteSessionIssuerID, ProjectID: conv.ToNullUUID(*auth.ProjectID)})
			require.NoError(t, err)
			switch tc.name {
			case "expired response":
				expires.Store(time.Now().Add(-time.Minute).Unix())
			case "expires during lock wait":
				expires.Store(time.Now().Add(2 * time.Second).Unix())
			case "authentication method removed":
				err = repo.New(tx).SetPreparationFixtureIssuerPostAuth(ctx, repo.SetPreparationFixtureIssuerPostAuthParams{ID: in.RemoteSessionIssuerID, ProjectID: conv.ToNullUUID(*auth.ProjectID)})
			case "discovery failed":
				err = repo.New(tx).FailPreparationFixtureIssuerMetadata(ctx, repo.FailPreparationFixtureIssuerMetadataParams{ID: in.RemoteSessionIssuerID, ProjectID: conv.ToNullUUID(*auth.ProjectID)})
			}
			require.NoError(t, err)
			close(respond)
			select {
			case <-responded:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if tc.name == "expires during lock wait" {
				require.Eventually(t, func() bool { return time.Now().After(time.Unix(expires.Load(), 0)) }, 5*time.Second, 10*time.Millisecond)
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

func TestPreparationDCRIntegration_ExplicitRetryAfterRejection(t *testing.T) {
	t.Parallel()
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if posts.Add(1) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"client_id":"retry-client","client_secret":"retry-secret","token_endpoint_auth_method":"client_secret_basic","grant_types":["urn:ietf:params:oauth:grant-type:jwt-bearer"]}`))
	}))
	t.Cleanup(server.Close)
	ctx, ti, in, _ := preparationDCRFixture(t, server.URL)
	rejected, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "provider_rejection", rejected.State)
	unchanged, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, rejected.Generation, unchanged.Generation)
	require.EqualValues(t, 1, posts.Load())
	in.ExpectedGeneration = rejected.Generation
	ready, err := restartPreparationService(t, ti).PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "ready", ready.State)
	require.Greater(t, ready.Generation, rejected.Generation)
	require.EqualValues(t, 2, posts.Load())
}

func TestPreparationDCRIntegration_CIMDProvenanceRequiresGeneration(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	err := repo.New(ti.conn).EnablePreparationFixtureCIMD(ctx, repo.EnablePreparationFixtureCIMDParams{ID: in.RemoteSessionIssuerID, ProjectID: conv.ToNullUUID(*auth.ProjectID)})
	require.NoError(t, err)
	err = repo.New(ti.conn).SetPreparationFixtureCIMDURI(ctx, repo.SetPreparationFixtureCIMDURIParams{ID: in.ClientID, ProjectID: conv.ToNullUUID(*auth.ProjectID)})
	require.NoError(t, err)
	in.ConfirmGrants = []string{preparationJWTGrant}
	manual, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "ready", manual.State)
	in.ConfirmGrants = nil
	in.Mechanism = "cimd"
	stale, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "configuration_required", stale.State)
	require.Equal(t, manual.Generation, stale.Generation)
	require.Equal(t, "administrator_declared", stale.GrantSource)
	in.ExpectedGeneration = manual.Generation
	published, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "published_acceptance_unverified", published.State)
	require.Greater(t, published.Generation, manual.Generation)
	require.Equal(t, "cimd_published", published.GrantSource)
}

func TestPreparationDCRIntegration_UnlinkWaitsForDurableSubmission(t *testing.T) {
	t.Parallel()
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"client_id":"serialized-client","client_secret":"serialized-secret","token_endpoint_auth_method":"client_secret_basic","grant_types":["urn:ietf:params:oauth:grant-type:jwt-bearer"]}`))
	}))
	t.Cleanup(server.Close)
	ctx, ti, in, _ := preparationDCRFixture(t, server.URL)
	type outcome struct {
		result *remotesessions.PreparationResult
		err    error
	}
	finished := make(chan outcome, 1)
	go func() { r, err := ti.service.PrepareIdentityChaining(ctx, in); finished <- outcome{r, err} }()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("registration did not start")
	}
	// Another service cannot cancel the incarnation while its provider write is
	// in flight. Its lock wait must time out, not unlink and orphan the response.
	waitCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	_, err := restartPreparationService(t, ti).UnlinkIdentityChaining(waitCtx, in)
	require.Error(t, err)
	releaseOnce.Do(func() { close(release) })
	completed := <-finished
	require.NoError(t, completed.err)
	require.Equal(t, "ready", completed.result.State)
	require.NotEqual(t, uuid.Nil, completed.result.ClientID)
	in.ExpectedGeneration = completed.result.Generation
	unlinked, err := ti.service.UnlinkIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "unlinked", unlinked.State)
	require.Equal(t, completed.result.Generation+1, unlinked.Generation)
	// The successful registration remains available for explicit reconciliation.
	auth, _ := contextvalues.GetAuthContext(ctx)
	count, err := repo.New(ti.conn).CountPreparationFixtureIssuerClients(ctx, repo.CountPreparationFixtureIssuerClientsParams{ProjectID: conv.ToNullUUID(*auth.ProjectID), IssuerID: in.RemoteSessionIssuerID})
	require.NoError(t, err)
	require.EqualValues(t, 2, count)
}
