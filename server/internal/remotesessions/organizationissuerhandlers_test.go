package remotesessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	orgclientsgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_clients"
	orgissuersgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// TestListIssuers returns both organizational and project-specific issuers
// in the caller's org, each tagged with its client count and (for
// project-specific) the owning project name.
func TestListIssuers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	orgIssuer, err := ti.service.CreateIssuer(ctx, newCreateIssuerPayload("admin-list-org", nil))
	require.NoError(t, err)

	projIssuerID := createRemoteIssuer(t, ctx, ti, "admin-list-proj", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "admin-list-usi").String()
	createRemoteClient(t, ctx, ti, projIssuerID, userIssuerID, "admin-list-client")

	result, err := ti.service.ListIssuers(ctx, &orgissuersgen.ListIssuersPayload{
		Cursor:       nil,
		Limit:        nil,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	byID := make(map[string]*orgissuersgen.OrganizationRemoteSessionIssuer)
	for _, item := range result.Items {
		byID[item.Issuer.ID] = item
	}

	gotOrg, ok := byID[orgIssuer.ID]
	require.True(t, ok, "organizational issuer should be listed")
	require.Empty(t, gotOrg.Issuer.ProjectID)
	require.Nil(t, gotOrg.ProjectName)
	require.Equal(t, 0, gotOrg.ClientCount)

	gotProj, ok := byID[projIssuerID]
	require.True(t, ok, "project-specific issuer should be listed")
	require.NotEmpty(t, gotProj.Issuer.ProjectID)
	require.NotNil(t, gotProj.ProjectName)
	require.NotEmpty(t, *gotProj.ProjectName)
	require.Equal(t, 1, gotProj.ClientCount)
}

// TestListIssuers_CrossOrgIsolation proves an issuer owned by another
// organization is never surfaced to this org's admin.
func TestListIssuers_CrossOrgIsolation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	otherOrgID := createOrganization(t, ctx, ti.conn, "admin-other-org")
	foreignIssuerID := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, otherOrgID, "admin-foreign-issuer")

	result, err := ti.service.ListIssuers(ctx, &orgissuersgen.ListIssuersPayload{
		Cursor:       nil,
		Limit:        nil,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	for _, item := range result.Items {
		require.NotEqual(t, foreignIssuerID.String(), item.Issuer.ID)
	}

	_, err = ti.service.GetIssuer(ctx, &orgissuersgen.GetIssuerPayload{
		ID:           foreignIssuerID.String(),
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

// TestListIssuers_RBACForbidden proves the listing requires org:read.
func TestListIssuers_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// Install a principal with no org grants at all.
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.ListIssuers(ctx, &orgissuersgen.ListIssuersPayload{
		Cursor:       nil,
		Limit:        nil,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}

// TestDeleteIssuer_BlockedByClients refuses to delete an issuer that still
// has clients, then succeeds once the client is removed.
func TestDeleteIssuer_BlockedByClients(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "admin-delissuer-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "admin-delissuer-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "admin-delissuer-client")

	err := ti.service.DeleteIssuer(ctx, &orgissuersgen.DeleteIssuerPayload{
		ID:           issuerID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeConflict)

	err = ti.service.DeleteClient(ctx, &orgclientsgen.DeleteClientPayload{
		ID:           clientID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	err = ti.service.DeleteIssuer(ctx, &orgissuersgen.DeleteIssuerPayload{
		ID:           issuerID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
}

// TestDeleteIssuer_CrossOrgNotFound proves an issuer owned by another
// organization returns NotFound (rather than silently succeeding or probing
// its client count) when an admin attempts to delete it.
func TestDeleteIssuer_CrossOrgNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	otherOrgID := createOrganization(t, ctx, ti.conn, "admin-delissuer-other-org")
	foreignIssuerID := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, otherOrgID, "admin-delissuer-foreign")

	err := ti.service.DeleteIssuer(ctx, &orgissuersgen.DeleteIssuerPayload{
		ID:           foreignIssuerID.String(),
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)

	// The foreign issuer is untouched (still resolvable in its own org).
	_, err = repo.New(ti.conn).GetOrganizationRemoteSessionIssuerByID(ctx, repo.GetOrganizationRemoteSessionIssuerByIDParams{
		ID:             foreignIssuerID,
		OrganizationID: conv.ToPGText(otherOrgID),
	})
	require.NoError(t, err)
}

func newCreateIssuerPayload(slug string, projectID *string) *orgissuersgen.CreateIssuerPayload {
	authEP := "https://idp.example.com/authorize"
	tokenEP := "https://idp.example.com/token"
	oidc := false
	passthrough := false
	return &orgissuersgen.CreateIssuerPayload{
		SessionToken:                      nil,
		ApikeyToken:                       nil,
		ProjectID:                         projectID,
		Slug:                              slug,
		Issuer:                            "https://idp.example.com",
		Name:                              nil,
		LogoAssetID:                       nil,
		AuthorizationEndpoint:             &authEP,
		TokenEndpoint:                     &tokenEP,
		RegistrationEndpoint:              nil,
		JwksURI:                           nil,
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
		Oidc:                              &oidc,
		Passthrough:                       &passthrough,
	}
}

// TestCreateIssuer_Organizational creates an organization-level issuer
// (no project_id) and records a create audit event.
func TestCreateIssuer_Organizational(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerCreate)
	require.NoError(t, err)

	created, err := ti.service.CreateIssuer(ctx, newCreateIssuerPayload("admin-create-org", nil))
	require.NoError(t, err)
	require.Empty(t, created.ProjectID)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerCreate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

// TestCreateIssuer_ProjectSpecific creates a project-specific issuer for a
// project in the caller's organization.
func TestCreateIssuer_ProjectSpecific(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	pid := authCtx.ProjectID.String()

	created, err := ti.service.CreateIssuer(ctx, newCreateIssuerPayload("admin-create-proj", &pid))
	require.NoError(t, err)
	require.Equal(t, pid, created.ProjectID)
}

// TestCreateIssuer_ProjectNotInOrg rejects a project_id that does not
// belong to the caller's organization.
func TestCreateIssuer_ProjectNotInOrg(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	bogus := uuid.NewString()
	_, err := ti.service.CreateIssuer(ctx, newCreateIssuerPayload("admin-create-bad", &bogus))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// TestCreateIssuer_DuplicateSlug maps a duplicate-slug insert on a
// project-specific issuer to a 409 conflict rather than an unexpected fault.
func TestCreateIssuer_DuplicateSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	pid := authCtx.ProjectID.String()

	_, err := ti.service.CreateIssuer(ctx, newCreateIssuerPayload("admin-dup-slug", &pid))
	require.NoError(t, err)

	_, err = ti.service.CreateIssuer(ctx, newCreateIssuerPayload("admin-dup-slug", &pid))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeConflict)
}

// newUpdateIssuerNamePayload sets only the display name, leaving every other
// field nil so the update preserves the existing values (COALESCE narg).
func newUpdateIssuerNamePayload(id string, name *string) *orgissuersgen.UpdateIssuerPayload {
	return &orgissuersgen.UpdateIssuerPayload{
		SessionToken:                      nil,
		ApikeyToken:                       nil,
		ID:                                id,
		Slug:                              nil,
		Issuer:                            nil,
		Name:                              name,
		LogoAssetID:                       nil,
		AuthorizationEndpoint:             nil,
		TokenEndpoint:                     nil,
		RegistrationEndpoint:              nil,
		JwksURI:                           nil,
		ScopesSupported:                   nil,
		GrantTypesSupported:               nil,
		ResponseTypesSupported:            nil,
		TokenEndpointAuthMethodsSupported: nil,
		Oidc:                              nil,
		Passthrough:                       nil,
	}
}

// TestUpdateIssuer_Name verifies that the display name is persisted on update
// (regression: the org-admin update query previously omitted the name column,
// so renames silently no-oped) and that an empty string clears it.
func TestUpdateIssuer_Name(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateIssuer(ctx, newCreateIssuerPayload("admin-update-name", nil))
	require.NoError(t, err)

	renamed := "Renamed Provider"
	updated, err := ti.service.UpdateIssuer(ctx, newUpdateIssuerNamePayload(created.ID, &renamed))
	require.NoError(t, err)
	require.NotNil(t, updated.Name)
	require.Equal(t, renamed, *updated.Name)

	got, err := ti.service.GetIssuer(ctx, &orgissuersgen.GetIssuerPayload{
		ID:           created.ID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.NotNil(t, got.Name)
	require.Equal(t, renamed, *got.Name)

	empty := ""
	cleared, err := ti.service.UpdateIssuer(ctx, newUpdateIssuerNamePayload(created.ID, &empty))
	require.NoError(t, err)
	require.Nil(t, cleared.Name)
}

// TestMoveIssuer_ProjectToOrganizational promotes a project-specific issuer to
// organization-level (clears project_id) and records an update audit event.
func TestMoveIssuer_ProjectToOrganizational(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	pid := authCtx.ProjectID.String()

	created, err := ti.service.CreateIssuer(ctx, newCreateIssuerPayload("admin-move-to-org", &pid))
	require.NoError(t, err)
	require.Equal(t, pid, created.ProjectID)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdate)
	require.NoError(t, err)

	moved, err := ti.service.MoveIssuer(ctx, &orgissuersgen.MoveIssuerPayload{
		ID:           created.ID,
		ProjectID:    nil,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Empty(t, moved.ProjectID)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

// TestMoveIssuer_OrganizationalToProject scopes an organization-level issuer to
// a project in the caller's organization.
func TestMoveIssuer_OrganizationalToProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateIssuer(ctx, newCreateIssuerPayload("admin-move-to-proj", nil))
	require.NoError(t, err)
	require.Empty(t, created.ProjectID)

	projectID := createProject(t, ctx, ti.conn, "admin-move-target-proj").String()

	moved, err := ti.service.MoveIssuer(ctx, &orgissuersgen.MoveIssuerPayload{
		ID:           created.ID,
		ProjectID:    &projectID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Equal(t, projectID, moved.ProjectID)
}

// TestMoveIssuer_BetweenProjects reassigns a project-specific issuer from one
// project to another in the caller's organization.
func TestMoveIssuer_BetweenProjects(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	sourcePID := authCtx.ProjectID.String()

	created, err := ti.service.CreateIssuer(ctx, newCreateIssuerPayload("admin-move-between", &sourcePID))
	require.NoError(t, err)
	require.Equal(t, sourcePID, created.ProjectID)

	targetPID := createProject(t, ctx, ti.conn, "admin-move-between-target").String()

	moved, err := ti.service.MoveIssuer(ctx, &orgissuersgen.MoveIssuerPayload{
		ID:           created.ID,
		ProjectID:    &targetPID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Equal(t, targetPID, moved.ProjectID)
}

// TestMoveIssuer_ProjectNotInOrg rejects a target project_id that does not
// belong to the caller's organization.
func TestMoveIssuer_ProjectNotInOrg(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateIssuer(ctx, newCreateIssuerPayload("admin-move-bad-proj", nil))
	require.NoError(t, err)

	bogus := uuid.NewString()
	_, err = ti.service.MoveIssuer(ctx, &orgissuersgen.MoveIssuerPayload{
		ID:           created.ID,
		ProjectID:    &bogus,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// TestMoveIssuer_SlugConflict refuses to move an issuer into a project that
// already has an issuer with the same slug.
func TestMoveIssuer_SlugConflict(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	targetPID := createProject(t, ctx, ti.conn, "admin-move-conflict-proj").String()

	// An existing issuer in the target project occupies the slug.
	_, err := ti.service.CreateIssuer(ctx, newCreateIssuerPayload("admin-move-conflict-slug", &targetPID))
	require.NoError(t, err)

	// An organization-level issuer sharing the slug cannot move into that project.
	orgIssuer, err := ti.service.CreateIssuer(ctx, newCreateIssuerPayload("admin-move-conflict-slug", nil))
	require.NoError(t, err)

	_, err = ti.service.MoveIssuer(ctx, &orgissuersgen.MoveIssuerPayload{
		ID:           orgIssuer.ID,
		ProjectID:    &targetPID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeConflict)
}

// TestMoveIssuer_NotFound returns not-found for an unknown issuer id in the
// caller's organization.
func TestMoveIssuer_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.MoveIssuer(ctx, &orgissuersgen.MoveIssuerPayload{
		ID:           uuid.NewString(),
		ProjectID:    nil,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

// TestMoveIssuer_RBACForbidden proves the move requires org:admin; org:read is
// insufficient.
func TestMoveIssuer_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateIssuer(ctx, newCreateIssuerPayload("admin-move-rbac", nil))
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})

	_, err = ti.service.MoveIssuer(ctx, &orgissuersgen.MoveIssuerPayload{
		ID:           created.ID,
		ProjectID:    nil,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}
