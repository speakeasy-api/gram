package remotesessions_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestListRemoteSessions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rs-list", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-rs-list").String()
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID, "rs-list-client")

	live := insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject("user_live"), userIssuerID, clientID)
	soft := insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject("user_soft"), userIssuerID, clientID)

	// Soft-delete one row directly so it must be excluded from the listing.
	_, err := repo.New(ti.conn).RevokeRemoteSession(ctx, repo.RevokeRemoteSessionParams{
		ID:        soft.ID,
		ProjectID: liveProjectID(t, ctx),
	})
	require.NoError(t, err)

	result, err := ti.service.ListRemoteSessions(ctx, &gen.ListRemoteSessionsPayload{
		SubjectUrn:            nil,
		RemoteSessionClientID: nil,
		Cursor:                nil,
		Limit:                 nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)

	ids := make(map[string]bool, len(result.Items))
	for _, item := range result.Items {
		require.Equal(t, clientID, item.RemoteSessionClientID)
		require.Equal(t, userIssuerID, item.UserSessionIssuerID)
		ids[item.ID] = true
	}
	require.True(t, ids[live.ID.String()], "live session must be returned")
	require.False(t, ids[soft.ID.String()], "soft-deleted session must be excluded")
}

// TestListRemoteSessions_OrgLevelClientScopedByUserSessionIssuerProject proves
// the project session list is scoped by the session's user_session_issuer
// project: a session established through an organization-level client bound to
// this project's user_session_issuer is listed, while another project's session
// on the same shared org-level client is not.
func TestListRemoteSessions_OrgLevelClientScopedByUserSessionIssuerProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	orgIssuer := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "rs-orglevel-issuer")
	callerUserIssuer := createUserSessionIssuer(t, ctx, ti.conn, "usi-rs-orglevel-caller")
	otherProject := createProject(t, ctx, ti.conn, "rs-orglevel-other-project")
	otherUserIssuer := createUserSessionIssuerInProject(t, ctx, ti.conn, otherProject, "usi-rs-orglevel-other")
	orgClient := seedOrgLevelRemoteClient(t, ctx, ti.conn, authCtx.ActiveOrganizationID, orgIssuer, "rs-orglevel-client", callerUserIssuer, otherUserIssuer)

	mine := insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject("user_mine"), callerUserIssuer.String(), orgClient.String())
	theirs := insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject("user_theirs"), otherUserIssuer.String(), orgClient.String())

	result, err := ti.service.ListRemoteSessions(ctx, &gen.ListRemoteSessionsPayload{})
	require.NoError(t, err)

	ids := make(map[string]bool, len(result.Items))
	for _, item := range result.Items {
		ids[item.ID] = true
	}
	require.True(t, ids[mine.ID.String()], "org-level client session for this project's user_session_issuer must be listed")
	require.False(t, ids[theirs.ID.String()], "another project's session on the shared org-level client must not be listed")
}

// TestRevokeRemoteSession_OrgLevelClientFromOwningProject confirms a project
// admin can revoke a session established through an organization-level client
// bound to their own user_session_issuer.
func TestRevokeRemoteSession_OrgLevelClientFromOwningProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	orgIssuer := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "rs-revoke-issuer")
	userIssuer := createUserSessionIssuer(t, ctx, ti.conn, "usi-rs-revoke")
	orgClient := seedOrgLevelRemoteClient(t, ctx, ti.conn, authCtx.ActiveOrganizationID, orgIssuer, "rs-revoke-client", userIssuer)
	session := insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject("user_revoke"), userIssuer.String(), orgClient.String())

	require.NoError(t, ti.service.RevokeRemoteSession(ctx, &gen.RevokeRemoteSessionPayload{ID: session.ID.String()}))

	result, err := ti.service.ListRemoteSessions(ctx, &gen.ListRemoteSessionsPayload{})
	require.NoError(t, err)
	for _, item := range result.Items {
		require.NotEqual(t, session.ID.String(), item.ID, "revoked session must no longer be listed")
	}
}

// TestRevokeRemoteSession_OtherProjectsOrgLevelSessionNotRevoked confirms a
// project admin cannot revoke a session on a shared organization-level client
// that belongs to another project's user_session_issuer; the revoke resolves to
// a no-op and the session stays active.
func TestRevokeRemoteSession_OtherProjectsOrgLevelSessionNotRevoked(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	orgIssuer := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "rs-xproj-issuer")
	otherProject := createProject(t, ctx, ti.conn, "rs-xproj-other-project")
	otherUserIssuer := createUserSessionIssuerInProject(t, ctx, ti.conn, otherProject, "usi-rs-xproj-other")
	orgClient := seedOrgLevelRemoteClient(t, ctx, ti.conn, authCtx.ActiveOrganizationID, orgIssuer, "rs-xproj-client", otherUserIssuer)
	subject := urn.NewUserSubject("user_xproj")
	session := insertRemoteSession(t, ctx, ti.conn, subject, otherUserIssuer.String(), orgClient.String())

	require.NoError(t, ti.service.RevokeRemoteSession(ctx, &gen.RevokeRemoteSessionPayload{ID: session.ID.String()}))

	got, err := repo.New(ti.conn).GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            subject,
		RemoteSessionClientID: orgClient,
	})
	require.NoError(t, err)
	require.Equal(t, session.ID, got.ID, "another project's session must remain active")
}

