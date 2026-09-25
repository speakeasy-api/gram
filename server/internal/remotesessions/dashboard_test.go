package remotesessions_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestCommitServerIdentityConfigurationManualCreatesConfigurationAtomically(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	targetID, userIssuerID := createServerIdentityTarget(t, ctx, ti, "manual-target")

	issuerAuditBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerCreate)
	require.NoError(t, err)
	clientAuditBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientCreate)
	require.NoError(t, err)

	result, err := ti.service.CommitServerIdentityConfiguration(ctx, &gen.CommitServerIdentityConfigurationPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		McpServerID:      targetID.String(),
		ProviderID:       nil,
		CreateProvider:   serverIdentityProviderForm("manual-provider", nil, false),
		ClientMode:       "manual",
		ExistingClientID: nil,
		ClientConfiguration: &gen.ServerIdentityClientConfiguration{
			ClientID:                conv.PtrEmpty("manual-client"),
			ClientSecret:            conv.PtrEmpty("manual-secret"),
			TokenEndpointAuthMethod: conv.PtrEmpty("client_secret_basic"),
			Scope:                   []string{"openid", "profile"},
			Audience:                conv.PtrEmpty("https://mcp.example.com"),
		},
	})
	require.NoError(t, err)
	require.Equal(t, "registered", *result.Status)
	require.Equal(t, "manual", *result.RegistrationMethod)
	require.False(t, result.ManualSetupRequired)
	require.Equal(t, "project-specific", *result.ProviderTier)
	require.Equal(t, "project-specific", *result.ClientTier)
	require.Equal(t, []string{userIssuerID.String()}, result.Client.UserSessionIssuerIds)
	require.Equal(t, "/remote-identity-providers/"+result.Provider.ID, *result.ProviderPath)
	require.Equal(t, *result.ProviderPath+"/clients/"+result.Client.ID, *result.ClientPath)

	clientUUID, err := uuid.Parse(result.Client.ID)
	require.NoError(t, err)
	storedClient, err := repo.New(ti.conn).GetRemoteSessionClientByID(ctx, repo.GetRemoteSessionClientByIDParams{
		ProjectID:      projectIDFromContext(t, ctx),
		OrganizationID: activeOrganizationID(t, ctx),
		ID:             clientUUID,
	})
	require.NoError(t, err)
	require.True(t, storedClient.RemoteSessionClient.ClientSecretEncrypted.Valid)
	require.NotEqual(t, "manual-secret", storedClient.RemoteSessionClient.ClientSecretEncrypted.String)

	storedTarget, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID:        targetID,
		ProjectID: projectIDFromContext(t, ctx),
	})
	require.NoError(t, err)
	require.True(t, storedTarget.RemoteSessionIssuerID.Valid)
	require.Equal(t, result.Provider.ID, storedTarget.RemoteSessionIssuerID.UUID.String())

	issuerAuditAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerCreate)
	require.NoError(t, err)
	clientAuditAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientCreate)
	require.NoError(t, err)
	require.Equal(t, issuerAuditBefore+1, issuerAuditAfter)
	require.Equal(t, clientAuditBefore+1, clientAuditAfter)
}

func TestCommitServerIdentityConfigurationAutoPrefersCIMD(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	targetID, userIssuerID := createServerIdentityTarget(t, ctx, ti, "cimd-target")
	providerID := createServerIdentityProvider(t, ctx, ti, "cimd-provider", "https://registration.invalid", true, []string{"none", "client_secret_basic"})

	result, err := ti.service.CommitServerIdentityConfiguration(ctx, autoServerIdentityPayload(targetID, providerID))
	require.NoError(t, err)
	require.Equal(t, "registered", *result.Status)
	require.Equal(t, "cimd", *result.RegistrationMethod)
	require.Equal(t, "none", *result.Client.TokenEndpointAuthMethod)
	require.Equal(t, []string{userIssuerID.String()}, result.Client.UserSessionIssuerIds)
	require.NotNil(t, result.Client.ClientIDMetadataURI)
	require.Equal(t, *result.Client.ClientIDMetadataURI, result.Client.ClientID)
}

func TestCommitServerIdentityConfigurationAutoRegistersOnceWithDCR(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	targetID, _ := createServerIdentityTarget(t, ctx, ti, "dcr-target")
	var requests atomic.Int64
	registrationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		var body struct {
			Scope                   string `json:"scope"`
			TokenEndpointAuthMethod string `json:"token_endpoint_auth_method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if body.Scope != "openid profile" || body.TokenEndpointAuthMethod != "client_secret_post" {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"client_id":                  "registered-client",
			"client_secret":              "registered-secret",
			"token_endpoint_auth_method": "client_secret_post",
		})
	}))
	t.Cleanup(registrationServer.Close)

	providerID := createServerIdentityProvider(t, ctx, ti, "dcr-provider", registrationServer.URL, false, []string{"client_secret_post"})
	payload := autoServerIdentityPayload(targetID, providerID)
	payload.ClientConfiguration.Scope = []string{"openid", "profile"}
	payload.ClientConfiguration.TokenEndpointAuthMethod = conv.PtrEmpty("client_secret_post")

	result, err := ti.service.CommitServerIdentityConfiguration(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, int64(1), requests.Load())
	require.Equal(t, "registered", *result.Status)
	require.Equal(t, "dcr", *result.RegistrationMethod)
	require.Equal(t, "registered-client", result.Client.ClientID)
	require.Equal(t, "client_secret_post", *result.Client.TokenEndpointAuthMethod)

	clientID, err := uuid.Parse(result.Client.ID)
	require.NoError(t, err)
	stored, err := repo.New(ti.conn).GetRemoteSessionClientByID(ctx, repo.GetRemoteSessionClientByIDParams{
		ProjectID:      projectIDFromContext(t, ctx),
		OrganizationID: activeOrganizationID(t, ctx),
		ID:             clientID,
	})
	require.NoError(t, err)
	require.True(t, stored.RemoteSessionClient.ClientSecretEncrypted.Valid)
	require.NotEqual(t, "registered-secret", stored.RemoteSessionClient.ClientSecretEncrypted.String)
}

func TestCommitServerIdentityConfigurationReturnsDCRRefusalWithoutChangingState(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	targetID, _ := createServerIdentityTarget(t, ctx, ti, "dcr-refusal-target")
	registrationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_client_metadata","error_description":"bad metadata"}`))
	}))
	t.Cleanup(registrationServer.Close)
	providerID := createServerIdentityProvider(t, ctx, ti, "dcr-refusal-provider", registrationServer.URL, false, []string{"client_secret_basic"})

	result, err := ti.service.CommitServerIdentityConfiguration(ctx, autoServerIdentityPayload(targetID, providerID))
	require.NoError(t, err)
	require.Nil(t, result.Status)
	require.Equal(t, "dcr", *result.RegistrationMethod)
	require.False(t, result.ManualSetupRequired)
	require.Equal(t, "refused", result.Failure.Outcome)
	require.Equal(t, "authorization_rejected", result.Failure.Reason)
	require.False(t, result.Failure.Retryable)
	require.Equal(t, http.StatusBadRequest, *result.Failure.HTTPStatus)
	clients, err := repo.New(ti.conn).CountRemoteSessionClientsByIssuerID(ctx, providerID)
	require.NoError(t, err)
	require.Zero(t, clients)
}

