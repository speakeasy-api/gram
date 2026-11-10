package remotesessions_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	sessionsgen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	templateClientID     = "template-app"
	templateSecretCipher = "ciphertext-of-the-template-secret"
	templateCallbackBase = "https://auth.template.example.com"
)

// createTemplateClient stores a confidential personal sign-in client the way
// an administrator registered it some time ago: with a secret, a callback
// origin of its own, scopes and an audience.
func createTemplateClient(t *testing.T, ctx context.Context, ti *testInstance, providerID uuid.UUID, mutate func(*repo.CreateRemoteSessionClientParams)) repo.RemoteSessionClient {
	t.Helper()
	params := repo.CreateRemoteSessionClientParams{
		ProjectID:               conv.ToNullUUID(projectIDFromContext(t, ctx)),
		OrganizationID:          conv.ToPGText(activeOrganizationID(t, ctx)),
		RemoteSessionIssuerID:   providerID,
		ClientID:                templateClientID,
		ClientSecretEncrypted:   conv.ToPGText(templateSecretCipher),
		ClientIDIssuedAt:        conv.ToPGTimestamptz(time.Now().UTC().Add(-48 * time.Hour)),
		ClientSecretExpiresAt:   conv.ToPGTimestamptz(time.Now().UTC().Add(72 * time.Hour)),
		TokenEndpointAuthMethod: conv.ToPGText(string(remotesessions.TokenEndpointAuthMethodBasic)),
		Scope:                   []string{"repo", "read:user"},
		Audience:                conv.ToPGText("https://api.template.example.com"),
		CallbackBaseUrl:         conv.ToPGText(templateCallbackBase),
	}
	if mutate != nil {
		mutate(&params)
	}
	client, err := repo.New(ti.conn).CreateRemoteSessionClient(ctx, params)
	require.NoError(t, err)
	return client
}

func getStoredClient(t *testing.T, ctx context.Context, ti *testInstance, id uuid.UUID) repo.GetRemoteSessionClientByIDRow {
	t.Helper()
	row, err := repo.New(ti.conn).GetRemoteSessionClientByID(ctx, repo.GetRemoteSessionClientByIDParams{
		ProjectID:      projectIDFromContext(t, ctx),
		OrganizationID: activeOrganizationID(t, ctx),
		ID:             id,
	})
	require.NoError(t, err)
	return row
}

func templateCommitPayload(targetID, providerID, templateID uuid.UUID) *sessionsgen.CommitServerIdentityConfigurationPayload {
	return &sessionsgen.CommitServerIdentityConfigurationPayload{
		McpServerID: targetID.String(),
		ProviderID:  conv.PtrEmpty(providerID.String()),
		ClientMode:  "manual",
		ClientConfiguration: &sessionsgen.ServerIdentityClientConfiguration{
			ClientID:                conv.PtrEmpty(templateClientID),
			TemplateClientID:        conv.PtrEmpty(templateID.String()),
			TokenEndpointAuthMethod: conv.PtrEmpty(string(remotesessions.TokenEndpointAuthMethodBasic)),
			Scope:                   []string{"repo", "read:user"},
			Audience:                conv.PtrEmpty("https://api.template.example.com"),
		},
	}
}

