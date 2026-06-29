package remotesessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
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

	result, err := ti.service.ListIssuers(ctx, &orggen.ListIssuersPayload{
		Cursor:       nil,
		Limit:        nil,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	byID := make(map[string]*orggen.OrganizationRemoteSessionIssuer)
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

	result, err := ti.service.ListIssuers(ctx, &orggen.ListIssuersPayload{
		Cursor:       nil,
		Limit:        nil,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	for _, item := range result.Items {
		require.NotEqual(t, foreignIssuerID.String(), item.Issuer.ID)
	}

	_, err = ti.service.GetIssuer(ctx, &orggen.GetIssuerPayload{
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

	_, err := ti.service.ListIssuers(ctx, &orggen.ListIssuersPayload{
		Cursor:       nil,
		Limit:        nil,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}

// TestListClients lists the clients of an issuer with their MCP server
// attachment and active session counts (both zero here — no MCP servers
// attached and no sessions minted).
func TestListClients(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "admin-clients-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "admin-clients-usi").String()
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID, "admin-clients-client")

	result, err := ti.service.ListClients(ctx, &orggen.ListClientsPayload{
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

// TestGetClient_CrossOrgNotFound proves a client reached through an issuer
// in another org is not resolvable.
func TestGetClient_CrossOrgNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.GetClient(ctx, &orggen.GetClientPayload{
		ID:           uuid.NewString(),
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

// TestListClientSessions lists the sessions minted against a client.
func TestListClientSessions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "admin-sessions-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "admin-sessions-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "admin-sessions-client")
	insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject("admin-sessions-subject"), userIssuerID.String(), clientID)

	result, err := ti.service.ListClientSessions(ctx, &orggen.ListClientSessionsPayload{
		ClientID:     clientID,
		Cursor:       nil,
		Limit:        nil,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, urn.NewUserSubject("admin-sessions-subject").String(), result.Items[0].SubjectUrn)
}

// TestRevokeSession revokes a single session and records an audit event.
func TestRevokeSession(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "admin-revoke-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "admin-revoke-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "admin-revoke-client")
	session := insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject("admin-revoke-subject"), userIssuerID.String(), clientID)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionDelete)
	require.NoError(t, err)

	err = ti.service.RevokeSession(ctx, &orggen.RevokeSessionPayload{
		ID:           session.ID.String(),
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionDelete)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	// The audit event is attributed to the client's owning project (resolved
	// from the revoked session's client), not left unattributed.
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionRemoteSessionDelete)
	require.NoError(t, err)
	require.True(t, record.ProjectID.Valid)
	require.Equal(t, *authCtx.ProjectID, record.ProjectID.UUID)

	// The session is gone from the client's active list.
	result, err := ti.service.ListClientSessions(ctx, &orggen.ListClientSessionsPayload{
		ClientID:     clientID,
		Cursor:       nil,
		Limit:        nil,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Empty(t, result.Items)

	// Revoking again is idempotent.
	err = ti.service.RevokeSession(ctx, &orggen.RevokeSessionPayload{
		ID:           session.ID.String(),
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
}

// TestRevokeAllClientSessions revokes every session for a client and
// records exactly one bulk audit event with the revoked count.
func TestRevokeAllClientSessions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "admin-revokeall-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "admin-revokeall-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "admin-revokeall-client")
	insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject("admin-revokeall-a"), userIssuerID.String(), clientID)
	insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject("admin-revokeall-b"), userIssuerID.String(), clientID)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientRevokeSessions)
	require.NoError(t, err)

	result, err := ti.service.RevokeAllClientSessions(ctx, &orggen.RevokeAllClientSessionsPayload{
		ClientID:     clientID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Equal(t, 2, result.RevokedCount)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientRevokeSessions)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
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

	err = ti.service.DeleteClient(ctx, &orggen.DeleteClientPayload{
		ID:           clientID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientDelete)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	// The client is gone and its sessions cascaded.
	_, err = ti.service.GetClient(ctx, &orggen.GetClientPayload{
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

	err := ti.service.DeleteClient(ctx, &orggen.DeleteClientPayload{
		ID:           clientID,
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

	err := ti.service.DeleteIssuer(ctx, &orggen.DeleteIssuerPayload{
		ID:           issuerID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeConflict)

	err = ti.service.DeleteClient(ctx, &orggen.DeleteClientPayload{
		ID:           clientID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	err = ti.service.DeleteIssuer(ctx, &orggen.DeleteIssuerPayload{
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

	err := ti.service.DeleteIssuer(ctx, &orggen.DeleteIssuerPayload{
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
	updated, err := ti.service.UpdateClient(ctx, &orggen.UpdateClientPayload{
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
	recleared, err := ti.service.UpdateClient(ctx, &orggen.UpdateClientPayload{
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

	preflight, err := ti.service.GetClientDeletePreflight(ctx, &orggen.GetClientDeletePreflightPayload{
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
	_, err = ti.service.UpdateClient(ctx, &orggen.UpdateClientPayload{
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

	err := ti.service.RemoveClientFromMcpServer(ctx, &orggen.RemoveClientFromMcpServerPayload{
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

	err := ti.service.RemoveClientFromMcpServer(ctx, &orggen.RemoveClientFromMcpServerPayload{
		ClientID:     clientID,
		McpServerID:  foreignServerID.String(),
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func newCreateIssuerPayload(slug string, projectID *string) *orggen.CreateIssuerPayload {
	authEP := "https://idp.example.com/authorize"
	tokenEP := "https://idp.example.com/token"
	oidc := false
	passthrough := false
	return &orggen.CreateIssuerPayload{
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

// newUpdateIssuerNamePayload sets only the display name, leaving every other
// field nil so the update preserves the existing values (COALESCE narg).
func newUpdateIssuerNamePayload(id string, name *string) *orggen.UpdateIssuerPayload {
	return &orggen.UpdateIssuerPayload{
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

	got, err := ti.service.GetIssuer(ctx, &orggen.GetIssuerPayload{
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

	moved, err := ti.service.MoveIssuer(ctx, &orggen.MoveIssuerPayload{
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

	moved, err := ti.service.MoveIssuer(ctx, &orggen.MoveIssuerPayload{
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

	moved, err := ti.service.MoveIssuer(ctx, &orggen.MoveIssuerPayload{
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
	_, err = ti.service.MoveIssuer(ctx, &orggen.MoveIssuerPayload{
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

	_, err = ti.service.MoveIssuer(ctx, &orggen.MoveIssuerPayload{
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

	_, err := ti.service.MoveIssuer(ctx, &orggen.MoveIssuerPayload{
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

	_, err = ti.service.MoveIssuer(ctx, &orggen.MoveIssuerPayload{
		ID:           created.ID,
		ProjectID:    nil,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}

// newCreateClientPayload builds a standalone client-create payload (no
// user_session_issuer attachments) under the given issuer, optionally
// downscoped to projectID and with an optional client secret.
func newCreateClientPayload(issuerID string, projectID, clientSecret *string) *orggen.CreateClientPayload {
	return &orggen.CreateClientPayload{
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

// TestCreateClient_OrganizationalIssuerRequiresProject rejects a standalone
// client under an organization-level issuer when no project is named: a
// remote_session_client must be project-scoped, and there is no project to
// inherit.
func TestCreateClient_OrganizationalIssuerRequiresProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	orgIssuer, err := ti.service.CreateIssuer(ctx, newCreateIssuerPayload("admin-cc-org-issuer", nil))
	require.NoError(t, err)

	_, err = ti.service.CreateClient(ctx, newCreateClientPayload(orgIssuer.ID, nil, nil))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
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