func TestCommitServerIdentityConfigurationValidatesPlanBeforeDCR(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	targetID, _ := createServerIdentityTarget(t, ctx, ti, "invalid-plan-target")
	var requests atomic.Int64
	registrationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(registrationServer.Close)
	providerID := createServerIdentityProvider(t, ctx, ti, "invalid-plan-provider", registrationServer.URL, false, []string{"client_secret_basic"})
	payload := autoServerIdentityPayload(targetID, providerID)
	payload.ExistingClientID = conv.PtrEmpty(uuid.NewString())

	_, err := ti.service.CommitServerIdentityConfiguration(ctx, payload)
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.Zero(t, requests.Load())
}

func TestCommitServerIdentityConfigurationRefusesPaddedDuplicateSlugBeforeDCR(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	targetID, _ := createServerIdentityTarget(t, ctx, ti, "padded-slug-target")
	var requests atomic.Int64
	registrationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(registrationServer.Close)
	createServerIdentityProvider(t, ctx, ti, "padded-slug-provider", registrationServer.URL, false, []string{"client_secret_basic"})
	payload := autoServerIdentityPayload(targetID, uuid.Nil)
	payload.ProviderID = nil
	payload.CreateProvider = serverIdentityProviderForm("padded-slug-new-issuer", conv.PtrEmpty(registrationServer.URL), false)
	payload.CreateProvider.Slug = "  padded-slug-provider  "

	_, err := ti.service.CommitServerIdentityConfiguration(ctx, payload)
	requireOopsCode(t, err, oops.CodeConflict)
	require.Zero(t, requests.Load())
}

func TestCommitServerIdentityConfigurationDCRAuditFailureRollsBackWithoutRetry(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	targetID, _ := createServerIdentityTarget(t, ctx, ti, "dcr-rollback-target")
	var requests atomic.Int64
	registrationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"client_id":                  "rollback-client",
			"client_secret":              "rollback-secret",
			"token_endpoint_auth_method": "client_secret_basic",
		})
	}))
	t.Cleanup(registrationServer.Close)
	providerID := createServerIdentityProvider(t, ctx, ti, "dcr-rollback-provider", registrationServer.URL, false, []string{"client_secret_basic"})
	testenv.RejectWritesTo(t, ctx, ti.conn, "audit_logs")

	_, err := ti.service.CommitServerIdentityConfiguration(ctx, autoServerIdentityPayload(targetID, providerID))
	requireOopsCode(t, err, oops.CodeUnexpected)
	require.Equal(t, int64(1), requests.Load(), "a local failure after DCR must not retry or compensate upstream")
	clients, err := repo.New(ti.conn).CountRemoteSessionClientsByIssuerID(ctx, providerID)
	require.NoError(t, err)
	require.Zero(t, clients, "the client insert must roll back with its audit event")
	target, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID:        targetID,
		ProjectID: projectIDFromContext(t, ctx),
	})
	require.NoError(t, err)
	require.False(t, target.RemoteSessionIssuerID.Valid, "derived MCP identity state must roll back too")
}

func TestCommitServerIdentityConfigurationAutoRequiresManualSetupWithoutChangingState(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	targetID, _ := createServerIdentityTarget(t, ctx, ti, "fallback-target")
	providerID := createServerIdentityProvider(t, ctx, ti, "fallback-provider", "", false, []string{"client_secret_basic"})
	clientsBefore, err := repo.New(ti.conn).CountRemoteSessionClientsByIssuerID(ctx, providerID)
	require.NoError(t, err)

	result, err := ti.service.CommitServerIdentityConfiguration(ctx, autoServerIdentityPayload(targetID, providerID))
	require.NoError(t, err)
	require.True(t, result.ManualSetupRequired)
	require.Nil(t, result.Status)
	require.Nil(t, result.RegistrationMethod)
	require.Nil(t, result.Client)
	require.Equal(t, providerID.String(), result.Provider.ID)

	clientsAfter, err := repo.New(ti.conn).CountRemoteSessionClientsByIssuerID(ctx, providerID)
	require.NoError(t, err)
	require.Equal(t, clientsBefore, clientsAfter)
	storedTarget, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID:        targetID,
		ProjectID: projectIDFromContext(t, ctx),
	})
	require.NoError(t, err)
	require.False(t, storedTarget.RemoteSessionIssuerID.Valid)
}

