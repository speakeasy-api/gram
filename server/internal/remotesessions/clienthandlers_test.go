package remotesessions_test

import (
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestCreateRemoteSessionClient_Manual(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsc-manual", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-manual").String()

	clientID := "manual-client-id"
	clientSecret := "manual-client-secret"

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientCreate)
	require.NoError(t, err)

	result, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerIds:  []string{userIssuerID},
		ClientID:              clientID,
		ClientSecret:          &clientSecret,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, clientID, result.ClientID)
	require.Equal(t, issuerID, result.RemoteSessionIssuerID)
	require.Equal(t, []string{userIssuerID}, result.UserSessionIssuerIds)
	require.NotEmpty(t, result.ID)

	clientUUID, err := uuid.Parse(result.ID)
	require.NoError(t, err)
	userIssuerUUID, err := uuid.Parse(userIssuerID)
	require.NoError(t, err)
	require.Equal(t, 1, countRemoteSessionClientUserSessionIssuerBindings(t, ctx, ti.conn, clientUUID, userIssuerUUID))

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestCreateRemoteSessionClient_RejectsDuplicateRemoteIssuerBinding(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsc-dup", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-dup").String()

	// First client binds (user_session_issuer, remote_session_issuer).
	createRemoteClient(t, ctx, ti, issuerID, userIssuerID, "dup-client-1")

	// Second client for the same pair must be rejected by the attach-time
	// guard with a conflict, not a raw constraint error.
	_, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerIds:  []string{userIssuerID},
		ClientID:              "dup-client-2",
		ClientSecret:          nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestCreateRemoteSessionClient_Manual_WithAuthMethodPost(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsc-post", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-post").String()

	clientID := "post-client-id"
	clientSecret := "post-client-secret"
	authMethod := "client_secret_post"

	result, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID:   issuerID,
		UserSessionIssuerIds:    []string{userIssuerID},
		ClientID:                clientID,
		ClientSecret:            &clientSecret,
		TokenEndpointAuthMethod: &authMethod,
		SessionToken:            nil,
		ApikeyToken:             nil,
		ProjectSlugInput:        nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result.TokenEndpointAuthMethod)
	require.Equal(t, "client_secret_post", *result.TokenEndpointAuthMethod)

	// Round-trip via Get to confirm the column survives a read after the
	// transaction closes.
	fetched, err := ti.service.GetRemoteSessionClient(ctx, &clientsgen.GetRemoteSessionClientPayload{
		ID:               result.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, fetched.TokenEndpointAuthMethod)
	require.Equal(t, "client_secret_post", *fetched.TokenEndpointAuthMethod)
}

func TestCreateRemoteSessionClient_Manual_AuthMethodOmittedStaysNil(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsc-nil", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-nil").String()

	clientID := "nil-client-id"
	result, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID:   issuerID,
		UserSessionIssuerIds:    []string{userIssuerID},
		ClientID:                clientID,
		ClientSecret:            nil,
		TokenEndpointAuthMethod: nil,
		SessionToken:            nil,
		ApikeyToken:             nil,
		ProjectSlugInput:        nil,
	})
	require.NoError(t, err)
	// NULL in storage surfaces as a nil pointer; runtime resolves to
	// client_secret_basic via resolveClientAuthMethod, but the API surface
	// preserves the unset state.
	require.Nil(t, result.TokenEndpointAuthMethod)
}

func TestCreateRemoteSessionClient_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsc-rbac", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-rbac").String()
	clientID := "rbac-client-id"

	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeProjectRead,
		Selector: authz.NewSelector(authz.ScopeProjectRead, authCtx.ProjectID.String()),
	})

	_, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerIds:  []string{userIssuerID},
		ClientID:              clientID,
		ClientSecret:          nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestCreateRemoteSessionClient_RejectsCrossProjectUserIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsc-xproj-usi", "")
	otherProject := createProject(t, ctx, ti.conn, "other-"+uuid.NewString()[:8])
	foreignUserIssuer := createUserSessionIssuerInProject(t, ctx, ti.conn, otherProject, "usi-foreign").String()

	_, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerIds:  []string{foreignUserIssuer},
		ClientID:              "xproj-usi-client",
		ClientSecret:          nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestCreateRemoteSessionClient_RejectsCrossProjectRemoteIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	otherProject := createProject(t, ctx, ti.conn, "other-"+uuid.NewString()[:8])
	foreignRemoteIssuer := createRemoteIssuerInProject(t, ctx, ti.conn, otherProject, "rsi-foreign").String()
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-xproj-rsi").String()

	_, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID: foreignRemoteIssuer,
		UserSessionIssuerIds:  []string{userIssuerID},
		ClientID:              "xproj-rsi-client",
		ClientSecret:          nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

