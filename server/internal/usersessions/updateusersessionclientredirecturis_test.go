package usersessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/user_session_clients"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func updateRedirectURIsPayload(id string, redirectURIs ...string) *gen.UpdateUserSessionClientRedirectURIsPayload {
	return &gen.UpdateUserSessionClientRedirectURIsPayload{
		ID:               id,
		RedirectUris:     redirectURIs,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}
}

func TestUpdateUserSessionClientRedirectURIs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := seedIssuer(t, ctx, ti, "update-redirects")

	created, err := ti.service.CreateUserSessionClient(ctx, createClientPayload(issuerID.String(),
		"https://agent.example.com/cb",
		"https://staging.agent.example.com/cb",
	))
	require.NoError(t, err)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionClientUpdateRedirectURIs)
	require.NoError(t, err)

	updated, err := ti.service.UpdateUserSessionClientRedirectURIs(ctx, updateRedirectURIsPayload(created.Client.ID,
		"https://agent.example.com/cb",
		"https://agent.example.com/v2/cb",
	))
	require.NoError(t, err)
	require.Equal(t, []string{"https://agent.example.com/cb", "https://agent.example.com/v2/cb"}, updated.RedirectUris)

	got, err := ti.service.GetUserSessionClient(ctx, &gen.GetUserSessionClientPayload{
		ID:               created.Client.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, updated.RedirectUris, got.RedirectUris)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionClientUpdateRedirectURIs)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	entry, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionUserSessionClientUpdateRedirectURIs)
	require.NoError(t, err)
	beforeSnapshot, err := audittest.DecodeAuditData(entry.BeforeSnapshot)
	require.NoError(t, err)
	afterSnapshot, err := audittest.DecodeAuditData(entry.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, []any{"https://agent.example.com/cb", "https://staging.agent.example.com/cb"}, beforeSnapshot["RedirectUris"])
	require.Equal(t, []any{"https://agent.example.com/cb", "https://agent.example.com/v2/cb"}, afterSnapshot["RedirectUris"])
}

func TestUpdateUserSessionClientRedirectURIs_DCRClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := seedIssuer(t, ctx, ti, "update-redirects-dcr")

	client, err := seedUserSessionClient(t, ctx, ti.conn, issuerID, "dcr-client")
	require.NoError(t, err)

	updated, err := ti.service.UpdateUserSessionClientRedirectURIs(ctx, updateRedirectURIsPayload(client.ID.String(), "https://override.example.com/cb"))
	require.NoError(t, err)
	require.Equal(t, []string{"https://override.example.com/cb"}, updated.RedirectUris)
}

func TestUpdateUserSessionClientRedirectURIs_RejectsCIMDClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := seedIssuer(t, ctx, ti, "update-redirects-cimd")

	client, err := seedCimdUserSessionClient(t, ctx, ti.conn, issuerID, "https://client.example.com/oauth/metadata.json")
	require.NoError(t, err)

	_, err = ti.service.UpdateUserSessionClientRedirectURIs(ctx, updateRedirectURIsPayload(client.ID.String(), "https://override.example.com/cb"))
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestUpdateUserSessionClientRedirectURIs_RejectsUnsafeRedirectURI(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := seedIssuer(t, ctx, ti, "update-redirects-bad-uri")

	created, err := ti.service.CreateUserSessionClient(ctx, createClientPayload(issuerID.String(), "https://agent.example.com/cb"))
	require.NoError(t, err)

	_, err = ti.service.UpdateUserSessionClientRedirectURIs(ctx, updateRedirectURIsPayload(created.Client.ID, "https://agent.example.com/cb", "javascript:alert(1)"))
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestUpdateUserSessionClientRedirectURIs_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.UpdateUserSessionClientRedirectURIs(ctx, updateRedirectURIsPayload(uuid.NewString(), "https://agent.example.com/cb"))
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestUpdateUserSessionClientRedirectURIs_ProjectIssuerRequiresProjectWrite(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := seedIssuer(t, ctx, ti, "update-redirects-rbac")

	created, err := ti.service.CreateUserSessionClient(ctx, createClientPayload(issuerID.String(), "https://agent.example.com/cb"))
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	readOnly := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()))
	_, err = ti.service.UpdateUserSessionClientRedirectURIs(readOnly, updateRedirectURIsPayload(created.Client.ID, "https://other.example.com/cb"))
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestUpdateUserSessionClientRedirectURIs_OrganizationIssuerRequiresOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := seedOrganizationTierIssuer(t, ctx, ti.conn, "update-redirects-org")

	created, err := ti.service.CreateUserSessionClient(ctx, createClientPayload(issuerID.String(), "https://agent.example.com/cb"))
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	projectWriter := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeProjectWrite, authCtx.ProjectID.String()))
	_, err = ti.service.UpdateUserSessionClientRedirectURIs(projectWriter, updateRedirectURIsPayload(created.Client.ID, "https://other.example.com/cb"))
	requireOopsCode(t, err, oops.CodeForbidden)

	orgAdmin := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID))
	_, err = ti.service.UpdateUserSessionClientRedirectURIs(orgAdmin, updateRedirectURIsPayload(created.Client.ID, "https://other.example.com/cb"))
	require.NoError(t, err)
}