func TestCommitServerIdentityConfigurationExistingClientNeedsOnlyMCPWrite(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	targetID, userIssuerID := createServerIdentityTarget(t, ctx, ti, "existing-target")
	providerID := createServerIdentityProvider(t, ctx, ti, "existing-provider", "", false, []string{"client_secret_basic"})
	client, err := repo.New(ti.conn).CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ProjectID:             conv.ToNullUUID(projectIDFromContext(t, ctx)),
		OrganizationID:        conv.ToPGText(activeOrganizationID(t, ctx)),
		RemoteSessionIssuerID: providerID,
		ClientID:              "existing-client",
		ClientIDIssuedAt:      conv.ToPGTimestamptz(time.Now().UTC()),
	})
	require.NoError(t, err)
	restrictedCtx := withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, targetID.String()))

	result, err := ti.service.CommitServerIdentityConfiguration(restrictedCtx, &gen.CommitServerIdentityConfigurationPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		McpServerID:         targetID.String(),
		ProviderID:          conv.PtrEmpty(providerID.String()),
		CreateProvider:      nil,
		ClientMode:          "existing",
		ExistingClientID:    conv.PtrEmpty(client.ID.String()),
		ClientConfiguration: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "linked", *result.Status)
	require.Equal(t, "existing", *result.RegistrationMethod)
	require.Equal(t, []string{userIssuerID.String()}, result.Client.UserSessionIssuerIds)

	_, err = ti.service.CommitServerIdentityConfiguration(restrictedCtx, &gen.CommitServerIdentityConfigurationPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		McpServerID:      targetID.String(),
		ProviderID:       nil,
		CreateProvider:   serverIdentityProviderForm("denied-provider", nil, false),
		ClientMode:       "manual",
		ExistingClientID: nil,
		ClientConfiguration: &gen.ServerIdentityClientConfiguration{
			ClientID:                conv.PtrEmpty("denied-client"),
			ClientSecret:            nil,
			TokenEndpointAuthMethod: conv.PtrEmpty("none"),
			Scope:                   nil,
			Audience:                nil,
		},
	})
	requireOopsCode(t, err, oops.CodeForbidden)

	projectOnlyCtx := withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeProjectWrite, projectIDFromContext(t, ctx).String()))
	_, err = ti.service.CommitServerIdentityConfiguration(projectOnlyCtx, &gen.CommitServerIdentityConfigurationPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		McpServerID:         targetID.String(),
		ProviderID:          conv.PtrEmpty(providerID.String()),
		CreateProvider:      nil,
		ClientMode:          "existing",
		ExistingClientID:    conv.PtrEmpty(client.ID.String()),
		ClientConfiguration: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestCommitServerIdentityConfigurationAllowsDuplicateClientIDs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	firstTargetID, _ := createServerIdentityTarget(t, ctx, ti, "duplicate-client-first")
	secondTargetID, _ := createServerIdentityTarget(t, ctx, ti, "duplicate-client-second")
	providerID := createServerIdentityProvider(t, ctx, ti, "duplicate-client-provider", "", false, []string{"none"})

	create := func(targetID uuid.UUID) *gen.CommitServerIdentityConfigurationPayload {
		return &gen.CommitServerIdentityConfigurationPayload{
			SessionToken:     nil,
			ApikeyToken:      nil,
			ProjectSlugInput: nil,
			McpServerID:      targetID.String(),
			ProviderID:       conv.PtrEmpty(providerID.String()),
			CreateProvider:   nil,
			ClientMode:       "manual",
			ExistingClientID: nil,
			ClientConfiguration: &gen.ServerIdentityClientConfiguration{
				ClientID:                conv.PtrEmpty("shared-upstream-client-id"),
				ClientSecret:            nil,
				TokenEndpointAuthMethod: conv.PtrEmpty("none"),
				Scope:                   nil,
				Audience:                nil,
			},
		}
	}

	first, err := ti.service.CommitServerIdentityConfiguration(ctx, create(firstTargetID))
	require.NoError(t, err)
	second, err := ti.service.CommitServerIdentityConfiguration(ctx, create(secondTargetID))
	require.NoError(t, err)
	require.NotEqual(t, first.Client.ID, second.Client.ID)
	require.Equal(t, first.Client.ClientID, second.Client.ClientID)
}