func TestCommitServerIdentityConfigurationFromTemplateCreatesIndependentClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	providerID := createServerIdentityProvider(t, ctx, ti, "template-provider", "", false, []string{"client_secret_basic", "client_secret_post"})

	// The template is in use: bound to another server's issuer, with a person
	// signed in through it.
	sourceServerID, sourceIssuerID := createServerIdentityTarget(t, ctx, ti, "template-source")
	template := createTemplateClient(t, ctx, ti, providerID, nil)
	require.NoError(t, repo.New(ti.conn).AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: template.ID,
		UserSessionIssuerID:   sourceIssuerID,
	}))
	require.NoError(t, remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, ti.conn, activeOrganizationID(t, ctx), projectIDFromContext(t, ctx), []uuid.UUID{sourceIssuerID}))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_, err := repo.New(ti.conn).UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{
		SubjectUrn:            urn.NewUserSubject(authCtx.UserID),
		UserSessionIssuerID:   sourceIssuerID,
		RemoteSessionClientID: template.ID,
		AccessTokenEncrypted:  "ciphertext-of-a-user-access-token",
		RefreshTokenEncrypted: conv.ToPGText("ciphertext-of-a-user-refresh-token"),
		AccessExpiresAt:       conv.ToPGTimestamptz(time.Now().Add(time.Hour)),
		Scopes:                []string{"repo"},
	})
	require.NoError(t, err)
	before := getStoredClient(t, ctx, ti, template.ID)

	targetID, targetIssuerID := createServerIdentityTarget(t, ctx, ti, "template-target")
	duplicateAuditBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientDuplicate)
	require.NoError(t, err)

	payload := templateCommitPayload(targetID, providerID, template.ID)
	// The administrator reviewed the copied settings and narrowed two.
	payload.ClientConfiguration.Scope = []string{"repo"}
	payload.ClientConfiguration.TokenEndpointAuthMethod = conv.PtrEmpty(string(remotesessions.TokenEndpointAuthMethodPost))
	result, err := ti.service.CommitServerIdentityConfiguration(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "registered", *result.Status)
	require.Equal(t, "manual", *result.RegistrationMethod)
	require.NotEqual(t, template.ID.String(), result.Client.ID)
	require.Equal(t, []string{targetIssuerID.String()}, result.Client.UserSessionIssuerIds)

	copyID, err := uuid.Parse(result.Client.ID)
	require.NoError(t, err)
	copied := getStoredClient(t, ctx, ti, copyID).RemoteSessionClient
	require.Equal(t, templateClientID, copied.ClientID)
	require.Equal(t, providerID, copied.RemoteSessionIssuerID)
	// The secret moved between rows as ciphertext, with the registration's
	// callback and lifecycle stamps.
	require.Equal(t, templateSecretCipher, copied.ClientSecretEncrypted.String)
	require.Equal(t, templateCallbackBase, copied.CallbackBaseUrl.String)
	require.Equal(t, before.RemoteSessionClient.ClientIDIssuedAt, copied.ClientIDIssuedAt)
	require.Equal(t, before.RemoteSessionClient.ClientSecretExpiresAt, copied.ClientSecretExpiresAt)
	// What the administrator edited is stored as edited.
	require.Equal(t, []string{"repo"}, copied.Scope)
	require.Equal(t, string(remotesessions.TokenEndpointAuthMethodPost), copied.TokenEndpointAuthMethod.String)
	require.Equal(t, "https://api.template.example.com", copied.Audience.String)

	// Nobody is signed in through the new client.
	sessions, err := repo.New(ti.conn).CountActiveRemoteSessionsByClientID(ctx, copyID)
	require.NoError(t, err)
	require.Zero(t, sessions)
	sourceSessions, err := repo.New(ti.conn).CountActiveRemoteSessionsByClientID(ctx, template.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, sourceSessions)

	// The template row and the server using it are as they were.
	require.Equal(t, before, getStoredClient(t, ctx, ti, template.ID))
	requireClientBound(t, ctx, ti, template.ID, sourceIssuerID, true)
	requireClientBound(t, ctx, ti, template.ID, targetIssuerID, false)
	requireClientBound(t, ctx, ti, copyID, sourceIssuerID, false)
	associations, err := ti.service.ListServerClientAssociations(ctx, &sessionsgen.ListServerClientAssociationsPayload{McpServerID: sourceServerID.String()})
	require.NoError(t, err)
	require.True(t, slices.ContainsFunc(associations.Associations, func(a *sessionsgen.ServerClientAssociation) bool {
		return a.RemoteSessionClientID == template.ID.String()
	}))
	require.False(t, slices.ContainsFunc(associations.Associations, func(a *sessionsgen.ServerClientAssociation) bool {
		return a.RemoteSessionClientID == copyID.String()
	}))

	duplicateAuditAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientDuplicate)
	require.NoError(t, err)
	require.Equal(t, duplicateAuditBefore+1, duplicateAuditAfter)
}