// TestCreateRemoteSessionClient_OrgLevelIssuer binds a project-scoped client to
// an organization-level (cross-project, project_id IS NULL) issuer inherited
// from the project's org. AGE-2485 added inheritance for these issuers but
// deferred runtime consumption; the client stays project-owned while the issuer
// it references is shared across the org.
func TestCreateRemoteSessionClient_OrgLevelIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	issuerID := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "org-create-client")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "org-create-client")

	created, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID: issuerID.String(),
		UserSessionIssuerIds:  []string{userIssuerID.String()},
		ClientID:              "org-create-cid",
		ClientSecret:          nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)
	require.Equal(t, issuerID.String(), created.RemoteSessionIssuerID)

	clientUUID, err := uuid.Parse(created.ID)
	require.NoError(t, err)
	require.Equal(t, 1, countRemoteSessionClientUserSessionIssuerBindings(t, ctx, ti.conn, clientUUID, userIssuerID))
}

// TestCreateRemoteSessionClient_CrossOrgIssuerRejected confirms a caller cannot
// bind a client to an org-level issuer owned by a different organization: the
// reachability gate matches inherited issuers only when organization_id equals
// the caller's active org.
func TestCreateRemoteSessionClient_CrossOrgIssuerRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	otherOrg := createOrganization(t, ctx, ti.conn, "other-org-create")
	issuerID := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, otherOrg, "create-cross-org")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "create-cross-org").String()

	_, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID: issuerID.String(),
		UserSessionIssuerIds:  []string{userIssuerID},
		ClientID:              "cross-org-cid",
		ClientSecret:          nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestAttachUserSessionIssuer_RejectsCrossProjectUserIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsc-attach-xproj", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-attach-xproj").String()

	created, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerIds:  []string{userIssuerID},
		ClientID:              "attach-xproj-client",
		ClientSecret:          nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)

	otherProject := createProject(t, ctx, ti.conn, "other-"+uuid.NewString()[:8])
	foreignUserIssuer := createUserSessionIssuerInProject(t, ctx, ti.conn, otherProject, "usi-attach-foreign").String()

	_, err = ti.service.AttachUserSessionIssuer(ctx, &clientsgen.AttachUserSessionIssuerPayload{
		ID:                  created.ID,
		UserSessionIssuerID: foreignUserIssuer,
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)

	// The rejected attach rolls back / never happened: the original binding is
	// untouched and the foreign binding was never recorded.
	clientUUID, err := uuid.Parse(created.ID)
	require.NoError(t, err)
	userIssuerUUID, err := uuid.Parse(userIssuerID)
	require.NoError(t, err)
	foreignUserIssuerUUID, err := uuid.Parse(foreignUserIssuer)
	require.NoError(t, err)
	require.Equal(t, 1, countRemoteSessionClientUserSessionIssuerBindings(t, ctx, ti.conn, clientUUID, userIssuerUUID))
	require.Equal(t, 0, countRemoteSessionClientUserSessionIssuerBindings(t, ctx, ti.conn, clientUUID, foreignUserIssuerUUID))
}

func TestGetRemoteSessionClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsc-get", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-get").String()
	clientID := "get-client-id"

	created, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerIds:  []string{userIssuerID},
		ClientID:              clientID,
		ClientSecret:          nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)

	fetched, err := ti.service.GetRemoteSessionClient(ctx, &clientsgen.GetRemoteSessionClientPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, fetched.ID)
	require.Equal(t, clientID, fetched.ClientID)

	_, err = ti.service.GetRemoteSessionClient(ctx, &clientsgen.GetRemoteSessionClientPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

// TestGetRemoteSessionClientWithIssuerByID_OrgLevelIssuer guards the runtime
// token resolver (used by the refresh path): the joined client+issuer view must
// resolve an org-level issuer's token endpoint even though the issuer carries no
// project_id. The join keys purely on remote_session_issuer_id, so no project
// predicate filters the issuer side.
func TestGetRemoteSessionClientWithIssuerByID_OrgLevelIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	issuerID := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "org-tokenresolve")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "org-tokenresolve")
	clientID := createRemoteClient(t, ctx, ti, issuerID.String(), userIssuerID.String(), "org-resolve-cid")

	clientUUID, err := uuid.Parse(clientID)
	require.NoError(t, err)

	row, err := repo.New(ti.conn).GetRemoteSessionClientWithIssuerByID(ctx, clientUUID)
	require.NoError(t, err)
	require.Equal(t, issuerID, row.RemoteSessionIssuerID)
	require.Equal(t, "https://idp.example.com/token", row.TokenEndpoint.String, "token resolver joins to the org-level issuer's token endpoint")
}