func TestCommitServerIdentityConfigurationReplacesCurrentClientAtomically(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	targetID, userIssuerID := createServerIdentityTarget(t, ctx, ti, "replace-target")

	initial, err := ti.service.CommitServerIdentityConfiguration(ctx, &gen.CommitServerIdentityConfigurationPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		McpServerID:      targetID.String(),
		ProviderID:       nil,
		CreateProvider:   serverIdentityProviderForm("replace-initial-provider", nil, false),
		ClientMode:       "manual",
		ExistingClientID: nil,
		ClientConfiguration: &gen.ServerIdentityClientConfiguration{
			ClientID:                conv.PtrEmpty("replace-initial-client"),
			ClientSecret:            nil,
			TokenEndpointAuthMethod: conv.PtrEmpty("none"),
			Scope:                   nil,
			Audience:                nil,
		},
	})
	require.NoError(t, err)
	initialClientID, err := uuid.Parse(initial.Client.ID)
	require.NoError(t, err)

	replacementProviderID := createServerIdentityProvider(t, ctx, ti, "replace-next-provider", "", false, []string{"none"})
	replacementClient, err := repo.New(ti.conn).CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ProjectID:             conv.ToNullUUID(projectIDFromContext(t, ctx)),
		OrganizationID:        conv.ToPGText(activeOrganizationID(t, ctx)),
		RemoteSessionIssuerID: replacementProviderID,
		ClientID:              "replace-next-client",
		ClientIDIssuedAt:      conv.ToPGTimestamptz(time.Now().UTC()),
	})
	require.NoError(t, err)
	detachAuditBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientDetachUserSessionIssuer)
	require.NoError(t, err)

	result, err := ti.service.CommitServerIdentityConfiguration(ctx, &gen.CommitServerIdentityConfigurationPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		McpServerID:         targetID.String(),
		ProviderID:          conv.PtrEmpty(replacementProviderID.String()),
		CreateProvider:      nil,
		ClientMode:          "existing",
		ExistingClientID:    conv.PtrEmpty(replacementClient.ID.String()),
		ClientConfiguration: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "linked", *result.Status)

	oldClient, err := repo.New(ti.conn).GetRemoteSessionClientByID(ctx, repo.GetRemoteSessionClientByIDParams{
		ProjectID:      projectIDFromContext(t, ctx),
		OrganizationID: activeOrganizationID(t, ctx),
		ID:             initialClientID,
	})
	require.NoError(t, err)
	require.NotContains(t, oldClient.UserSessionIssuerIds, userIssuerID)
	newClient, err := repo.New(ti.conn).GetRemoteSessionClientByID(ctx, repo.GetRemoteSessionClientByIDParams{
		ProjectID:      projectIDFromContext(t, ctx),
		OrganizationID: activeOrganizationID(t, ctx),
		ID:             replacementClient.ID,
	})
	require.NoError(t, err)
	require.Contains(t, newClient.UserSessionIssuerIds, userIssuerID)
	storedTarget, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID:        targetID,
		ProjectID: projectIDFromContext(t, ctx),
	})
	require.NoError(t, err)
	require.Equal(t, replacementProviderID, storedTarget.RemoteSessionIssuerID.UUID)
	detachAuditAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientDetachUserSessionIssuer)
	require.NoError(t, err)
	require.Equal(t, detachAuditBefore+1, detachAuditAfter)
}

func TestCommitServerIdentityConfigurationLocksUserSessionIssuerBeforeReplacingBindings(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	targetID, userIssuerID := createServerIdentityTarget(t, ctx, ti, "replace-binding-lock")
	providerID := createServerIdentityProvider(t, ctx, ti, "replace-binding-lock-provider", "", false, []string{"none"})
	client, err := repo.New(ti.conn).CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ProjectID:             conv.ToNullUUID(projectIDFromContext(t, ctx)),
		OrganizationID:        conv.ToPGText(activeOrganizationID(t, ctx)),
		RemoteSessionIssuerID: providerID,
		ClientID:              "replace-binding-lock-client",
		ClientIDIssuedAt:      conv.ToPGTimestamptz(time.Now().UTC()),
	})
	require.NoError(t, err)

	tx := testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, usersessionsrepo.New(tx).LockUserSessionIssuerForOwnerBinding(ctx, userIssuerID))

	done := make(chan error, 1)
	go func() {
		_, err := ti.service.CommitServerIdentityConfiguration(ctx, &gen.CommitServerIdentityConfigurationPayload{
			SessionToken:        nil,
			ApikeyToken:         nil,
			ProjectSlugInput:    nil,
			McpServerID:         targetID.String(),
			ProviderID:          conv.PtrEmpty(providerID.String()),
			CreateProvider:      nil,
			ClientMode:          "existing",
			ExistingClientID:    conv.PtrEmpty(client.ID.String()),
			ClientConfiguration: nil,
		})
		done <- err
	}()

	testenv.WaitForBlockedBackend(t, ctx, ti.conn)
	select {
	case err := <-done:
		require.Fail(t, "atomic configuration completed while another transaction held the user-session-issuer binding lock", "%v", err)
	default:
	}
	require.NoError(t, tx.Rollback(ctx))
	require.Eventually(t, func() bool { return len(done) > 0 }, 30*time.Second, 25*time.Millisecond,
		"atomic configuration did not complete after the user-session-issuer binding lock was released")
	require.NoError(t, <-done)
}

func createServerIdentityTarget(t *testing.T, ctx context.Context, ti *testInstance, slug string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, slug+"-issuer")
	return createServerIdentityTargetOnIssuer(t, ctx, ti, slug, userIssuerID), userIssuerID
}

func createServerIdentityTargetOnIssuer(t *testing.T, ctx context.Context, ti *testInstance, slug string, userIssuerID uuid.UUID) uuid.UUID {
	t.Helper()
	projectID := projectIDFromContext(t, ctx)
	remote, err := remotemcprepo.New(ti.conn).CreateServer(ctx, remotemcprepo.CreateServerParams{
		ID:            uuid.New(),
		ProjectID:     projectID,
		Name:          conv.ToPGText(slug),
		Slug:          conv.ToPGText(slug),
		TransportType: "streamable-http",
		Url:           "https://mcp.example.com/" + slug,
	})
	require.NoError(t, err)
	target, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           projectID,
		Name:                conv.ToPGText(slug),
		Slug:                conv.ToPGText(slug),
		UserSessionIssuerID: conv.ToNullUUID(userIssuerID),
		RemoteMcpServerID:   conv.ToNullUUID(remote.ID),
		Visibility:          "private",
	})
	require.NoError(t, err)
	return target.ID
}

func createServerIdentityProvider(t *testing.T, ctx context.Context, ti *testInstance, slug, registrationEndpoint string, cimd bool, authMethods []string) uuid.UUID {
	t.Helper()
	provider, err := repo.New(ti.conn).CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         conv.ToNullUUID(projectIDFromContext(t, ctx)),
		OrganizationID:                    conv.ToPGText(activeOrganizationID(t, ctx)),
		Slug:                              slug,
		Issuer:                            "https://idp.example.com/" + slug,
		AuthorizationEndpoint:             conv.ToPGText("https://idp.example.com/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://idp.example.com/token"),
		RegistrationEndpoint:              conv.ToPGTextEmpty(registrationEndpoint),
		ScopesSupported:                   []string{},
		GrantTypesSupported:               []string{},
		ResponseTypesSupported:            []string{},
		TokenEndpointAuthMethodsSupported: authMethods,
		ClientIDMetadataDocumentSupported: cimd,
	})
	require.NoError(t, err)
	return provider.ID
}

