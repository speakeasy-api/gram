package remotesessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	orgclientsgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// TestListClients lists the clients of an issuer with their MCP server
// attachment and active session counts (both zero here — no MCP servers
// attached and no sessions minted).
func TestListClients(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "admin-clients-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "admin-clients-usi").String()
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID, "admin-clients-client")

	result, err := ti.service.ListClients(ctx, &orgclientsgen.ListClientsPayload{
		IssuerID:     issuerID,
		Cursor:       nil,
		Limit:        nil,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, clientID, result.Items[0].Client.ID)
	require.Equal(t, 0, result.Items[0].McpServerCount)
	require.Equal(t, 0, result.Items[0].ActiveSessionCount)
}

// TestGetClient_NotFound proves an unknown remote_session_client id in the
// caller's organization returns NotFound.
func TestGetClient_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.GetClient(ctx, &orgclientsgen.GetClientPayload{
		ID:           uuid.NewString(),
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

// TestDeleteClient soft-deletes a client, cascades its sessions, and
// records a single client-delete audit event.
func TestDeleteClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "admin-delclient-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "admin-delclient-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "admin-delclient-client")
	insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject("admin-delclient-subject"), userIssuerID.String(), clientID)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientDelete)
	require.NoError(t, err)

	err = ti.service.DeleteClient(ctx, &orgclientsgen.DeleteClientPayload{
		ID:           clientID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientDelete)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	// The client is gone and its sessions cascaded.
	_, err = ti.service.GetClient(ctx, &orgclientsgen.GetClientPayload{
		ID:           clientID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

// TestDeleteClient_RBACForbidden proves a write requires org:admin; org:read
// is insufficient.
func TestDeleteClient_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "admin-delclient-rbac-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "admin-delclient-rbac-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "admin-delclient-rbac-client")

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})

	err := ti.service.DeleteClient(ctx, &orgclientsgen.DeleteClientPayload{
		ID:           clientID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}

// TestUpdateClient patches the non-secret fields of a client and records an
// update audit event.
func TestUpdateClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "admin-updclient-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "admin-updclient-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "admin-updclient-client")

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientUpdate)
	require.NoError(t, err)

	audience := "https://api.example.com"
	authMethod := "client_secret_post"
	updated, err := ti.service.UpdateClient(ctx, &orgclientsgen.UpdateClientPayload{
		SessionToken:            nil,
		ApikeyToken:             nil,
		ID:                      clientID,
		ClientSecret:            nil,
		TokenEndpointAuthMethod: &authMethod,
		Scope:                   []string{"openid", "profile"},
		Audience:                &audience,
	})
	require.NoError(t, err)
	require.NotNil(t, updated.Audience)
	require.Equal(t, audience, *updated.Audience)
	require.Equal(t, []string{"openid", "profile"}, updated.Scope)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientUpdate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	// An empty-string audience clears the field back to unset (NULL).
	cleared := ""
	recleared, err := ti.service.UpdateClient(ctx, &orgclientsgen.UpdateClientPayload{
		SessionToken:            nil,
		ApikeyToken:             nil,
		ID:                      clientID,
		ClientSecret:            nil,
		TokenEndpointAuthMethod: nil,
		Scope:                   nil,
		Audience:                &cleared,
	})
	require.NoError(t, err)
	require.Nil(t, recleared.Audience)
}