func TestListRemoteSessionClients(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsc-list", "")

	for _, c := range []string{"list-client-1", "list-client-2"} {
		clientID := c
		userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-list-"+clientID).String()
		_, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
			RemoteSessionIssuerID: issuerID,
			UserSessionIssuerIds:  []string{userIssuerID},
			ClientID:              clientID,
			ClientSecret:          nil,
			SessionToken:          nil,
			ApikeyToken:           nil,
			ProjectSlugInput:      nil,
		})
		require.NoError(t, err)
	}

	result, err := ti.service.ListRemoteSessionClients(ctx, &clientsgen.ListRemoteSessionClientsPayload{
		RemoteSessionIssuerID: &issuerID,
		UserSessionIssuerID:   nil,
		Cursor:                nil,
		Limit:                 nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(result.Items), 2)
	for _, item := range result.Items {
		require.Equal(t, issuerID, item.RemoteSessionIssuerID)
	}
}

func TestListRemoteSessionClients_PaginationTraversal(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsc-page", "")

	const total = 5
	wantIDs := make(map[string]bool, total)
	for range total {
		clientID := uuid.NewString()
		userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-page-"+clientID).String()
		id := createRemoteClient(t, ctx, ti, issuerID, userIssuerID, clientID)
		wantIDs[id] = true
	}

	pageSize := 2
	gotIDs := make(map[string]bool, total)
	var cursor *string
	pages := 0
	for {
		pages++
		require.Less(t, pages, 10, "pagination did not terminate")
		result, err := ti.service.ListRemoteSessionClients(ctx, &clientsgen.ListRemoteSessionClientsPayload{
			RemoteSessionIssuerID: &issuerID,
			UserSessionIssuerID:   nil,
			Cursor:                cursor,
			Limit:                 &pageSize,
			SessionToken:          nil,
			ApikeyToken:           nil,
			ProjectSlugInput:      nil,
		})
		require.NoError(t, err)
		for _, item := range result.Items {
			require.False(t, gotIDs[item.ID], "duplicate id across pages: %s", item.ID)
			gotIDs[item.ID] = true
		}
		if result.NextCursor == nil {
			break
		}
		cursor = result.NextCursor
	}
	for id := range wantIDs {
		require.True(t, gotIDs[id], "client %s missing from paginated traversal", id)
	}
}

func TestUpdateRemoteSessionClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsc-update", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-update").String()
	clientID := "update-client-id"

	created, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerIds:  []string{userIssuerID},
		ClientID:              clientID,
		ClientSecret:          nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientUpdate)
	require.NoError(t, err)

	newSecret := "rotated-secret"
	updated, err := ti.service.UpdateRemoteSessionClient(ctx, &clientsgen.UpdateRemoteSessionClientPayload{
		ID:                      created.ID,
		ClientSecret:            &newSecret,
		TokenEndpointAuthMethod: nil,
		SessionToken:            nil,
		ApikeyToken:             nil,
		ProjectSlugInput:        nil,
	})
	require.NoError(t, err)
	require.Equal(t, created.ClientID, updated.ClientID)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestUpdateRemoteSessionClient_SwitchAuthMethod(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsc-switch", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-switch").String()
	clientID := "switch-client-id"

	// Start with default (NULL) auth method.
	created, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID:   issuerID,
		UserSessionIssuerIds:    []string{userIssuerID},
		ClientID:                clientID,
		ClientSecret:            nil,
		TokenEndpointAuthMethod: nil,
		SessionToken:            nil,
		ApikeyToken:             nil,
		ProjectSlugInput:        nil,
	})
	require.NoError(t, err)
	require.Nil(t, created.TokenEndpointAuthMethod)

	post := "client_secret_post"
	updated, err := ti.service.UpdateRemoteSessionClient(ctx, &clientsgen.UpdateRemoteSessionClientPayload{
		ID:                      created.ID,
		ClientSecret:            nil,
		TokenEndpointAuthMethod: &post,
		SessionToken:            nil,
		ApikeyToken:             nil,
		ProjectSlugInput:        nil,
	})
	require.NoError(t, err)
	require.NotNil(t, updated.TokenEndpointAuthMethod)
	require.Equal(t, "client_secret_post", *updated.TokenEndpointAuthMethod)
}

