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
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestCommitServerUserIdentityConfigurationManualCreatesConfigurationAtomically(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	targetID, userIssuerID := createServerIdentityTarget(t, ctx, ti, "manual-target")

	issuerAuditBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerCreate)
	require.NoError(t, err)
	clientAuditBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientCreate)
	require.NoError(t, err)

	result, err := ti.service.CommitServerUserIdentityConfiguration(ctx, &gen.CommitServerUserIdentityConfigurationPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		McpServerID:      targetID.String(),
		ProviderID:       nil,
		CreateProvider:   serverIdentityProviderForm("manual-provider", nil, false),
		ClientMode:       "manual",
		ExistingClientID: nil,
		ClientConfiguration: &gen.ServerUserIdentityClientConfiguration{
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

func TestCommitServerUserIdentityConfigurationAutoPrefersCIMD(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	targetID, userIssuerID := createServerIdentityTarget(t, ctx, ti, "cimd-target")
	providerID := createServerIdentityProvider(t, ctx, ti, "cimd-provider", "https://registration.invalid", true, []string{"none", "client_secret_basic"})

	result, err := ti.service.CommitServerUserIdentityConfiguration(ctx, autoServerIdentityPayload(targetID, providerID))
	require.NoError(t, err)
	require.Equal(t, "registered", *result.Status)
	require.Equal(t, "cimd", *result.RegistrationMethod)
	require.Equal(t, "none", *result.Client.TokenEndpointAuthMethod)
	require.Equal(t, []string{userIssuerID.String()}, result.Client.UserSessionIssuerIds)
	require.NotNil(t, result.Client.ClientIDMetadataURI)
	require.Equal(t, *result.Client.ClientIDMetadataURI, result.Client.ClientID)
}

func TestCommitServerUserIdentityConfigurationAutoRegistersOnceWithDCR(t *testing.T) {
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

	result, err := ti.service.CommitServerUserIdentityConfiguration(ctx, payload)
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

func TestCommitServerUserIdentityConfigurationAutoRequiresManualSetupWithoutChangingState(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	targetID, _ := createServerIdentityTarget(t, ctx, ti, "fallback-target")
	providerID := createServerIdentityProvider(t, ctx, ti, "fallback-provider", "", false, []string{"client_secret_basic"})
	clientsBefore, err := repo.New(ti.conn).CountRemoteSessionClientsByIssuerID(ctx, providerID)
	require.NoError(t, err)

	result, err := ti.service.CommitServerUserIdentityConfiguration(ctx, autoServerIdentityPayload(targetID, providerID))
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

func TestCommitServerUserIdentityConfigurationExistingClientNeedsOnlyMCPWrite(t *testing.T) {
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

	result, err := ti.service.CommitServerUserIdentityConfiguration(restrictedCtx, &gen.CommitServerUserIdentityConfigurationPayload{
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

	_, err = ti.service.CommitServerUserIdentityConfiguration(restrictedCtx, &gen.CommitServerUserIdentityConfigurationPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		McpServerID:      targetID.String(),
		ProviderID:       nil,
		CreateProvider:   serverIdentityProviderForm("denied-provider", nil, false),
		ClientMode:       "manual",
		ExistingClientID: nil,
		ClientConfiguration: &gen.ServerUserIdentityClientConfiguration{
			ClientID:                conv.PtrEmpty("denied-client"),
			ClientSecret:            nil,
			TokenEndpointAuthMethod: conv.PtrEmpty("none"),
			Scope:                   nil,
			Audience:                nil,
		},
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestCommitServerUserIdentityConfigurationReplacesCurrentClientAtomically(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	targetID, userIssuerID := createServerIdentityTarget(t, ctx, ti, "replace-target")

	initial, err := ti.service.CommitServerUserIdentityConfiguration(ctx, &gen.CommitServerUserIdentityConfigurationPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		McpServerID:      targetID.String(),
		ProviderID:       nil,
		CreateProvider:   serverIdentityProviderForm("replace-initial-provider", nil, false),
		ClientMode:       "manual",
		ExistingClientID: nil,
		ClientConfiguration: &gen.ServerUserIdentityClientConfiguration{
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

	result, err := ti.service.CommitServerUserIdentityConfiguration(ctx, &gen.CommitServerUserIdentityConfigurationPayload{
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

func TestCommitServerUserIdentityConfigurationLocksUserSessionIssuerBeforeReplacingBindings(t *testing.T) {
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
		_, err := ti.service.CommitServerUserIdentityConfiguration(ctx, &gen.CommitServerUserIdentityConfigurationPayload{
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

	require.Never(t, func() bool { return len(done) > 0 }, 500*time.Millisecond, 25*time.Millisecond,
		"atomic configuration completed while another transaction held the user-session-issuer binding lock")
	require.NoError(t, tx.Rollback(ctx))
	require.Eventually(t, func() bool { return len(done) > 0 }, 30*time.Second, 25*time.Millisecond,
		"atomic configuration did not complete after the user-session-issuer binding lock was released")
	require.NoError(t, <-done)
}

func createServerIdentityTarget(t *testing.T, ctx context.Context, ti *testInstance, slug string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	projectID := projectIDFromContext(t, ctx)
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, slug+"-issuer")
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
	return target.ID, userIssuerID
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

func autoServerIdentityPayload(targetID, providerID uuid.UUID) *gen.CommitServerUserIdentityConfigurationPayload {
	return &gen.CommitServerUserIdentityConfigurationPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		McpServerID:      targetID.String(),
		ProviderID:       conv.PtrEmpty(providerID.String()),
		CreateProvider:   nil,
		ClientMode:       "auto",
		ExistingClientID: nil,
		ClientConfiguration: &gen.ServerUserIdentityClientConfiguration{
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