func autoServerIdentityPayload(targetID, providerID uuid.UUID) *gen.CommitServerIdentityConfigurationPayload {
	return &gen.CommitServerIdentityConfigurationPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		McpServerID:      targetID.String(),
		ProviderID:       conv.PtrEmpty(providerID.String()),
		CreateProvider:   nil,
		ClientMode:       "auto",
		ExistingClientID: nil,
		ClientConfiguration: &gen.ServerIdentityClientConfiguration{
			ClientID:                nil,
			ClientSecret:            nil,
			TokenEndpointAuthMethod: nil,
			Scope:                   nil,
			Audience:                nil,
		},
	}
}

func serverIdentityProviderForm(slug string, registrationEndpoint *string, cimd bool) *gen.CreateRemoteSessionIssuerForm {
	return &gen.CreateRemoteSessionIssuerForm{
		Slug:                              slug,
		Issuer:                            "https://idp.example.com/" + slug,
		Name:                              nil,
		LogoAssetID:                       nil,
		ClientSetupDocumentationURL:       nil,
		AuthorizationEndpoint:             conv.PtrEmpty("https://idp.example.com/authorize"),
		TokenEndpoint:                     conv.PtrEmpty("https://idp.example.com/token"),
		RevocationEndpoint:                nil,
		RegistrationEndpoint:              registrationEndpoint,
		JwksURI:                           nil,
		ServiceDocumentation:              nil,
		OpPolicyURI:                       nil,
		OpTosURI:                          nil,
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic", "none"},
		CodeChallengeMethodsSupported:     []string{"S256"},
		Oidc:                              nil,
		Passthrough:                       nil,
		ClientIDMetadataDocumentSupported: &cimd,
		UserinfoEndpoint:                  nil,
		IntrospectionEndpoint:             nil,
		IntrospectionEndpointAuthMethodsSupported:  nil,
		IDTokenSigningAlgValuesSupported:           nil,
		ClaimsSupported:                            nil,
		BackchannelLogoutSupported:                 nil,
		AuthorizationResponseIssParameterSupported: nil,
		ScopeOverride:                              nil,
		ResourceIndicatorSupported:                 nil,
	}
}

func projectIDFromContext(t *testing.T, ctx context.Context) uuid.UUID {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	return *authCtx.ProjectID
}

func TestCommitServerIdentityConfigurationRequiresWriteOnEveryServerSharingTheIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectID := projectIDFromContext(t, ctx)
	targetID, userIssuerID := createServerIdentityTarget(t, ctx, ti, "shared-issuer-target")

	// A second MCP server on the same user session issuer. The client binding
	// this RPC rewrites is keyed by issuer, not by server, so committing on the
	// target re-stamps this sibling too.
	siblingRemote, err := remotemcprepo.New(ti.conn).CreateServer(ctx, remotemcprepo.CreateServerParams{
		ID:            uuid.New(),
		ProjectID:     projectID,
		Name:          conv.ToPGText("shared-issuer-sibling"),
		Slug:          conv.ToPGText("shared-issuer-sibling"),
		TransportType: "streamable-http",
		Url:           "https://mcp.example.com/shared-issuer-sibling",
	})
	require.NoError(t, err)
	sibling, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           projectID,
		Name:                conv.ToPGText("shared-issuer-sibling"),
		Slug:                conv.ToPGText("shared-issuer-sibling"),
		UserSessionIssuerID: conv.ToNullUUID(userIssuerID),
		RemoteMcpServerID:   conv.ToNullUUID(siblingRemote.ID),
		Visibility:          "private",
	})
	require.NoError(t, err)

	providerID := createServerIdentityProvider(t, ctx, ti, "shared-issuer-provider", "", false, []string{"client_secret_basic"})
	client, err := repo.New(ti.conn).CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ProjectID:             conv.ToNullUUID(projectID),
		OrganizationID:        conv.ToPGText(activeOrganizationID(t, ctx)),
		RemoteSessionIssuerID: providerID,
		ClientID:              "shared-issuer-client",
		ClientIDIssuedAt:      conv.ToPGTimestamptz(time.Now().UTC()),
	})
	require.NoError(t, err)

	payload := func() *gen.CommitServerIdentityConfigurationPayload {
		return &gen.CommitServerIdentityConfigurationPayload{
			SessionToken:        nil,
			ApikeyToken:         nil,
			ProjectSlugInput:    nil,
			McpServerID:         targetID.String(),
			ProviderID:          conv.PtrEmpty(providerID.String()),
			CreateProvider:      nil,
			ClientMode:          "existing",
			ExistingClientID:    conv.PtrEmpty(client.ID.String()),
			ClientConfiguration: nil,
		}
	}

	targetOnlyCtx := withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, targetID.String()))
	_, err = ti.service.CommitServerIdentityConfiguration(targetOnlyCtx, payload())
	requireOopsCode(t, err, oops.CodeForbidden)

	bothCtx := withExactAccessGrants(t, ctx, ti.conn,
		authz.NewGrant(authz.ScopeMCPWrite, targetID.String()),
		authz.NewGrant(authz.ScopeMCPWrite, sibling.ID.String()),
	)
	result, err := ti.service.CommitServerIdentityConfiguration(bothCtx, payload())
	require.NoError(t, err)
	require.Equal(t, "linked", *result.Status)
	require.Equal(t, []string{userIssuerID.String()}, result.Client.UserSessionIssuerIds)
}