func TestCreateRemoteSessionClient_PersistsScopeOverride(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsc-scope-create", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-scope-create").String()

	clientID := "scope-create-client-id"
	scope := []string{"read:tools", "write:tools"}

	result, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID:   issuerID,
		UserSessionIssuerIds:    []string{userIssuerID},
		ClientID:                clientID,
		ClientSecret:            nil,
		TokenEndpointAuthMethod: nil,
		Scope:                   scope,
		SessionToken:            nil,
		ApikeyToken:             nil,
		ProjectSlugInput:        nil,
	})
	require.NoError(t, err)
	require.Equal(t, scope, result.Scope)

	fetched, err := ti.service.GetRemoteSessionClient(ctx, &clientsgen.GetRemoteSessionClientPayload{
		ID:               result.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, scope, fetched.Scope)
}

func TestCreateRemoteSessionClient_ScopeOmittedStaysNil(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsc-scope-omit", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-scope-omit").String()

	clientID := "scope-omit-client-id"
	result, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID:   issuerID,
		UserSessionIssuerIds:    []string{userIssuerID},
		ClientID:                clientID,
		ClientSecret:            nil,
		TokenEndpointAuthMethod: nil,
		Scope:                   nil,
		SessionToken:            nil,
		ApikeyToken:             nil,
		ProjectSlugInput:        nil,
	})
	require.NoError(t, err)
	// Absent means "fall back to the issuer's scopes_supported in the OAuth
	// dance"; the API surface keeps that distinct from the empty array.
	require.Nil(t, result.Scope)
}

func TestUpdateRemoteSessionClient_SetsScope(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsc-scope-update", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-scope-update").String()
	clientID := "scope-update-client-id"

	created, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID:   issuerID,
		UserSessionIssuerIds:    []string{userIssuerID},
		ClientID:                clientID,
		ClientSecret:            nil,
		TokenEndpointAuthMethod: nil,
		Scope:                   nil,
		SessionToken:            nil,
		ApikeyToken:             nil,
		ProjectSlugInput:        nil,
	})
	require.NoError(t, err)
	require.Nil(t, created.Scope)

	scope := []string{"read:tools"}
	updated, err := ti.service.UpdateRemoteSessionClient(ctx, &clientsgen.UpdateRemoteSessionClientPayload{
		ID:                      created.ID,
		ClientSecret:            nil,
		TokenEndpointAuthMethod: nil,
		Scope:                   scope,
		SessionToken:            nil,
		ApikeyToken:             nil,
		ProjectSlugInput:        nil,
	})
	require.NoError(t, err)
	require.Equal(t, scope, updated.Scope)
}

func TestCreateRemoteSessionClient_PersistsAudience(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsc-aud-create", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-aud-create").String()

	clientID := "aud-create-client-id"
	audience := "https://api.example.com"

	result, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID:   issuerID,
		UserSessionIssuerIds:    []string{userIssuerID},
		ClientID:                clientID,
		ClientSecret:            nil,
		TokenEndpointAuthMethod: nil,
		Scope:                   nil,
		Audience:                &audience,
		SessionToken:            nil,
		ApikeyToken:             nil,
		ProjectSlugInput:        nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result.Audience)
	require.Equal(t, audience, *result.Audience)

	fetched, err := ti.service.GetRemoteSessionClient(ctx, &clientsgen.GetRemoteSessionClientPayload{
		ID:               result.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, fetched.Audience)
	require.Equal(t, audience, *fetched.Audience)
}

func TestCreateRemoteSessionClient_AudienceOmittedStaysNil(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsc-aud-omit", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-aud-omit").String()

	clientID := "aud-omit-client-id"
	result, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID:   issuerID,
		UserSessionIssuerIds:    []string{userIssuerID},
		ClientID:                clientID,
		ClientSecret:            nil,
		TokenEndpointAuthMethod: nil,
		Scope:                   nil,
		Audience:                nil,
		SessionToken:            nil,
		ApikeyToken:             nil,
		ProjectSlugInput:        nil,
	})
	require.NoError(t, err)
	require.Nil(t, result.Audience)
}

func TestUpdateRemoteSessionClient_SetsAudience(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsc-aud-update", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-aud-update").String()
	clientID := "aud-update-client-id"

	created, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID:   issuerID,
		UserSessionIssuerIds:    []string{userIssuerID},
		ClientID:                clientID,
		ClientSecret:            nil,
		TokenEndpointAuthMethod: nil,
		Scope:                   nil,
		Audience:                nil,
		SessionToken:            nil,
		ApikeyToken:             nil,
		ProjectSlugInput:        nil,
	})
	require.NoError(t, err)
	require.Nil(t, created.Audience)

	audience := "https://api.example.com"
	updated, err := ti.service.UpdateRemoteSessionClient(ctx, &clientsgen.UpdateRemoteSessionClientPayload{
		ID:                      created.ID,
		ClientSecret:            nil,
		TokenEndpointAuthMethod: nil,
		Scope:                   nil,
		Audience:                &audience,
		SessionToken:            nil,
		ApikeyToken:             nil,
		ProjectSlugInput:        nil,
	})
	require.NoError(t, err)
	require.NotNil(t, updated.Audience)
	require.Equal(t, audience, *updated.Audience)
}

func TestDeleteRemoteSessionClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsc-delete", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-delete").String()
	clientID := "delete-client-id"

	created, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerIds:  []string{userIssuerID},
		ClientID:              clientID,
		ClientSecret:          nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)

	clientUUID, err := uuid.Parse(created.ID)
	require.NoError(t, err)
	userIssuerUUID, err := uuid.Parse(userIssuerID)
	require.NoError(t, err)

	_, err = repo.New(ti.conn).InsertRemoteSession(ctx, repo.InsertRemoteSessionParams{
		SubjectUrn:            urn.NewUserSubject("test-principal"),
		UserSessionIssuerID:   userIssuerUUID,
		RemoteSessionClientID: clientUUID,
		AccessTokenEncrypted:  "ciphertext",
		AccessExpiresAt:       pgtype.Timestamptz{Time: time.Now().Add(time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
	})
	require.NoError(t, err)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientDelete)
	require.NoError(t, err)

	err = ti.service.DeleteRemoteSessionClient(ctx, &clientsgen.DeleteRemoteSessionClientPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientDelete)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)

	// Get should now miss.
	_, err = ti.service.GetRemoteSessionClient(ctx, &clientsgen.GetRemoteSessionClientPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)

	activeSessions, err := repo.New(ti.conn).CountActiveRemoteSessionsByClientID(ctx, clientUUID)
	require.NoError(t, err)
	require.Equal(t, int64(0), activeSessions)
	require.Equal(t, 0, countRemoteSessionClientUserSessionIssuerBindings(t, ctx, ti.conn, clientUUID, userIssuerUUID))
}