func TestCommitServerIdentityConfigurationFromTemplateKeepsSuppliedSecret(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	providerID := createServerIdentityProvider(t, ctx, ti, "template-secret-provider", "", false, []string{"client_secret_basic"})
	template := createTemplateClient(t, ctx, ti, providerID, nil)
	targetID, _ := createServerIdentityTarget(t, ctx, ti, "template-secret-target")

	payload := templateCommitPayload(targetID, providerID, template.ID)
	payload.ClientConfiguration.ClientSecret = conv.PtrEmpty("a-freshly-entered-secret")
	result, err := ti.service.CommitServerIdentityConfiguration(ctx, payload)
	require.NoError(t, err)

	copyID, err := uuid.Parse(result.Client.ID)
	require.NoError(t, err)
	copied := getStoredClient(t, ctx, ti, copyID).RemoteSessionClient
	require.True(t, copied.ClientSecretEncrypted.Valid)
	require.NotEqual(t, templateSecretCipher, copied.ClientSecretEncrypted.String)
	require.NotEqual(t, "a-freshly-entered-secret", copied.ClientSecretEncrypted.String)
	// The expiry described the template's secret, not the one entered now.
	require.False(t, copied.ClientSecretExpiresAt.Valid)
	// The registration is still the template's, so its callback is kept.
	require.Equal(t, templateCallbackBase, copied.CallbackBaseUrl.String)
	require.Equal(t, templateSecretCipher, getStoredClient(t, ctx, ti, template.ID).RemoteSessionClient.ClientSecretEncrypted.String)
}