func TestCommitServerIdentityConfigurationLocksProviderBeforeCommitting(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	targetID, _ := createServerIdentityTarget(t, ctx, ti, "provider-lock")
	providerID := createServerIdentityProvider(t, ctx, ti, "provider-lock-provider", "", false, []string{"none"})
	projectID := projectIDFromContext(t, ctx)
	client, err := repo.New(ti.conn).CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ProjectID:             conv.ToNullUUID(projectID),
		OrganizationID:        conv.ToPGText(activeOrganizationID(t, ctx)),
		RemoteSessionIssuerID: providerID,
		ClientID:              "provider-lock-client",
		ClientIDIssuedAt:      conv.ToPGTimestamptz(time.Now().UTC()),
	})
	require.NoError(t, err)

	// Stand in for a concurrent UpdateRemoteSessionIssuer mid-edit. This must be
	// an UPDATE, not SELECT ... FOR UPDATE: an UPDATE that leaves the key alone
	// takes FOR NO KEY UPDATE, which does NOT conflict with the FOR KEY SHARE
	// that the client insert's foreign key takes on this row. So only the
	// handler's own FOR UPDATE read can block here, and a SELECT ... FOR UPDATE
	// stand-in would block the insert on the foreign key instead and pass
	// whether or not the handler locks anything.
	tx := testenv.BeginTx(t, ctx, ti.conn)
	_, err = repo.New(tx).UpdateRemoteSessionIssuer(ctx, repo.UpdateRemoteSessionIssuerParams{
		ID:        providerID,
		ProjectID: conv.ToNullUUID(projectID),
		// Every narg left NULL, so this keeps all values and only takes the
		// row lock -- the state a real edit is in between its UPDATE and COMMIT.
		Slug:                              pgtype.Text{String: "", Valid: false},
		Issuer:                            pgtype.Text{String: "", Valid: false},
		Name:                              pgtype.Text{String: "", Valid: false},
		LogoAssetID:                       pgtype.Text{String: "", Valid: false},
		ClientSetupDocumentationUrl:       pgtype.Text{String: "", Valid: false},
		AuthorizationEndpoint:             pgtype.Text{String: "", Valid: false},
		TokenEndpoint:                     pgtype.Text{String: "", Valid: false},
		RevocationEndpoint:                pgtype.Text{String: "", Valid: false},
		RegistrationEndpoint:              pgtype.Text{String: "", Valid: false},
		JwksUri:                           pgtype.Text{String: "", Valid: false},
		ServiceDocumentation:              pgtype.Text{String: "", Valid: false},
		OpPolicyUri:                       pgtype.Text{String: "", Valid: false},
		OpTosUri:                          pgtype.Text{String: "", Valid: false},
		ScopesSupported:                   nil,
		GrantTypesSupported:               nil,
		ResponseTypesSupported:            nil,
		TokenEndpointAuthMethodsSupported: nil,
		CodeChallengeMethodsSupported:     nil,
		ClientIDMetadataDocumentSupported: pgtype.Bool{Bool: false, Valid: false},
		UserinfoEndpoint:                  pgtype.Text{String: "", Valid: false},
		IntrospectionEndpoint:             pgtype.Text{String: "", Valid: false},
		IntrospectionEndpointAuthMethodsSupported:  nil,
		IDTokenSigningAlgValuesSupported:           nil,
		ClaimsSupported:                            nil,
		BackchannelLogoutSupported:                 pgtype.Bool{Bool: false, Valid: false},
		AuthorizationResponseIssParameterSupported: pgtype.Bool{Bool: false, Valid: false},
		ScopeOverride:                              nil,
		ResourceIndicatorSupported:                 pgtype.Bool{Bool: false, Valid: false},
		Oidc:                                       pgtype.Bool{Bool: false, Valid: false},
		Passthrough:                                pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() {
		_, err := ti.service.CommitServerIdentityConfiguration(ctx, &gen.CommitServerIdentityConfigurationPayload{
			SessionToken:        nil,
			ApikeyToken:         nil,
			ProjectSlugInput:    nil,
			McpServerID:         targetID.String(),
			ProviderID:          conv.PtrEmpty(providerID.String()),
			CreateProvider:      nil,
			ClientMode:          "existing",
			ExistingClientID:    conv.PtrEmpty(client.ID.String()),
			ClientConfiguration: nil,
		})
		done <- err
	}()

	testenv.WaitForBlockedBackend(t, ctx, ti.conn)
	select {
	case err := <-done:
		require.Fail(t, "atomic configuration completed while another transaction held the provider row", "%v", err)
	default:
	}
	require.NoError(t, tx.Rollback(ctx))
	require.Eventually(t, func() bool { return len(done) > 0 }, 30*time.Second, 25*time.Millisecond,
		"atomic configuration did not complete after the provider row lock was released")
	require.NoError(t, <-done)
}

// bindServerIdentityClient creates a client for providerID, owned by the
// project or, when orgOwned, by the organization, binds it to userIssuerID and
// restamps the project's MCP servers on that issuer.
func bindServerIdentityClient(t *testing.T, ctx context.Context, ti *testInstance, clientID string, providerID, userIssuerID uuid.UUID, orgOwned bool) uuid.UUID {
	t.Helper()
	projectID := projectIDFromContext(t, ctx)
	client := createServerIdentityClient(t, ctx, ti, clientID, providerID, orgOwned)
	require.NoError(t, repo.New(ti.conn).AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: client,
		UserSessionIssuerID:   userIssuerID,
	}))
	require.NoError(t, remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, ti.conn, activeOrganizationID(t, ctx), projectID, []uuid.UUID{userIssuerID}))
	return client
}

func createServerIdentityClient(t *testing.T, ctx context.Context, ti *testInstance, clientID string, providerID uuid.UUID, orgOwned bool) uuid.UUID {
	t.Helper()
	projectID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if !orgOwned {
		projectID = conv.ToNullUUID(projectIDFromContext(t, ctx))
	}
	client, err := repo.New(ti.conn).CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ProjectID:             projectID,
		OrganizationID:        conv.ToPGText(activeOrganizationID(t, ctx)),
		RemoteSessionIssuerID: providerID,
		ClientID:              clientID,
		ClientIDIssuedAt:      conv.ToPGTimestamptz(time.Now().UTC()),
	})
	require.NoError(t, err)
	return client.ID
}

