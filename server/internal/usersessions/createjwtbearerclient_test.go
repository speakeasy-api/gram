package usersessions_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func createOrganizationIssuerForJWTBearer(t *testing.T, ctx context.Context, ti *testInstance, slug string) string {
	t.Helper()

	issuer, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
		SessionToken:         nil,
		Slug:                 slug,
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)
	return issuer.ID
}

func loadOrganizationClient(t *testing.T, ctx context.Context, ti *testInstance, id string) repo.UserSessionClient {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	row, err := repo.New(ti.conn).GetUserSessionClientByID(ctx, repo.GetUserSessionClientByIDParams{
		ID:             uuid.MustParse(id),
		ProjectID:      uuid.Nil,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	return row
}

func TestCreateJWTBearerClient_ConfidentialReturnsSecretOnceAndStoresHash(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuerID := createOrganizationIssuerForJWTBearer(t, ctx, ti, "jwt-bearer-confidential")

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionClientCreate)
	require.NoError(t, err)

	for _, method := range []string{oauthwire.AuthMethodClientSecretBasic, oauthwire.AuthMethodClientSecretPost} {
		created, err := ti.service.CreateJWTBearerClient(ctx, &orggen.CreateJWTBearerClientPayload{
			SessionToken:            nil,
			UserSessionIssuerID:     issuerID,
			ClientName:              "Codex",
			TokenEndpointAuthMethod: method,
		})
		require.NoError(t, err)
		require.NotNil(t, created.ClientSecret)
		require.NotEmpty(t, *created.ClientSecret)
		require.Equal(t, issuerID, created.Client.UserSessionIssuerID)
		require.Equal(t, "Codex", created.Client.ClientName)
		require.Equal(t, "secret", created.Client.CredentialKind)
		require.Equal(t, method, *created.Client.TokenEndpointAuthMethod)
		require.Empty(t, created.Client.RedirectUris)
		require.Equal(t, issuerID, created.Issuer.ID)
		require.False(t, created.TrustedIdentityProviderConfigured)

		row := loadOrganizationClient(t, ctx, ti, created.Client.ID)
		require.Equal(t, created.Client.ClientID, row.ClientID)
		require.True(t, row.ClientSecretHash.Valid)
		require.NotEqual(t, *created.ClientSecret, row.ClientSecretHash.String)
		require.NoError(t, bcrypt.CompareHashAndPassword([]byte(row.ClientSecretHash.String), []byte(*created.ClientSecret)))
		require.False(t, row.ProjectID.Valid)
		require.Equal(t, authCtx.ActiveOrganizationID, row.OrganizationID.String)

		record, err := audittest.LatestAuditLogByActionAndSubject(ctx, ti.conn, audit.ActionUserSessionClientCreate, created.Client.ID)
		require.NoError(t, err)
		require.Equal(t, authCtx.ActiveOrganizationID, record.OrganizationID)
		require.False(t, record.ProjectID.Valid)
		require.Equal(t, created.Client.ClientID, record.SubjectSlug)
		require.NotContains(t, string(record.AfterSnapshot), *created.ClientSecret)
		require.NotContains(t, string(record.AfterSnapshot), row.ClientSecretHash.String)
	}

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionClientCreate)
	require.NoError(t, err)
	require.Equal(t, before+2, after)
}

func TestCreateJWTBearerClient_PublicClientHasNoSecret(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := createOrganizationIssuerForJWTBearer(t, ctx, ti, "jwt-bearer-public")

	created, err := ti.service.CreateJWTBearerClient(ctx, &orggen.CreateJWTBearerClientPayload{
		SessionToken:            nil,
		UserSessionIssuerID:     issuerID,
		ClientName:              "Public CLI",
		TokenEndpointAuthMethod: oauthwire.AuthMethodNone,
	})
	require.NoError(t, err)
	require.Nil(t, created.ClientSecret)
	require.Equal(t, "public", created.Client.CredentialKind)
	require.True(t, strings.HasPrefix(created.Client.ClientID, "client_"))

	row := loadOrganizationClient(t, ctx, ti, created.Client.ID)
	require.False(t, row.ClientSecretHash.Valid)
	require.Empty(t, row.RedirectUris)
}

func TestCreateJWTBearerClient_ReportsTrustedIdentityProvider(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// The token path needs a live linked issuer with keys; a link to one
	// without a jwks_uri still rejects every assertion.
	for _, tc := range []struct {
		name    string
		jwksURI pgtype.Text
		want    bool
	}{
		{name: "with-keys", jwksURI: pgtype.Text{String: "https://idp.example.com/keys", Valid: true}, want: true},
		{name: "without-keys", jwksURI: pgtype.Text{}, want: false},
	} {
		remote, err := remotesessionsrepo.New(ti.conn).CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
			OrganizationID:                    pgtype.Text{String: authCtx.ActiveOrganizationID, Valid: true},
			Slug:                              "jwt-bearer-trusted-" + tc.name,
			Issuer:                            "https://" + tc.name + ".idp.example.com",
			JwksUri:                           tc.jwksURI,
			ScopesSupported:                   []string{},
			GrantTypesSupported:               []string{},
			ResponseTypesSupported:            []string{},
			TokenEndpointAuthMethodsSupported: []string{},
		})
		require.NoError(t, err)
		target := remote.ID.String()
		issuer, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
			SessionToken:                 nil,
			Slug:                         "jwt-bearer-trusted-" + tc.name,
			AuthnChallengeMode:           "chain",
			SessionDurationHours:         24,
			TrustedRemoteSessionIssuerID: &target,
		})
		require.NoError(t, err)

		created, err := ti.service.CreateJWTBearerClient(ctx, &orggen.CreateJWTBearerClientPayload{
			SessionToken:            nil,
			UserSessionIssuerID:     issuer.ID,
			ClientName:              "Codex",
			TokenEndpointAuthMethod: oauthwire.AuthMethodNone,
		})
		require.NoError(t, err)
		require.Equal(t, tc.want, created.TrustedIdentityProviderConfigured, tc.name)
	}
}