func TestCommitServerIdentityConfigurationRefusesUnusableTemplates(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	organizationID := activeOrganizationID(t, ctx)
	providerID := createServerIdentityProvider(t, ctx, ti, "refusal-provider", "", true, []string{"client_secret_basic", "none"})
	otherProviderID := createServerIdentityProvider(t, ctx, ti, "refusal-other-provider", "", false, []string{"client_secret_basic"})
	targetID, targetIssuerID := createServerIdentityTarget(t, ctx, ti, "refusal-target")
	usable := createTemplateClient(t, ctx, ti, providerID, nil)

	rejected := createTemplateClient(t, ctx, ti, providerID, nil)
	marked, err := repo.New(ti.conn).MarkRemoteSessionClientUpstreamRejected(ctx, repo.MarkRemoteSessionClientUpstreamRejectedParams{ID: rejected.ID, ClientID: rejected.ClientID})
	require.NoError(t, err)
	require.EqualValues(t, 1, marked)

	documentID, err := uuid.NewV7()
	require.NoError(t, err)
	document, err := repo.New(ti.conn).CreateRemoteSessionClientCIMD(ctx, repo.CreateRemoteSessionClientCIMDParams{
		ID:                    documentID,
		ProjectID:             conv.ToNullUUID(projectIDFromContext(t, ctx)),
		OrganizationID:        conv.ToPGText(organizationID),
		RemoteSessionIssuerID: providerID,
		ClientIDMetadataUri:   "https://app.example.com/oauth/client-metadata/" + documentID.String(),
		ClientIDIssuedAt:      conv.ToPGTimestamptz(time.Now().UTC()),
	})
	require.NoError(t, err)

	serviceAccount := createTemplateClient(t, ctx, ti, providerID, func(p *repo.CreateRemoteSessionClientParams) {
		p.CredentialOwner = conv.ToPGText(string(remotesessions.CredentialOwnerSelf))
		p.GrantTypes = []string{"client_credentials"}
	})
	public := createTemplateClient(t, ctx, ti, providerID, func(p *repo.CreateRemoteSessionClientParams) {
		p.ClientSecretEncrypted = conv.ToPGTextEmpty("")
		p.ClientSecretExpiresAt = conv.ToPGTimestamptz(time.Time{})
		p.ClientSecretExpiresAt.Valid = false
		p.TokenEndpointAuthMethod = conv.ToPGText(string(remotesessions.TokenEndpointAuthMethodNone))
	})

	keySigned := createTemplateClient(t, ctx, ti, providerID, func(p *repo.CreateRemoteSessionClientParams) {
		p.TokenEndpointAuthMethod = conv.ToPGText(string(remotesessions.TokenEndpointAuthMethodPrivateKeyJWT))
		p.JsonWebKeySetID = uuid.NullUUID{UUID: createJsonWebKeySet(t, ctx, ti.conn, organizationID, "refusal-key-set"), Valid: true}
	})

	cases := []struct {
		name   string
		mutate func(*sessionsgen.CommitServerIdentityConfigurationPayload)
		code   oops.Code
	}{
		{
			name: "a template that does not exist",
			mutate: func(p *sessionsgen.CommitServerIdentityConfigurationPayload) {
				p.ClientConfiguration.TemplateClientID = conv.PtrEmpty(uuid.NewString())
			},
			code: oops.CodeNotFound,
		},
		{
			name: "a template of another provider",
			mutate: func(p *sessionsgen.CommitServerIdentityConfigurationPayload) {
				p.ProviderID = conv.PtrEmpty(otherProviderID.String())
			},
			code: oops.CodeBadRequest,
		},
		{
			name: "a client_id the stored secret was not issued for",
			mutate: func(p *sessionsgen.CommitServerIdentityConfigurationPayload) {
				p.ClientConfiguration.ClientID = conv.PtrEmpty("some-other-app")
			},
			code: oops.CodeBadRequest,
		},
		{
			name: "a client the provider rejected",
			mutate: func(p *sessionsgen.CommitServerIdentityConfigurationPayload) {
				p.ClientConfiguration.TemplateClientID = conv.PtrEmpty(rejected.ID.String())
			},
			code: oops.CodeBadRequest,
		},
		{
			name: "a client metadata document client",
			mutate: func(p *sessionsgen.CommitServerIdentityConfigurationPayload) {
				p.ClientConfiguration.TemplateClientID = conv.PtrEmpty(document.ID.String())
				p.ClientConfiguration.ClientID = conv.PtrEmpty(document.ClientID)
			},
			code: oops.CodeBadRequest,
		},
		{
			name: "a client that signs in as itself",
			mutate: func(p *sessionsgen.CommitServerIdentityConfigurationPayload) {
				p.ClientConfiguration.TemplateClientID = conv.PtrEmpty(serviceAccount.ID.String())
			},
			code: oops.CodeBadRequest,
		},
		{
			name: "a client that signs in with a key set",
			mutate: func(p *sessionsgen.CommitServerIdentityConfigurationPayload) {
				p.ClientConfiguration.TemplateClientID = conv.PtrEmpty(keySigned.ID.String())
			},
			code: oops.CodeBadRequest,
		},
		{
			name: "a secret-based method when the template stores no secret",
			mutate: func(p *sessionsgen.CommitServerIdentityConfigurationPayload) {
				p.ClientConfiguration.TemplateClientID = conv.PtrEmpty(public.ID.String())
			},
			code: oops.CodeBadRequest,
		},
		{
			name: "a provider created in the same request",
			mutate: func(p *sessionsgen.CommitServerIdentityConfigurationPayload) {
				p.ProviderID = nil
				p.CreateProvider = serverIdentityProviderForm("refusal-new-provider", nil, false)
			},
			code: oops.CodeBadRequest,
		},
		{
			name: "automatic registration",
			mutate: func(p *sessionsgen.CommitServerIdentityConfigurationPayload) {
				p.ClientMode = "auto"
				p.ClientConfiguration.ClientID = nil
			},
			code: oops.CodeBadRequest,
		},
	}
	templates := []uuid.UUID{usable.ID, rejected.ID, document.ID, serviceAccount.ID, public.ID, keySigned.ID}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			payload := templateCommitPayload(targetID, providerID, usable.ID)
			tc.mutate(payload)
			_, err := ti.service.CommitServerIdentityConfiguration(ctx, payload)
			requireOopsCode(t, err, tc.code)

			// The refusal neither fell back to linking a template nor left a
			// client behind.
			for _, id := range templates {
				requireClientBound(t, ctx, ti, id, targetIssuerID, false)
			}
			stored, err := repo.New(ti.conn).CountRemoteSessionClientsByIssuerID(ctx, providerID)
			require.NoError(t, err)
			require.EqualValues(t, len(templates), stored)
		})
	}
}