func requireClientBound(t *testing.T, ctx context.Context, ti *testInstance, clientID, userIssuerID uuid.UUID, bound bool) {
	t.Helper()
	client, err := repo.New(ti.conn).GetRemoteSessionClientByID(ctx, repo.GetRemoteSessionClientByIDParams{
		ProjectID:      projectIDFromContext(t, ctx),
		OrganizationID: activeOrganizationID(t, ctx),
		ID:             clientID,
	})
	require.NoError(t, err)
	require.Equal(t, bound, slices.Contains(client.UserSessionIssuerIds, userIssuerID))
}

func TestCommitServerIdentityConfigurationRefusesReplacingOrgClientOnOrgIssuerBeforeDCR(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	userIssuerID := seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "org-replace-issuer")
	targetID := createServerIdentityTargetOnIssuer(t, ctx, ti, "org-replace-target", userIssuerID)
	currentProviderID := createServerIdentityProvider(t, ctx, ti, "org-replace-current", "", false, []string{"none"})
	orgClientID := bindServerIdentityClient(t, ctx, ti, "org-replace-shared-client", currentProviderID, userIssuerID, true)

	var requests atomic.Int64
	registrationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(registrationServer.Close)
	nextProviderID := createServerIdentityProvider(t, ctx, ti, "org-replace-next", registrationServer.URL, false, []string{"client_secret_basic"})

	_, err := ti.service.CommitServerIdentityConfiguration(ctx, autoServerIdentityPayload(targetID, nextProviderID))
	requireOopsCode(t, err, oops.CodeConflict)
	require.ErrorContains(t, err, remotesessions.OrgWideBindingMessage)
	require.Zero(t, requests.Load(), "the refusal must come before an upstream client is registered")
	requireClientBound(t, ctx, ti, orgClientID, userIssuerID, true)
}

func TestCommitServerIdentityConfigurationRefusesReplacingClientWithLiveAgentBindingBeforeDCR(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	targetID, userIssuerID := createServerIdentityTarget(t, ctx, ti, "agent-replace-target")
	currentProviderID := createServerIdentityProvider(t, ctx, ti, "agent-replace-current", "", false, []string{"none"})
	currentClientID := bindServerIdentityClient(t, ctx, ti, "agent-replace-client", currentProviderID, userIssuerID, false)
	seedLiveAgentBinding(t, ctx, ti, currentClientID, userIssuerID)

	var requests atomic.Int64
	registrationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(registrationServer.Close)
	nextProviderID := createServerIdentityProvider(t, ctx, ti, "agent-replace-next", registrationServer.URL, false, []string{"client_secret_basic"})

	_, err := ti.service.CommitServerIdentityConfiguration(ctx, autoServerIdentityPayload(targetID, nextProviderID))
	requireOopsCode(t, err, oops.CodeConflict)
	require.ErrorContains(t, err, "agents are still bound to this MCP server")
	require.Zero(t, requests.Load(), "the refusal must come before an upstream client is registered")
	requireClientBound(t, ctx, ti, currentClientID, userIssuerID, true)
	requireLiveAgentBindings(t, ctx, ti, currentClientID, userIssuerID)
}

func TestCommitServerIdentityConfigurationRefusesLinkingOrgClientToOrgIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	userIssuerID := seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "org-link-issuer")
	targetID := createServerIdentityTargetOnIssuer(t, ctx, ti, "org-link-target", userIssuerID)
	providerID := createServerIdentityProvider(t, ctx, ti, "org-link-provider", "", false, []string{"none"})
	orgClientID := createServerIdentityClient(t, ctx, ti, "org-link-client", providerID, true)

	_, err := ti.service.CommitServerIdentityConfiguration(ctx, existingServerIdentityPayload(targetID, providerID, orgClientID))
	requireOopsCode(t, err, oops.CodeConflict)
	require.ErrorContains(t, err, remotesessions.OrgWideBindingMessage)
	requireClientBound(t, ctx, ti, orgClientID, userIssuerID, false)
}

func TestCommitServerIdentityConfigurationKeepsAlreadyBoundOrgClientOnOrgIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	userIssuerID := seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "org-keep-issuer")
	targetID := createServerIdentityTargetOnIssuer(t, ctx, ti, "org-keep-target", userIssuerID)
	providerID := createServerIdentityProvider(t, ctx, ti, "org-keep-provider", "", false, []string{"none"})
	orgClientID := bindServerIdentityClient(t, ctx, ti, "org-keep-client", providerID, userIssuerID, true)

	result, err := ti.service.CommitServerIdentityConfiguration(ctx, existingServerIdentityPayload(targetID, providerID, orgClientID))
	require.NoError(t, err)
	require.Equal(t, "linked", *result.Status)
	require.Equal(t, "organization-level", *result.ClientTier)
	requireClientBound(t, ctx, ti, orgClientID, userIssuerID, true)
}

