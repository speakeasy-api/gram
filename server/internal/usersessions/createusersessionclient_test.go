package usersessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	gen "github.com/speakeasy-api/gram/server/gen/user_session_clients"
	issuersgen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func createClientPayload(issuerID string, redirectURIs ...string) *gen.CreateUserSessionClientPayload {
	return &gen.CreateUserSessionClientPayload{
		UserSessionIssuerID:     issuerID,
		ClientName:              "Support triage agent",
		RedirectUris:            redirectURIs,
		TokenEndpointAuthMethod: "client_secret_basic",
		SessionToken:            nil,
		ApikeyToken:             nil,
		ProjectSlugInput:        nil,
	}
}

func TestCreateUserSessionClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "create-client-issuer",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionClientCreate)
	require.NoError(t, err)

	res, err := ti.service.CreateUserSessionClient(ctx, createClientPayload(issuer.ID,
		"https://agent.example.com/oauth/callback",
		"http://localhost:8765/callback",
	))
	require.NoError(t, err)

	require.NotEmpty(t, res.ClientSecret)
	require.Regexp(t, `^client_[0-9a-f-]{36}$`, res.Client.ClientID)
	require.Equal(t, "Support triage agent", res.Client.ClientName)
	require.Equal(t, issuer.ID, res.Client.UserSessionIssuerID)
	require.Equal(t, "secret", res.Client.CredentialKind)
	require.NotNil(t, res.Client.TokenEndpointAuthMethod)
	require.Equal(t, "client_secret_basic", *res.Client.TokenEndpointAuthMethod)
	require.Equal(t, []string{"https://agent.example.com/oauth/callback", "http://localhost:8765/callback"}, res.Client.RedirectUris)
	require.Nil(t, res.Client.ClientIDMetadataURI)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Only the hash is stored, and it verifies the returned secret.
	row, err := repo.New(ti.conn).GetUserSessionClientByID(ctx, repo.GetUserSessionClientByIDParams{
		ID:             uuid.MustParse(res.Client.ID),
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	require.True(t, row.ClientSecretHash.Valid)
	require.NotEqual(t, res.ClientSecret, row.ClientSecretHash.String)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(row.ClientSecretHash.String), []byte(res.ClientSecret)))

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionClientCreate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	entry, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionUserSessionClientCreate)
	require.NoError(t, err)
	require.Equal(t, res.Client.ID, entry.SubjectID)
	require.Equal(t, authCtx.UserID, entry.ActorID)
}

func TestCreateUserSessionClient_ClientSecretPost(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := seedIssuer(t, ctx, ti, "create-client-post")

	payload := createClientPayload(issuerID.String(), "https://agent.example.com/cb")
	payload.TokenEndpointAuthMethod = "client_secret_post"

	res, err := ti.service.CreateUserSessionClient(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, res.Client.TokenEndpointAuthMethod)
	require.Equal(t, "client_secret_post", *res.Client.TokenEndpointAuthMethod)
	require.Equal(t, "secret", res.Client.CredentialKind)
}

func TestCreateUserSessionClient_RejectsUnsafeRedirectURI(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := seedIssuer(t, ctx, ti, "create-client-bad-uri")

	for _, uri := range []string{"javascript:alert(1)", "http://agent.example.com/cb", "not a url", "https://agent.example.com/cb#frag"} {
		_, err := ti.service.CreateUserSessionClient(ctx, createClientPayload(issuerID.String(), uri))
		requireOopsCode(t, err, oops.CodeBadRequest)
	}
}

func TestCreateUserSessionClient_IssuerNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.CreateUserSessionClient(ctx, createClientPayload(uuid.NewString(), "https://agent.example.com/cb"))
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestCreateUserSessionClient_ProjectIssuerRequiresProjectWrite(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := seedIssuer(t, ctx, ti, "create-client-rbac")

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	readOnly := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()))
	_, err := ti.service.CreateUserSessionClient(readOnly, createClientPayload(issuerID.String(), "https://agent.example.com/cb"))
	requireOopsCode(t, err, oops.CodeForbidden)

	writer := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeProjectWrite, authCtx.ProjectID.String()))
	_, err = ti.service.CreateUserSessionClient(writer, createClientPayload(issuerID.String(), "https://agent.example.com/cb"))
	require.NoError(t, err)
}

func TestCreateUserSessionClient_OrganizationIssuerRequiresOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := seedOrganizationTierIssuer(t, ctx, ti.conn, "create-client-org")

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// project:write on the caller's project is not enough for an issuer
	// every project in the organization shares.
	projectWriter := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeProjectWrite, authCtx.ProjectID.String()))
	_, err := ti.service.CreateUserSessionClient(projectWriter, createClientPayload(issuerID.String(), "https://agent.example.com/cb"))
	requireOopsCode(t, err, oops.CodeForbidden)

	orgAdmin := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID))
	res, err := ti.service.CreateUserSessionClient(orgAdmin, createClientPayload(issuerID.String(), "https://agent.example.com/cb"))
	require.NoError(t, err)
	require.Equal(t, issuerID.String(), res.Client.UserSessionIssuerID)

	// Org-scoped: the audit entry carries no project.
	entry, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionUserSessionClientCreate)
	require.NoError(t, err)
	require.False(t, entry.ProjectID.Valid)
}