func TestCloneClientFromOAuthProxyProvider_HappyPath(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)

	secrets := []byte(`{"client_id":"upstream-cid","client_secret":"upstream-shhh"}`)
	proxyProviderID, _ := insertProxyProvider(t, ctx, ti.conn, "clone-happy", "custom", secrets)
	issuerID := createRemoteIssuer(t, ctx, ti, "clone-happy", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "clone-happy").String()

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientCreate)
	require.NoError(t, err)

	result, err := ti.service.CloneClientFromOAuthProxyProvider(ctx, &clientsgen.CloneClientFromOAuthProxyProviderPayload{
		OauthProxyProviderID:  proxyProviderID.String(),
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerIds:  []string{userIssuerID},
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)
	require.Equal(t, "upstream-cid", result.ClientID, "preserves upstream client_id so existing registrations keep working")
	require.Equal(t, issuerID, result.RemoteSessionIssuerID)
	require.Equal(t, []string{userIssuerID}, result.UserSessionIssuerIds)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestCloneClientFromOAuthProxyProvider_NonAdminRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	// No withAdmin: the realistic default user is not an admin.

	secrets := []byte(`{"client_id":"upstream-cid","client_secret":"upstream-shhh"}`)
	proxyProviderID, _ := insertProxyProvider(t, ctx, ti.conn, "clone-non-admin", "custom", secrets)
	issuerID := createRemoteIssuer(t, ctx, ti, "clone-non-admin", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "clone-non-admin").String()

	_, err := ti.service.CloneClientFromOAuthProxyProvider(ctx, &clientsgen.CloneClientFromOAuthProxyProviderPayload{
		OauthProxyProviderID:  proxyProviderID.String(),
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerIds:  []string{userIssuerID},
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestCloneClientFromOAuthProxyProvider_RejectsGramProvider(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)

	// "gram" providers don't store a usable upstream client; clone should refuse.
	secrets := []byte(`{}`)
	proxyProviderID, _ := insertProxyProvider(t, ctx, ti.conn, "clone-gram", "gram", secrets)
	issuerID := createRemoteIssuer(t, ctx, ti, "clone-gram", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "clone-gram").String()

	_, err := ti.service.CloneClientFromOAuthProxyProvider(ctx, &clientsgen.CloneClientFromOAuthProxyProviderPayload{
		OauthProxyProviderID:  proxyProviderID.String(),
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerIds:  []string{userIssuerID},
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCloneClientFromOAuthProxyProvider_EnvBackedSecrets(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)

	// Operators commonly store CLIENT_ID / CLIENT_SECRET in an environment and
	// reference it from the proxy provider's secrets via environment_slug. The
	// clone path resolves these the same way the runtime OAuth proxy does so
	// cutover works for existing env-backed providers without forcing operators
	// to inline credentials first.
	envSlug := seedEnvironmentWithEntries(t, ctx, ti, "envback-ok", map[string]string{
		"CLIENT_ID":     "env-upstream-cid",
		"CLIENT_SECRET": "env-upstream-shhh",
	})
	secrets := []byte(`{"environment_slug":"` + envSlug + `"}`)
	proxyProviderID, _ := insertProxyProvider(t, ctx, ti.conn, "clone-envback-ok", "custom", secrets)
	issuerID := createRemoteIssuer(t, ctx, ti, "clone-envback-ok", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "clone-envback-ok").String()

	result, err := ti.service.CloneClientFromOAuthProxyProvider(ctx, &clientsgen.CloneClientFromOAuthProxyProviderPayload{
		OauthProxyProviderID:  proxyProviderID.String(),
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerIds:  []string{userIssuerID},
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)
	require.Equal(t, "env-upstream-cid", result.ClientID, "resolves CLIENT_ID from the linked environment case-insensitively")
}

func TestCloneClientFromOAuthProxyProvider_EnvMissingCredential(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)

	// Environment exists and is linked, but CLIENT_SECRET is absent. The clone
	// must surface a bad-request rather than persist a half-populated client.
	envSlug := seedEnvironmentWithEntries(t, ctx, ti, "envback-missing", map[string]string{
		"CLIENT_ID": "only-cid",
	})
	secrets := []byte(`{"environment_slug":"` + envSlug + `"}`)
	proxyProviderID, _ := insertProxyProvider(t, ctx, ti.conn, "clone-envback-missing", "custom", secrets)
	issuerID := createRemoteIssuer(t, ctx, ti, "clone-envback-missing", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "clone-envback-missing").String()

	_, err := ti.service.CloneClientFromOAuthProxyProvider(ctx, &clientsgen.CloneClientFromOAuthProxyProviderPayload{
		OauthProxyProviderID:  proxyProviderID.String(),
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerIds:  []string{userIssuerID},
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCloneClientFromOAuthProxyProvider_ProviderNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)

	issuerID := createRemoteIssuer(t, ctx, ti, "clone-missing", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "clone-missing").String()

	_, err := ti.service.CloneClientFromOAuthProxyProvider(ctx, &clientsgen.CloneClientFromOAuthProxyProviderPayload{
		OauthProxyProviderID:  uuid.NewString(),
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerIds:  []string{userIssuerID},
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

// TestCloneClientFromOAuthProxyProvider_OrgLevelIssuer is the regression test
// for AGE-2593: the clone path used to force a project-only issuer lookup, so
// cloning onto an inherited org-level issuer returned a spurious 404. The
// dashboard "clone" wizard lets operators pick inherited issuers, so this is a
// reachable path.
func TestCloneClientFromOAuthProxyProvider_OrgLevelIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	secrets := []byte(`{"client_id":"upstream-cid","client_secret":"upstream-shhh"}`)
	proxyProviderID, _ := insertProxyProvider(t, ctx, ti.conn, "clone-org", "custom", secrets)
	issuerID := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "clone-org")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "clone-org").String()

	result, err := ti.service.CloneClientFromOAuthProxyProvider(ctx, &clientsgen.CloneClientFromOAuthProxyProviderPayload{
		OauthProxyProviderID:  proxyProviderID.String(),
		RemoteSessionIssuerID: issuerID.String(),
		UserSessionIssuerIds:  []string{userIssuerID},
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)
	require.Equal(t, "upstream-cid", result.ClientID)
	require.Equal(t, issuerID.String(), result.RemoteSessionIssuerID)
}

// TestCloneClientFromOAuthProxyProvider_CrossOrgIssuerRejected confirms the
// widened clone lookup still refuses an org-level issuer owned by a different
// organization.
func TestCloneClientFromOAuthProxyProvider_CrossOrgIssuerRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)

	otherOrg := createOrganization(t, ctx, ti.conn, "other-org-clone")
	issuerID := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, otherOrg, "clone-cross-org")

	secrets := []byte(`{"client_id":"upstream-cid","client_secret":"upstream-shhh"}`)
	proxyProviderID, _ := insertProxyProvider(t, ctx, ti.conn, "clone-cross-org", "custom", secrets)
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "clone-cross-org").String()

	_, err := ti.service.CloneClientFromOAuthProxyProvider(ctx, &clientsgen.CloneClientFromOAuthProxyProviderPayload{
		OauthProxyProviderID:  proxyProviderID.String(),
		RemoteSessionIssuerID: issuerID.String(),
		UserSessionIssuerIds:  []string{userIssuerID},
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestDeleteRemoteSessionClient_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientDelete)
	require.NoError(t, err)

	err = ti.service.DeleteRemoteSessionClient(ctx, &clientsgen.DeleteRemoteSessionClientPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "delete is idempotent: missing client returns success")

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientDelete)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount, "no audit entry when there was nothing to delete")
}

func TestAttachUserSessionIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsc-attach", "")

	// Standalone client: no user_session_issuer attachments.
	created, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerIds:  nil,
		ClientID:              "attach-client-id",
		ClientSecret:          nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)
	require.Empty(t, created.UserSessionIssuerIds)

	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-attach")
	userIssuerIDString := userIssuerID.String()

	clientUUID, err := uuid.Parse(created.ID)
	require.NoError(t, err)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientAttachUserSessionIssuer)
	require.NoError(t, err)

	attached, err := ti.service.AttachUserSessionIssuer(ctx, &clientsgen.AttachUserSessionIssuerPayload{
		ID:                  created.ID,
		UserSessionIssuerID: userIssuerIDString,
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
	})
	require.NoError(t, err)
	require.Equal(t, []string{userIssuerIDString}, attached.UserSessionIssuerIds)
	require.Equal(t, 1, countRemoteSessionClientUserSessionIssuerBindings(t, ctx, ti.conn, clientUUID, userIssuerID))

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientAttachUserSessionIssuer)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)

	// Re-attaching the same issuer is a no-op success: still exactly one binding.
	reattached, err := ti.service.AttachUserSessionIssuer(ctx, &clientsgen.AttachUserSessionIssuerPayload{
		ID:                  created.ID,
		UserSessionIssuerID: userIssuerIDString,
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
	})
	require.NoError(t, err)
	require.Equal(t, []string{userIssuerIDString}, reattached.UserSessionIssuerIds)
	require.Equal(t, 1, countRemoteSessionClientUserSessionIssuerBindings(t, ctx, ti.conn, clientUUID, userIssuerID))
}

func TestDetachUserSessionIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsc-detach", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-detach")
	userIssuerIDString := userIssuerID.String()

	created, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerIds:  []string{userIssuerIDString},
		ClientID:              "detach-client-id",
		ClientSecret:          nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)
	require.Equal(t, []string{userIssuerIDString}, created.UserSessionIssuerIds)

	clientUUID, err := uuid.Parse(created.ID)
	require.NoError(t, err)
	require.Equal(t, 1, countRemoteSessionClientUserSessionIssuerBindings(t, ctx, ti.conn, clientUUID, userIssuerID))

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientDetachUserSessionIssuer)
	require.NoError(t, err)

	detached, err := ti.service.DetachUserSessionIssuer(ctx, &clientsgen.DetachUserSessionIssuerPayload{
		ID:                  created.ID,
		UserSessionIssuerID: userIssuerIDString,
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
	})
	require.NoError(t, err)
	require.Empty(t, detached.UserSessionIssuerIds)
	require.Equal(t, 0, countRemoteSessionClientUserSessionIssuerBindings(t, ctx, ti.conn, clientUUID, userIssuerID))

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientDetachUserSessionIssuer)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)

	// Detaching an absent binding is a no-op success.
	redetached, err := ti.service.DetachUserSessionIssuer(ctx, &clientsgen.DetachUserSessionIssuerPayload{
		ID:                  created.ID,
		UserSessionIssuerID: userIssuerIDString,
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
	})
	require.NoError(t, err)
	require.Empty(t, redetached.UserSessionIssuerIds)
	require.Equal(t, 0, countRemoteSessionClientUserSessionIssuerBindings(t, ctx, ti.conn, clientUUID, userIssuerID))
}

func TestAttachUserSessionIssuer_RejectsDuplicateRemoteIssuerBinding(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsc-attach-dup", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-attach-dup").String()

	// Client A binds (user_session_issuer U, remote_session_issuer R).
	createRemoteClient(t, ctx, ti, issuerID, userIssuerID, "attach-dup-client-a")

	// Client B is standalone on the same remote issuer R.
	clientB, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerIds:  nil,
		ClientID:              "attach-dup-client-b",
		ClientSecret:          nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)

	// Attaching U to B violates the single-client-per-(U, R) guard.
	_, err = ti.service.AttachUserSessionIssuer(ctx, &clientsgen.AttachUserSessionIssuerPayload{
		ID:                  clientB.ID,
		UserSessionIssuerID: userIssuerID,
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeConflict)
}