func TestCreateJWTBearerClient_RejectsWhitespaceName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := createOrganizationIssuerForJWTBearer(t, ctx, ti, "jwt-bearer-blank-name")

	_, err := ti.service.CreateJWTBearerClient(ctx, &orggen.CreateJWTBearerClientPayload{
		SessionToken:            nil,
		UserSessionIssuerID:     issuerID,
		ClientName:              "   ",
		TokenEndpointAuthMethod: oauthwire.AuthMethodNone,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)

	created, err := ti.service.CreateJWTBearerClient(ctx, &orggen.CreateJWTBearerClientPayload{
		SessionToken:            nil,
		UserSessionIssuerID:     issuerID,
		ClientName:              "  Codex  ",
		TokenEndpointAuthMethod: oauthwire.AuthMethodNone,
	})
	require.NoError(t, err)
	require.Equal(t, "Codex", created.Client.ClientName)
}

func TestCreateJWTBearerClient_RequiresOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuerID := createOrganizationIssuerForJWTBearer(t, ctx, ti, "jwt-bearer-rbac")

	readCtx := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeOrgRead, authCtx.ActiveOrganizationID))
	_, err := ti.service.CreateJWTBearerClient(readCtx, &orggen.CreateJWTBearerClientPayload{
		SessionToken:            nil,
		UserSessionIssuerID:     issuerID,
		ClientName:              "Codex",
		TokenEndpointAuthMethod: oauthwire.AuthMethodNone,
	})
	requireOopsCode(t, err, oops.CodeForbidden)

	apiKeyAuthCtx := *authCtx
	apiKeyAuthCtx.APIKeyID = uuid.NewString()
	_, err = ti.service.CreateJWTBearerClient(contextvalues.SetAuthContext(ctx, &apiKeyAuthCtx), &orggen.CreateJWTBearerClientPayload{
		SessionToken:            nil,
		UserSessionIssuerID:     issuerID,
		ClientName:              "Codex",
		TokenEndpointAuthMethod: oauthwire.AuthMethodNone,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestCreateJWTBearerClient_RefusesProjectIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := seedIssuer(t, ctx, ti, "jwt-bearer-project")

	_, err := ti.service.CreateJWTBearerClient(ctx, &orggen.CreateJWTBearerClientPayload{
		SessionToken:            nil,
		UserSessionIssuerID:     issuerID.String(),
		ClientName:              "Codex",
		TokenEndpointAuthMethod: oauthwire.AuthMethodNone,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateJWTBearerClient_OtherOrganizationIssuerNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	otherOrganizationID := "org-" + uuid.NewString()
	_, err := organizationsrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, organizationsrepo.UpsertOrganizationMetadataParams{
		ID:          otherOrganizationID,
		Name:        "Other Organization",
		Slug:        "other-organization-" + uuid.NewString()[:8],
		WorkosID:    pgtype.Text{String: otherOrganizationID, Valid: true},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	foreign, err := repo.New(ti.conn).CreateOrganizationUserSessionIssuer(ctx, repo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID:               pgtype.Text{String: otherOrganizationID, Valid: true},
		Slug:                         "jwt-bearer-foreign",
		AuthnChallengeMode:           "chain",
		SessionDuration:              pgtype.Interval{Microseconds: 3_600_000_000, Valid: true},
		TrustedRemoteSessionIssuerID: uuid.NullUUID{},
		TrustedRemoteSessionClientID: uuid.NullUUID{},
	})
	require.NoError(t, err)

	_, err = ti.service.CreateJWTBearerClient(ctx, &orggen.CreateJWTBearerClientPayload{
		SessionToken:            nil,
		UserSessionIssuerID:     foreign.ID.String(),
		ClientName:              "Codex",
		TokenEndpointAuthMethod: oauthwire.AuthMethodNone,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestCreateJWTBearerClient_RejectsUnsupportedAuthMethod(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := createOrganizationIssuerForJWTBearer(t, ctx, ti, "jwt-bearer-method")

	_, err := ti.service.CreateJWTBearerClient(ctx, &orggen.CreateJWTBearerClientPayload{
		SessionToken:            nil,
		UserSessionIssuerID:     issuerID,
		ClientName:              "Codex",
		TokenEndpointAuthMethod: oauthwire.AuthMethodPrivateKeyJWT,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}