func TestListRemoteSessions_ResolvesUserSubjectIdentity(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rs-resolve", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-rs-resolve").String()
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID, "rs-resolve-client")

	seedUser(t, ctx, ti.conn, "user_resolve", "ada@example.com", "Ada Lovelace")

	userSession := insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject("user_resolve"), userIssuerID, clientID)
	anonSession := insertRemoteSession(t, ctx, ti.conn, urn.NewAnonymousSubject("mcp-session-anon"), userIssuerID, clientID)

	result, err := ti.service.ListRemoteSessions(ctx, &gen.ListRemoteSessionsPayload{
		SubjectUrn:            nil,
		RemoteSessionClientID: nil,
		Cursor:                nil,
		Limit:                 nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)

	byID := make(map[string]*types.RemoteSession, len(result.Items))
	for _, item := range result.Items {
		byID[item.ID] = item
	}

	resolved := byID[userSession.ID.String()]
	require.NotNil(t, resolved, "user session must be returned")
	require.NotNil(t, resolved.SubjectDisplayName)
	require.Equal(t, "Ada Lovelace", *resolved.SubjectDisplayName)
	require.NotNil(t, resolved.SubjectEmail)
	require.Equal(t, "ada@example.com", *resolved.SubjectEmail)

	// Anonymous subjects have no users row, so resolution stays nil.
	anon := byID[anonSession.ID.String()]
	require.NotNil(t, anon, "anonymous session must be returned")
	require.Nil(t, anon.SubjectDisplayName)
	require.Nil(t, anon.SubjectEmail)
}

func TestListRemoteSessions_FilteredByPrincipal(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rs-list-princ", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-rs-list-princ").String()
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID, "rs-list-princ-client")

	alice := urn.NewUserSubject("user_alice")
	insertRemoteSession(t, ctx, ti.conn, alice, userIssuerID, clientID)
	insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject("user_bob"), userIssuerID, clientID)

	filter := alice.String()
	result, err := ti.service.ListRemoteSessions(ctx, &gen.ListRemoteSessionsPayload{
		SubjectUrn:            &filter,
		RemoteSessionClientID: nil,
		Cursor:                nil,
		Limit:                 nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, filter, result.Items[0].SubjectUrn)
}

func TestListRemoteSessions_FilteredByClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rs-list-client", "")
	userIssuerAID := createUserSessionIssuer(t, ctx, ti.conn, "usi-rs-list-client-a").String()
	userIssuerBID := createUserSessionIssuer(t, ctx, ti.conn, "usi-rs-list-client-b").String()
	clientA := createRemoteClient(t, ctx, ti, issuerID, userIssuerAID, "rs-list-client-a")
	clientB := createRemoteClient(t, ctx, ti, issuerID, userIssuerBID, "rs-list-client-b")

	insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject("user_in_a"), userIssuerAID, clientA)
	insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject("user_in_b"), userIssuerBID, clientB)

	result, err := ti.service.ListRemoteSessions(ctx, &gen.ListRemoteSessionsPayload{
		SubjectUrn:            nil,
		RemoteSessionClientID: &clientA,
		Cursor:                nil,
		Limit:                 nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, clientA, result.Items[0].RemoteSessionClientID)
}

func TestListRemoteSessions_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.ListRemoteSessions(ctx, &gen.ListRemoteSessionsPayload{
		SubjectUrn:            nil,
		RemoteSessionClientID: nil,
		Cursor:                nil,
		Limit:                 nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestListRemoteSessions_PaginationTraversal(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rs-page", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-rs-page").String()
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID, "rs-page-client")

	const total = 5
	wantIDs := make(map[string]bool, total)
	for range total {
		row := insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject(uuid.NewString()), userIssuerID, clientID)
		wantIDs[row.ID.String()] = true
	}

	pageSize := 2
	gotIDs := make(map[string]bool, total)
	var cursor *string
	pages := 0
	for {
		pages++
		require.Less(t, pages, 10, "pagination did not terminate")
		result, err := ti.service.ListRemoteSessions(ctx, &gen.ListRemoteSessionsPayload{
			SubjectUrn:            nil,
			RemoteSessionClientID: &clientID,
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
		require.True(t, gotIDs[id], "session %s missing from paginated traversal", id)
	}
}

func TestRevokeRemoteSession(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rs-revoke", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-rs-revoke").String()
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID, "rs-revoke-client")

	session := insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject("user_revoke"), userIssuerID, clientID)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionDelete)
	require.NoError(t, err)

	err = ti.service.RevokeRemoteSession(ctx, &gen.RevokeRemoteSessionPayload{
		ID:               session.ID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionDelete)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)

	// Subsequent list must omit the revoked row.
	result, err := ti.service.ListRemoteSessions(ctx, &gen.ListRemoteSessionsPayload{
		SubjectUrn:            nil,
		RemoteSessionClientID: &clientID,
		Cursor:                nil,
		Limit:                 nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)
	for _, item := range result.Items {
		require.NotEqual(t, session.ID.String(), item.ID)
	}
}

func TestRevokeRemoteSession_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionDelete)
	require.NoError(t, err)

	err = ti.service.RevokeRemoteSession(ctx, &gen.RevokeRemoteSessionPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "revoke is idempotent: missing session returns success")

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionDelete)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount, "no audit entry when there was nothing to revoke")
}

func liveProjectID(t *testing.T, ctx context.Context) uuid.UUID {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	return *authCtx.ProjectID
}