// TestCreateRemoteSessionClient_MultipleIssuers creates one client attached to
// several user_session_issuers in a single call and asserts the returned array
// is sorted (regardless of input order) and every binding landed.
func TestCreateRemoteSessionClient_MultipleIssuers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsc-multi", "")
	u1 := createUserSessionIssuer(t, ctx, ti.conn, "usi-multi-1")
	u2 := createUserSessionIssuer(t, ctx, ti.conn, "usi-multi-2")
	u3 := createUserSessionIssuer(t, ctx, ti.conn, "usi-multi-3")

	want := []string{u1.String(), u2.String(), u3.String()}
	sort.Strings(want)

	// Pass the issuers out of sorted order; the result must come back sorted.
	created, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerIds:  []string{u3.String(), u1.String(), u2.String()},
		ClientID:              "multi-client-id",
		ClientSecret:          nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)
	require.Equal(t, want, created.UserSessionIssuerIds)

	clientUUID, err := uuid.Parse(created.ID)
	require.NoError(t, err)
	for _, u := range []uuid.UUID{u1, u2, u3} {
		require.Equal(t, 1, countRemoteSessionClientUserSessionIssuerBindings(t, ctx, ti.conn, clientUUID, u))
	}
}

// TestCreateRemoteSessionClient_MultiIssuerRollbackOnInvalid verifies the whole
// create rolls back when any issuer in the array is unreachable: no client row
// and no bindings survive.
func TestCreateRemoteSessionClient_MultiIssuerRollbackOnInvalid(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsc-multi-rollback", "")
	valid := createUserSessionIssuer(t, ctx, ti.conn, "usi-multi-valid")
	otherProject := createProject(t, ctx, ti.conn, "other-"+uuid.NewString()[:8])
	foreign := createUserSessionIssuerInProject(t, ctx, ti.conn, otherProject, "usi-multi-foreign")

	_, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerIds:  []string{valid.String(), foreign.String()},
		ClientID:              "multi-rollback-client-id",
		ClientSecret:          nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)

	// No orphan client row was committed for the remote issuer.
	list, err := ti.service.ListRemoteSessionClients(ctx, &clientsgen.ListRemoteSessionClientsPayload{
		RemoteSessionIssuerID: &issuerID,
		UserSessionIssuerID:   nil,
		Cursor:                nil,
		Limit:                 nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)
	require.Empty(t, list.Items)
}

// TestCreateRemoteSessionClient_MultiIssuerGuardConflictRollsBack verifies that
// when one issuer in the array collides with an existing client on the same
// remote issuer, the create is rejected and nothing is persisted.
func TestCreateRemoteSessionClient_MultiIssuerGuardConflictRollsBack(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsc-multi-guard", "")
	u1 := createUserSessionIssuer(t, ctx, ti.conn, "usi-multi-guard-1")
	u2 := createUserSessionIssuer(t, ctx, ti.conn, "usi-multi-guard-2")

	// Client A already binds (u1, R).
	createRemoteClient(t, ctx, ti, issuerID, u1.String(), "multi-guard-client-a")

	// Client B requests [u2, u1] on R: the u1 binding collides with A.
	_, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerIds:  []string{u2.String(), u1.String()},
		ClientID:              "multi-guard-client-b",
		ClientSecret:          nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeConflict)

	// B was never created: only A remains on the remote issuer.
	listR, err := ti.service.ListRemoteSessionClients(ctx, &clientsgen.ListRemoteSessionClientsPayload{
		RemoteSessionIssuerID: &issuerID,
		UserSessionIssuerID:   nil,
		Cursor:                nil,
		Limit:                 nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)
	require.Len(t, listR.Items, 1)

	// u2 picked up no binding from the rolled-back partial attempt.
	u2str := u2.String()
	listU2, err := ti.service.ListRemoteSessionClients(ctx, &clientsgen.ListRemoteSessionClientsPayload{
		RemoteSessionIssuerID: nil,
		UserSessionIssuerID:   &u2str,
		Cursor:                nil,
		Limit:                 nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)
	require.Empty(t, listU2.Items)
}

// TestCreateRemoteSessionClient_DeduplicatesIssuers confirms a repeated issuer in
// the input collapses to a single binding and a single-element result array.
func TestCreateRemoteSessionClient_DeduplicatesIssuers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rsc-dedupe", "")
	u1 := createUserSessionIssuer(t, ctx, ti.conn, "usi-dedupe")
	u1str := u1.String()

	created, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerIds:  []string{u1str, u1str},
		ClientID:              "dedupe-client-id",
		ClientSecret:          nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)
	require.Equal(t, []string{u1str}, created.UserSessionIssuerIds)

	clientUUID, err := uuid.Parse(created.ID)
	require.NoError(t, err)
	require.Equal(t, 1, countRemoteSessionClientUserSessionIssuerBindings(t, ctx, ti.conn, clientUUID, u1))
}