func TestCommitServerIdentityConfigurationReplacesProjectClientOnOrgIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	userIssuerID := seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "org-project-issuer")
	targetID := createServerIdentityTargetOnIssuer(t, ctx, ti, "org-project-target", userIssuerID)
	currentProviderID := createServerIdentityProvider(t, ctx, ti, "org-project-current", "", false, []string{"none"})
	projectClientID := bindServerIdentityClient(t, ctx, ti, "org-project-client", currentProviderID, userIssuerID, false)

	result, err := ti.service.CommitServerIdentityConfiguration(ctx, &gen.CommitServerIdentityConfigurationPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		McpServerID:      targetID.String(),
		ProviderID:       nil,
		CreateProvider:   serverIdentityProviderForm("org-project-next", nil, false),
		ClientMode:       "manual",
		ExistingClientID: nil,
		ClientConfiguration: &gen.ServerIdentityClientConfiguration{
			ClientID:                conv.PtrEmpty("org-project-next-client"),
			ClientSecret:            conv.PtrEmpty("org-project-next-secret"),
			TokenEndpointAuthMethod: conv.PtrEmpty("client_secret_basic"),
			Scope:                   []string{"openid"},
			Audience:                nil,
		},
	})
	require.NoError(t, err)
	require.Equal(t, "project-specific", *result.ClientTier)
	requireClientBound(t, ctx, ti, projectClientID, userIssuerID, false)

	storedTarget, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID:        targetID,
		ProjectID: projectIDFromContext(t, ctx),
	})
	require.NoError(t, err)
	require.Equal(t, result.Provider.ID, storedTarget.RemoteSessionIssuerID.UUID.String())
}

func existingServerIdentityPayload(targetID, providerID, clientID uuid.UUID) *gen.CommitServerIdentityConfigurationPayload {
	return &gen.CommitServerIdentityConfigurationPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		McpServerID:         targetID.String(),
		ProviderID:          conv.PtrEmpty(providerID.String()),
		CreateProvider:      nil,
		ClientMode:          "existing",
		ExistingClientID:    conv.PtrEmpty(clientID.String()),
		ClientConfiguration: nil,
	}
}

func manualExistingProviderIdentityPayload(targetID, providerID uuid.UUID, clientID string) *gen.CommitServerIdentityConfigurationPayload {
	payload := manualServerIdentityPayload(targetID, nil)
	payload.ProviderID = conv.PtrEmpty(providerID.String())
	payload.ClientConfiguration.ClientID = conv.PtrEmpty(clientID)
	return payload
}

func TestCommitServerIdentityConfigurationReplacesClientWhenServerIssuerColumnIsStale(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	targetID, userIssuerID := createServerIdentityTarget(t, ctx, ti, "stale-column-target")
	currentProviderID := createServerIdentityProvider(t, ctx, ti, "stale-column-current", "", false, []string{"none"})
	currentClientID := createServerIdentityClient(t, ctx, ti, "stale-column-current-client", currentProviderID, false)
	// Bound without the restamp, so the server's denormalized column stays NULL.
	require.NoError(t, repo.New(ti.conn).AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: currentClientID,
		UserSessionIssuerID:   userIssuerID,
	}))
	require.Equal(t, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, storedIssuer(t, ctx, ti.conn, projectIDFromContext(t, ctx), targetID))
	nextProviderID := createServerIdentityProvider(t, ctx, ti, "stale-column-next", "", false, []string{"none"})

	result, err := ti.service.CommitServerIdentityConfiguration(ctx, manualExistingProviderIdentityPayload(targetID, nextProviderID, "stale-column-next-client"))
	require.NoError(t, err)
	nextClientID, err := uuid.Parse(result.Client.ID)
	require.NoError(t, err)

	requireClientBound(t, ctx, ti, currentClientID, userIssuerID, false)
	requireClientBound(t, ctx, ti, nextClientID, userIssuerID, true)
	require.Equal(t, conv.ToNullUUID(nextProviderID), storedIssuer(t, ctx, ti.conn, projectIDFromContext(t, ctx), targetID))
}

func TestCommitServerIdentityConfigurationRefusesAmbiguousCurrentProviderBeforeDCR(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	targetID, userIssuerID := createServerIdentityTarget(t, ctx, ti, "ambiguous-target")
	firstProviderID := createServerIdentityProvider(t, ctx, ti, "ambiguous-first", "", false, []string{"none"})
	secondProviderID := createServerIdentityProvider(t, ctx, ti, "ambiguous-second", "", false, []string{"none"})
	firstClientID := bindServerIdentityClient(t, ctx, ti, "ambiguous-first-client", firstProviderID, userIssuerID, false)
	secondClientID := bindServerIdentityClient(t, ctx, ti, "ambiguous-second-client", secondProviderID, userIssuerID, false)

	var requests atomic.Int64
	registrationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(registrationServer.Close)
	nextProviderID := createServerIdentityProvider(t, ctx, ti, "ambiguous-next", registrationServer.URL, false, []string{"client_secret_basic"})

	_, err := ti.service.CommitServerIdentityConfiguration(ctx, autoServerIdentityPayload(targetID, nextProviderID))
	requireOopsCode(t, err, oops.CodeConflict)
	require.ErrorContains(t, err, "more than one Remote Identity Provider")
	require.Zero(t, requests.Load(), "the refusal must come before an upstream client is registered")
	requireClientBound(t, ctx, ti, firstClientID, userIssuerID, true)
	requireClientBound(t, ctx, ti, secondClientID, userIssuerID, true)
}

func TestCommitServerIdentityConfigurationReplacesClientForSameProvider(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	targetID, userIssuerID := createServerIdentityTarget(t, ctx, ti, "same-provider-target")
	providerID := createServerIdentityProvider(t, ctx, ti, "same-provider", "", false, []string{"none"})
	currentClientID := bindServerIdentityClient(t, ctx, ti, "same-provider-current-client", providerID, userIssuerID, false)

	result, err := ti.service.CommitServerIdentityConfiguration(ctx, manualExistingProviderIdentityPayload(targetID, providerID, "same-provider-next-client"))
	require.NoError(t, err)
	nextClientID, err := uuid.Parse(result.Client.ID)
	require.NoError(t, err)

	requireClientBound(t, ctx, ti, currentClientID, userIssuerID, false)
	requireClientBound(t, ctx, ti, nextClientID, userIssuerID, true)
	require.Equal(t, conv.ToNullUUID(providerID), storedIssuer(t, ctx, ti.conn, projectIDFromContext(t, ctx), targetID))
}