// TestGetClientDeletePreflight reports the client's active session count and
// (empty) MCP server names.
func TestGetClientDeletePreflight(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "admin-preflight-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "admin-preflight-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "admin-preflight-client")
	insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject("admin-preflight-subject"), userIssuerID.String(), clientID)

	preflight, err := ti.service.GetClientDeletePreflight(ctx, &orgclientsgen.GetClientDeletePreflightPayload{
		ID:           clientID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Equal(t, 1, preflight.SessionCount)
	require.Empty(t, preflight.McpServerNames)
}

// TestUpdateClient_RotatesSecret proves an org admin can rotate a client's
// secret and that the stored value is encrypted (never the plaintext).
func TestUpdateClient_RotatesSecret(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	issuerID := createRemoteIssuer(t, ctx, ti, "admin-rotate-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "admin-rotate-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "admin-rotate-client")
	clientUUID, err := uuid.Parse(clientID)
	require.NoError(t, err)

	before, err := repo.New(ti.conn).GetOrganizationRemoteSessionClientByID(ctx, repo.GetOrganizationRemoteSessionClientByIDParams{
		ID:             clientUUID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	require.NoError(t, err)
	require.False(t, before.RemoteSessionClient.ClientSecretEncrypted.Valid)

	secret := "rotated-secret-value"
	_, err = ti.service.UpdateClient(ctx, &orgclientsgen.UpdateClientPayload{
		SessionToken:            nil,
		ApikeyToken:             nil,
		ID:                      clientID,
		ClientSecret:            &secret,
		TokenEndpointAuthMethod: nil,
		Scope:                   nil,
		Audience:                nil,
	})
	require.NoError(t, err)

	after, err := repo.New(ti.conn).GetOrganizationRemoteSessionClientByID(ctx, repo.GetOrganizationRemoteSessionClientByIDParams{
		ID:             clientUUID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	require.NoError(t, err)
	require.True(t, after.RemoteSessionClient.ClientSecretEncrypted.Valid)
	require.NotEmpty(t, after.RemoteSessionClient.ClientSecretEncrypted.String)
	require.NotEqual(t, secret, after.RemoteSessionClient.ClientSecretEncrypted.String)
}

// TestRemoveClientFromMcpServer_NotAttached returns NotFound when the
// target MCP server is not attached to the client.
func TestRemoveClientFromMcpServer_NotAttached(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "admin-detach-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "admin-detach-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "admin-detach-client")

	err := ti.service.RemoveClientFromMcpServer(ctx, &orgclientsgen.RemoveClientFromMcpServerPayload{
		ClientID:     clientID,
		McpServerID:  uuid.NewString(),
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

// TestRemoveClientFromMcpServer_CrossOrgNotFound proves an MCP server owned by
// another organization returns NotFound rather than being resolved across the
// tenant boundary when an admin attempts to detach a client from it.
func TestRemoveClientFromMcpServer_CrossOrgNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "admin-detach-xorg-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "admin-detach-xorg-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "admin-detach-xorg-client")

	otherOrgID := createOrganization(t, ctx, ti.conn, "admin-detach-xorg-other")
	foreignServerID := seedMCPServerInOrg(t, ctx, ti.conn, otherOrgID, "admin-detach-xorg-foreign")

	err := ti.service.RemoveClientFromMcpServer(ctx, &orgclientsgen.RemoveClientFromMcpServerPayload{
		ClientID:     clientID,
		McpServerID:  foreignServerID.String(),
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

// newCreateClientPayload builds a standalone client-create payload (no
// user_session_issuer attachments) under the given issuer, optionally
// downscoped to projectID and with an optional client secret.
func newCreateClientPayload(issuerID string, projectID, clientSecret *string) *orgclientsgen.CreateClientPayload {
	return &orgclientsgen.CreateClientPayload{
		SessionToken:            nil,
		ApikeyToken:             nil,
		RemoteSessionIssuerID:   issuerID,
		ProjectID:               projectID,
		ClientID:                "admin-create-client-" + uuid.NewString(),
		ClientSecret:            clientSecret,
		TokenEndpointAuthMethod: nil,
		Scope:                   nil,
		Audience:                nil,
	}
}

// TestCreateClient_ProjectSpecificIssuerInheritsProject creates a standalone
// client under a project-specific issuer: the client inherits the issuer's
// project, carries no user_session_issuer attachments, and records a create
// audit event.
func TestCreateClient_ProjectSpecificIssuerInheritsProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuerID := createRemoteIssuer(t, ctx, ti, "admin-cc-inherit-issuer", "")

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientCreate)
	require.NoError(t, err)

	payload := newCreateClientPayload(issuerID, nil, nil)
	created, err := ti.service.CreateClient(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, payload.ClientID, created.ClientID)
	require.Equal(t, issuerID, created.RemoteSessionIssuerID)
	require.Equal(t, authCtx.ProjectID.String(), created.ProjectID)
	require.Empty(t, created.UserSessionIssuerIds, "standalone client has no attachments")

	createdUUID, err := uuid.Parse(created.ID)
	require.NoError(t, err)
	require.Equal(t, authCtx.ActiveOrganizationID, remoteSessionClientOrganizationID(t, ctx, ti.conn, *authCtx.ProjectID, createdUUID))

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientCreate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

// TestCreateClient_OrganizationalIssuerNoProjectCreatesOrgLevel creates an
// organization-level client (no project, attachable by every project in the
// org) when no project is named under an organization-level issuer, mirroring
// how an omitted project_id makes createIssuer organization-level.
func TestCreateClient_OrganizationalIssuerNoProjectCreatesOrgLevel(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	orgIssuer, err := ti.service.CreateIssuer(ctx, newCreateIssuerPayload("admin-cc-org-issuer", nil))
	require.NoError(t, err)

	created, err := ti.service.CreateClient(ctx, newCreateClientPayload(orgIssuer.ID, nil, nil))
	require.NoError(t, err)
	require.Empty(t, created.ProjectID, "organization-level client has no project")
	require.Equal(t, authCtx.ActiveOrganizationID, created.OrganizationID)
	require.Equal(t, orgIssuer.ID, created.RemoteSessionIssuerID)
}

// TestCreateClient_OrganizationalIssuerDownscope creates a standalone client
// under an organization-level issuer downscoped to a project in the org.
func TestCreateClient_OrganizationalIssuerDownscope(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	pid := authCtx.ProjectID.String()

	orgIssuer, err := ti.service.CreateIssuer(ctx, newCreateIssuerPayload("admin-cc-downscope-issuer", nil))
	require.NoError(t, err)

	created, err := ti.service.CreateClient(ctx, newCreateClientPayload(orgIssuer.ID, &pid, nil))
	require.NoError(t, err)
	require.Equal(t, pid, created.ProjectID)
	require.Equal(t, orgIssuer.ID, created.RemoteSessionIssuerID)

	createdUUID, err := uuid.Parse(created.ID)
	require.NoError(t, err)
	require.Equal(t, authCtx.ActiveOrganizationID, remoteSessionClientOrganizationID(t, ctx, ti.conn, *authCtx.ProjectID, createdUUID))
}

// TestCreateClient_ProjectMismatchForProjectIssuer rejects downscoping a client
// to a different project than the project-specific issuer's own, which would
// leave the client referencing an issuer unreachable from its project.
func TestCreateClient_ProjectMismatchForProjectIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "admin-cc-mismatch-issuer", "")
	otherProject := createProject(t, ctx, ti.conn, "admin-cc-other-project").String()

	_, err := ti.service.CreateClient(ctx, newCreateClientPayload(issuerID, &otherProject, nil))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// TestCreateClient_ProjectNotInOrg rejects a project_id that does not belong to
// the caller's organization.
func TestCreateClient_ProjectNotInOrg(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	orgIssuer, err := ti.service.CreateIssuer(ctx, newCreateIssuerPayload("admin-cc-badproj-issuer", nil))
	require.NoError(t, err)

	bogus := uuid.NewString()
	_, err = ti.service.CreateClient(ctx, newCreateClientPayload(orgIssuer.ID, &bogus, nil))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// TestCreateClient_IssuerNotFound rejects a client registered against an issuer
// that is not in the caller's organization.
func TestCreateClient_IssuerNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.CreateClient(ctx, newCreateClientPayload(uuid.NewString(), nil, nil))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

// TestCreateClient_RBACForbidden proves client creation requires org:admin; an
// org:read principal is rejected.
func TestCreateClient_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "admin-cc-rbac-issuer", "")

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})

	_, err := ti.service.CreateClient(ctx, newCreateClientPayload(issuerID, nil, nil))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}

// TestCreateClient_EncryptsSecret proves a supplied client secret is stored
// encrypted, never as the plaintext.
func TestCreateClient_EncryptsSecret(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	issuerID := createRemoteIssuer(t, ctx, ti, "admin-cc-secret-issuer", "")

	secret := "create-secret-value"
	created, err := ti.service.CreateClient(ctx, newCreateClientPayload(issuerID, nil, &secret))
	require.NoError(t, err)

	clientUUID, err := uuid.Parse(created.ID)
	require.NoError(t, err)

	stored, err := repo.New(ti.conn).GetOrganizationRemoteSessionClientByID(ctx, repo.GetOrganizationRemoteSessionClientByIDParams{
		ID:             clientUUID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	require.NoError(t, err)
	require.True(t, stored.RemoteSessionClient.ClientSecretEncrypted.Valid)
	require.NotEmpty(t, stored.RemoteSessionClient.ClientSecretEncrypted.String)
	require.NotEqual(t, secret, stored.RemoteSessionClient.ClientSecretEncrypted.String)
}

// TestCreateClient_OrgAdminAttachesToPlatformIssuer proves an org-admin
// standalone client can be created against a platform issuer. With no owning
// project on the issuer, the client is organization-level (NULL project,
// organization_id set), attachable by every project in the org.
func TestCreateClient_OrgAdminAttachesToPlatformIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	platformID := seedGlobalRemoteIssuer(t, ctx, ti.conn, "attach-platform-org")

	created, err := ti.service.CreateClient(ctx, newCreateClientPayload(platformID.String(), nil, nil))
	require.NoError(t, err)
	require.Empty(t, created.ProjectID, "client on a platform issuer with no project is organization-level")
	require.Equal(t, authCtx.ActiveOrganizationID, created.OrganizationID)
	require.Equal(t, platformID.String(), created.RemoteSessionIssuerID)
}

// TestOrgAdmin_ManagesTenantClientOnPlatformIssuer proves that once an org-admin
// client is created on a platform issuer it stays fully manageable through the
// org-admin surface: listable under the issuer, fetchable, patchable, and
// deletable. Before the org-reachability rescope these all scoped through the
// issuer's organization_id, which is NULL for a platform issuer, so the client
// was write-only state.
func TestOrgAdmin_ManagesTenantClientOnPlatformIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	platformID := seedGlobalRemoteIssuer(t, ctx, ti.conn, "manage-platform")
	created, err := ti.service.CreateClient(ctx, newCreateClientPayload(platformID.String(), nil, nil))
	require.NoError(t, err)

	// Listable under the issuer.
	list, err := ti.service.ListClients(ctx, &orgclientsgen.ListClientsPayload{
		IssuerID:     platformID.String(),
		Cursor:       nil,
		Limit:        nil,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	foundInList := false
	for _, item := range list.Items {
		if item.Client.ID == created.ID {
			foundInList = true
		}
	}
	require.True(t, foundInList, "tenant client on a platform issuer should be listable by the org admin")

	// Fetchable.
	got, err := ti.service.GetClient(ctx, &orgclientsgen.GetClientPayload{
		ID:           created.ID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)

	// Patchable.
	newMethod := "client_secret_post"
	updated, err := ti.service.UpdateClient(ctx, &orgclientsgen.UpdateClientPayload{
		ID:                      created.ID,
		ClientSecret:            nil,
		TokenEndpointAuthMethod: &newMethod,
		Scope:                   nil,
		Audience:                nil,
		SessionToken:            nil,
		ApikeyToken:             nil,
	})
	require.NoError(t, err)
	require.NotNil(t, updated.TokenEndpointAuthMethod)
	require.Equal(t, newMethod, *updated.TokenEndpointAuthMethod)

	// Deletable.
	err = ti.service.DeleteClient(ctx, &orgclientsgen.DeleteClientPayload{
		ID:           created.ID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	_, err = ti.service.GetClient(ctx, &orgclientsgen.GetClientPayload{
		ID:           created.ID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}