func TestCommitServerIdentityConfigurationFromPublicTemplateCopiesNoSecret(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	providerID := createServerIdentityProvider(t, ctx, ti, "public-template-provider", "", false, []string{"none"})
	template := createTemplateClient(t, ctx, ti, providerID, func(p *repo.CreateRemoteSessionClientParams) {
		p.ClientSecretEncrypted = conv.ToPGTextEmpty("")
		p.ClientSecretExpiresAt.Valid = false
		p.TokenEndpointAuthMethod = conv.ToPGText(string(remotesessions.TokenEndpointAuthMethodNone))
	})
	targetID, _ := createServerIdentityTarget(t, ctx, ti, "public-template-target")

	payload := templateCommitPayload(targetID, providerID, template.ID)
	payload.ClientConfiguration.TokenEndpointAuthMethod = conv.PtrEmpty(string(remotesessions.TokenEndpointAuthMethodNone))
	result, err := ti.service.CommitServerIdentityConfiguration(ctx, payload)
	require.NoError(t, err)

	copyID, err := uuid.Parse(result.Client.ID)
	require.NoError(t, err)
	require.False(t, getStoredClient(t, ctx, ti, copyID).RemoteSessionClient.ClientSecretEncrypted.Valid)
}

func TestCreateRemoteSessionClientFromTemplateCreatesStandaloneCopy(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	providerID := createServerIdentityProvider(t, ctx, ti, "create-template-provider", "", false, []string{"client_secret_basic"})
	_, sourceIssuerID := createServerIdentityTarget(t, ctx, ti, "create-template-source")
	template := createTemplateClient(t, ctx, ti, providerID, nil)
	require.NoError(t, repo.New(ti.conn).AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: template.ID,
		UserSessionIssuerID:   sourceIssuerID,
	}))
	before := getStoredClient(t, ctx, ti, template.ID)

	created, err := ti.service.CreateRemoteSessionClient(ctx, &gen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID:   providerID.String(),
		ClientID:                templateClientID,
		TemplateClientID:        conv.PtrEmpty(template.ID.String()),
		TokenEndpointAuthMethod: conv.PtrEmpty(string(remotesessions.TokenEndpointAuthMethodBasic)),
		Scope:                   []string{"repo"},
		CredentialOwner:         string(remotesessions.CredentialOwnerSubject),
	})
	require.NoError(t, err)
	require.NotEqual(t, template.ID.String(), created.ID)
	require.Empty(t, created.UserSessionIssuerIds)

	copyID, err := uuid.Parse(created.ID)
	require.NoError(t, err)
	copied := getStoredClient(t, ctx, ti, copyID).RemoteSessionClient
	require.Equal(t, templateSecretCipher, copied.ClientSecretEncrypted.String)
	require.Equal(t, templateCallbackBase, copied.CallbackBaseUrl.String)
	require.Equal(t, []string{"repo"}, copied.Scope)
	require.False(t, copied.Audience.Valid)
	require.Equal(t, before, getStoredClient(t, ctx, ti, template.ID))

	// A different client_id is a different registration, which the stored
	// secret does not belong to.
	_, err = ti.service.CreateRemoteSessionClient(ctx, &gen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID: providerID.String(),
		ClientID:              "another-app",
		TemplateClientID:      conv.PtrEmpty(template.ID.String()),
		CredentialOwner:       string(remotesessions.CredentialOwnerSubject),
	})
	requireOopsCode(t, err, oops.CodeBadRequest)

	// A shared credential is not a personal sign-in.
	_, err = ti.service.CreateRemoteSessionClient(ctx, &gen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID: providerID.String(),
		ClientID:              templateClientID,
		TemplateClientID:      conv.PtrEmpty(template.ID.String()),
		CredentialOwner:       string(remotesessions.CredentialOwnerSelf),
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}
