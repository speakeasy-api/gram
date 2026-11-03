package access

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const temporaryGrantRecipientID = "local_investigator"

func seedTemporaryGrantRecipient(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string) urn.Principal {
	t.Helper()
	seedConnectedUser(t, ctx, conn, orgID, temporaryGrantRecipientID, "investigator@test.com", "Investigator", "workos_investigator", "membership_investigator")
	return urn.NewPrincipal(urn.PrincipalTypeUser, temporaryGrantRecipientID)
}

func inOneHour() string {
	return time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
}

func createTemporaryGrant(t *testing.T, ctx context.Context, ti *testInstance, userID string, scope authz.Scope, reason *string) *gen.TemporaryGrant {
	t.Helper()
	grant, err := ti.service.CreateTemporaryGrant(ctx, &gen.CreateTemporaryGrantPayload{
		SessionToken: nil,
		UserID:       userID,
		Scope:        string(scope),
		Selector:     nil,
		ExpiresAt:    inOneHour(),
		Reason:       reason,
	})
	require.NoError(t, err)
	return grant
}

// expireTemporaryGrant moves a grant's deadline into the past, standing in for
// the passage of time.
func expireTemporaryGrant(t *testing.T, ctx context.Context, conn *pgxpool.Pool, id string) {
	t.Helper()
	_, err := conn.Exec(ctx, `UPDATE principal_grants SET expires_at = clock_timestamp() - interval '1 second' WHERE id = $1`, id)
	require.NoError(t, err)
}

func loadMemberGrants(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID, userID string) []authz.Grant {
	t.Helper()
	principals, err := authz.ResolveUserPrincipals(ctx, conn, orgID, userID)
	require.NoError(t, err)
	grants, err := authz.LoadGrants(ctx, conn, orgID, principals)
	require.NoError(t, err)
	return grants
}

func chatReadCheck(orgID string) authz.Check {
	return authz.Check{Scope: authz.ScopeChatRead, ResourceKind: "", ResourceID: orgID, Dimensions: nil}
}

func TestTemporaryGrant_AuthorizesUntilDeadline(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID
	seedTemporaryGrantRecipient(t, ctx, ti.conn, orgID)

	allowed, err := authz.GrantsAuthorize(loadMemberGrants(t, ctx, ti.conn, orgID, temporaryGrantRecipientID), chatReadCheck(orgID))
	require.NoError(t, err)
	require.False(t, allowed, "the member starts without chat:read")

	grant := createTemporaryGrant(t, ctx, ti, temporaryGrantRecipientID, authz.ScopeChatRead, new("Investigate a reported session"))
	require.Equal(t, "active", grant.Status)
	require.Positive(t, grant.RemainingSeconds)
	require.LessOrEqual(t, grant.RemainingSeconds, int64(time.Hour/time.Second))
	require.Empty(t, grant.OtherAccess)
	require.Equal(t, "chat", grant.Selector.ResourceKind)
	require.Equal(t, authz.WildcardResource, grant.Selector.ResourceID)

	allowed, err = authz.GrantsAuthorize(loadMemberGrants(t, ctx, ti.conn, orgID, temporaryGrantRecipientID), chatReadCheck(orgID))
	require.NoError(t, err)
	require.True(t, allowed, "the temporary grant authorizes requests while active")

	expireTemporaryGrant(t, ctx, ti.conn, grant.ID)

	allowed, err = authz.GrantsAuthorize(loadMemberGrants(t, ctx, ti.conn, orgID, temporaryGrantRecipientID), chatReadCheck(orgID))
	require.NoError(t, err)
	require.False(t, allowed, "the next request after the deadline is not authorized, with no role change")

	require.Empty(t, listPrincipalGrants(t, ctx, ti.conn, orgID, urn.NewPrincipal(urn.PrincipalTypeUser, temporaryGrantRecipientID)), "management reads ignore the expired row")

	active, err := ti.service.ListTemporaryGrants(ctx, &gen.ListTemporaryGrantsPayload{SessionToken: nil, UserID: nil, IncludeExpired: false})
	require.NoError(t, err)
	require.Empty(t, active.Grants)

	all, err := ti.service.ListTemporaryGrants(ctx, &gen.ListTemporaryGrantsPayload{SessionToken: nil, UserID: nil, IncludeExpired: true})
	require.NoError(t, err)
	require.Len(t, all.Grants, 1)
	require.Equal(t, "expired", all.Grants[0].Status)
	require.Zero(t, all.Grants[0].RemainingSeconds)
}

func TestTemporaryGrant_ExpiredGrantCanBeGivenAgain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID
	seedTemporaryGrantRecipient(t, ctx, ti.conn, orgID)

	first := createTemporaryGrant(t, ctx, ti, temporaryGrantRecipientID, authz.ScopeLogRead, nil)
	expireTemporaryGrant(t, ctx, ti.conn, first.ID)

	second := createTemporaryGrant(t, ctx, ti, temporaryGrantRecipientID, authz.ScopeLogRead, nil)
	require.Equal(t, first.ID, second.ID, "the expired row for the same target is restarted")
	require.Equal(t, "active", second.Status)
}

func TestTemporaryGrant_ConflictsWithExistingGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID
	recipient := seedTemporaryGrantRecipient(t, ctx, ti.conn, orgID)

	seedGrant(t, ctx, ti.conn, orgID, recipient, authz.ScopeLogRead, authz.WildcardResource)
	_, err := ti.service.CreateTemporaryGrant(ctx, &gen.CreateTemporaryGrantPayload{SessionToken: nil, UserID: temporaryGrantRecipientID, Scope: string(authz.ScopeLogRead), Selector: nil, ExpiresAt: inOneHour(), Reason: nil})
	requireOopsCode(t, err, oops.CodeConflict)

	rows := listPrincipalGrants(t, ctx, ti.conn, orgID, recipient)
	require.Len(t, rows, 1)

	createTemporaryGrant(t, ctx, ti, temporaryGrantRecipientID, authz.ScopeChatRead, nil)
	_, err = ti.service.CreateTemporaryGrant(ctx, &gen.CreateTemporaryGrantPayload{SessionToken: nil, UserID: temporaryGrantRecipientID, Scope: string(authz.ScopeChatRead), Selector: nil, ExpiresAt: inOneHour(), Reason: nil})
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestTemporaryGrant_PermanentWriteTakesOverTemporaryRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID
	recipient := seedTemporaryGrantRecipient(t, ctx, ti.conn, orgID)

	grant := createTemporaryGrant(t, ctx, ti, temporaryGrantRecipientID, authz.ScopeChatWrite, nil)
	seedGrant(t, ctx, ti.conn, orgID, recipient, authz.ScopeChatWrite, authz.WildcardResource)

	var expiresAt *time.Time
	require.NoError(t, ti.conn.QueryRow(ctx, `SELECT expires_at FROM principal_grants WHERE id = $1`, grant.ID).Scan(&expiresAt))
	require.Nil(t, expiresAt, "writing the grant permanently clears its deadline")

	_, err := ti.service.RevokeTemporaryGrant(ctx, &gen.RevokeTemporaryGrantPayload{SessionToken: nil, ID: grant.ID})
	requireOopsCode(t, err, oops.CodeNotFound)
	require.Len(t, listPrincipalGrants(t, ctx, ti.conn, orgID, recipient), 1, "revoking never removes a permanent grant")

	listed, err := ti.service.ListTemporaryGrants(ctx, &gen.ListTemporaryGrantsPayload{SessionToken: nil, UserID: nil, IncludeExpired: true})
	require.NoError(t, err)
	require.Empty(t, listed.Grants)
}

func TestTemporaryGrant_InsertIfAbsentTakesOverExpiredRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID
	recipient := seedTemporaryGrantRecipient(t, ctx, ti.conn, orgID)

	grant := createTemporaryGrant(t, ctx, ti, temporaryGrantRecipientID, authz.ScopeMCPConnect, nil)
	expireTemporaryGrant(t, ctx, ti.conn, grant.ID)

	selectors, err := authz.NewSelector(authz.ScopeMCPConnect, authz.WildcardResource).MarshalJSON()
	require.NoError(t, err)
	written, err := accessrepo.New(ti.conn).InsertPrincipalGrantIfAbsent(ctx, accessrepo.InsertPrincipalGrantIfAbsentParams{
		OrganizationID: orgID,
		PrincipalUrn:   recipient,
		Scope:          string(authz.ScopeMCPConnect),
		Selectors:      selectors,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, written, "an expired row does not stop a permanent default being written")
	require.Len(t, listPrincipalGrants(t, ctx, ti.conn, orgID, recipient), 1)

	written, err = accessrepo.New(ti.conn).InsertPrincipalGrantIfAbsent(ctx, accessrepo.InsertPrincipalGrantIfAbsentParams{
		OrganizationID: orgID,
		PrincipalUrn:   recipient,
		Scope:          string(authz.ScopeMCPConnect),
		Selectors:      selectors,
	})
	require.NoError(t, err)
	require.Zero(t, written, "an existing permanent row is left untouched")
}

func TestTemporaryGrant_RenewAndRevoke(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID
	recipient := seedTemporaryGrantRecipient(t, ctx, ti.conn, orgID)

	seedRole(t, ctx, ti.conn, orgID, mockRole("role_auditors", "Auditors", "auditors", "Reads transcripts"))
	rolePrincipal := seededRolePrincipal(t, ctx, ti.conn, orgID, "auditors")
	seedGrant(t, ctx, ti.conn, orgID, rolePrincipal, authz.ScopeLogRead, authz.WildcardResource)
	seedRoleAssignment(t, ctx, ti.conn, orgID, temporaryGrantRecipientID, mockMember("", "membership_investigator", "workos_investigator", "auditors"))

	grant := createTemporaryGrant(t, ctx, ti, temporaryGrantRecipientID, authz.ScopeLogRead, nil)
	require.Equal(t, []string{rolePrincipal.String()}, grant.OtherAccess, "the role still gives log access once the grant ends")

	renewBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessTemporaryGrantRenew)
	require.NoError(t, err)
	later := time.Now().Add(3 * time.Hour).UTC().Format(time.RFC3339)
	renewed, err := ti.service.RenewTemporaryGrant(ctx, &gen.RenewTemporaryGrantPayload{SessionToken: nil, ID: grant.ID, ExpiresAt: later})
	require.NoError(t, err)
	require.Equal(t, later, renewed.ExpiresAt)
	require.Greater(t, renewed.RemainingSeconds, int64(2*time.Hour/time.Second))
	renewAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessTemporaryGrantRenew)
	require.NoError(t, err)
	require.Equal(t, renewBefore+1, renewAfter)

	revokeBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessTemporaryGrantRevoke)
	require.NoError(t, err)
	revoked, err := ti.service.RevokeTemporaryGrant(ctx, &gen.RevokeTemporaryGrantPayload{SessionToken: nil, ID: grant.ID})
	require.NoError(t, err)
	require.Equal(t, "revoked", revoked.Status)
	require.Zero(t, revoked.RemainingSeconds)
	revokeAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessTemporaryGrantRevoke)
	require.NoError(t, err)
	require.Equal(t, revokeBefore+1, revokeAfter)

	require.Empty(t, listPrincipalGrants(t, ctx, ti.conn, orgID, recipient))
	allowed, err := authz.GrantsAuthorize(loadMemberGrants(t, ctx, ti.conn, orgID, temporaryGrantRecipientID), authz.Check{Scope: authz.ScopeLogRead, ResourceKind: "", ResourceID: uuid.NewString(), Dimensions: nil})
	require.NoError(t, err)
	require.True(t, allowed, "the role's grant is untouched by the revocation")

	_, err = ti.service.RenewTemporaryGrant(ctx, &gen.RenewTemporaryGrantPayload{SessionToken: nil, ID: grant.ID, ExpiresAt: later})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestTemporaryGrant_RenewExpiredConflicts(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	seedTemporaryGrantRecipient(t, ctx, ti.conn, authCtx.ActiveOrganizationID)

	grant := createTemporaryGrant(t, ctx, ti, temporaryGrantRecipientID, authz.ScopeChatRead, nil)
	expireTemporaryGrant(t, ctx, ti.conn, grant.ID)

	_, err := ti.service.RenewTemporaryGrant(ctx, &gen.RenewTemporaryGrantPayload{SessionToken: nil, ID: grant.ID, ExpiresAt: inOneHour()})
	requireOopsCode(t, err, oops.CodeConflict)
	_, err = ti.service.RevokeTemporaryGrant(ctx, &gen.RevokeTemporaryGrantPayload{SessionToken: nil, ID: grant.ID})
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestTemporaryGrant_SelfGrantIsAllowedAndAudited(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	grant := createTemporaryGrant(t, ctx, ti, authCtx.UserID, authz.ScopeChatRead, new("Verify onboarding traffic"))

	record, err := audittest.LatestAuditLogByActionAndSubject(ctx, ti.conn, audit.ActionAccessTemporaryGrantCreate, grant.ID)
	require.NoError(t, err)
	require.Equal(t, "access_temporary_grant", record.SubjectType)
	require.Equal(t, authCtx.UserID, record.ActorID)
	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, true, metadata["self_granted"])
	require.Equal(t, "Verify onboarding traffic", metadata["reason"])
	require.Equal(t, string(authz.ScopeChatRead), metadata["scope"])
	require.Equal(t, grant.ExpiresAt, metadata["expires_at"])
}

func TestTemporaryGrant_OtherMemberGrantIsNotSelfGranted(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	seedTemporaryGrantRecipient(t, ctx, ti.conn, authCtx.ActiveOrganizationID)

	grant := createTemporaryGrant(t, ctx, ti, temporaryGrantRecipientID, authz.ScopeChatRead, nil)
	record, err := audittest.LatestAuditLogByActionAndSubject(ctx, ti.conn, audit.ActionAccessTemporaryGrantCreate, grant.ID)
	require.NoError(t, err)
	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, false, metadata["self_granted"])
	require.Equal(t, "user:"+temporaryGrantRecipientID, metadata["recipient_urn"])
}

func TestTemporaryGrant_RequiresOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID
	seedTemporaryGrantRecipient(t, ctx, ti.conn, orgID)

	grant := createTemporaryGrant(t, ctx, ti, temporaryGrantRecipientID, authz.ScopeChatRead, nil)
	mine := createTemporaryGrant(t, ctx, ti, authCtx.UserID, authz.ScopeLogRead, nil)

	memberCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, orgID))

	_, err := ti.service.CreateTemporaryGrant(memberCtx, &gen.CreateTemporaryGrantPayload{SessionToken: nil, UserID: authCtx.UserID, Scope: string(authz.ScopeChatWrite), Selector: nil, ExpiresAt: inOneHour(), Reason: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.RenewTemporaryGrant(memberCtx, &gen.RenewTemporaryGrantPayload{SessionToken: nil, ID: mine.ID, ExpiresAt: inOneHour()})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.RevokeTemporaryGrant(memberCtx, &gen.RevokeTemporaryGrantPayload{SessionToken: nil, ID: grant.ID})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.RevokeTemporaryGrant(memberCtx, &gen.RevokeTemporaryGrantPayload{SessionToken: nil, ID: uuid.NewString()})
	requireOopsCode(t, err, oops.CodeForbidden)

	other := temporaryGrantRecipientID
	_, err = ti.service.ListTemporaryGrants(memberCtx, &gen.ListTemporaryGrantsPayload{SessionToken: nil, UserID: &other, IncludeExpired: false})
	requireOopsCode(t, err, oops.CodeForbidden)

	own, err := ti.service.ListTemporaryGrants(memberCtx, &gen.ListTemporaryGrantsPayload{SessionToken: nil, UserID: nil, IncludeExpired: false})
	require.NoError(t, err)
	require.Len(t, own.Grants, 1, "a member sees only their own grants")
	require.Equal(t, mine.ID, own.Grants[0].ID)

	revoked, err := ti.service.RevokeTemporaryGrant(memberCtx, &gen.RevokeTemporaryGrantPayload{SessionToken: nil, ID: mine.ID})
	require.NoError(t, err, "a member may end their own temporary grant")
	require.Equal(t, "revoked", revoked.Status)
}

func TestTemporaryGrant_TenantIsolation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID
	seedTemporaryGrantRecipient(t, ctx, ti.conn, orgID)
	grant := createTemporaryGrant(t, ctx, ti, temporaryGrantRecipientID, authz.ScopeChatRead, nil)

	otherOrgID := "org_" + uuid.NewString()
	seedOrganization(t, ctx, ti.conn, otherOrgID)
	otherAuthCtx := *authCtx
	otherAuthCtx.ActiveOrganizationID = otherOrgID
	otherCtx := contextvalues.SetAuthContext(ctx, &otherAuthCtx)
	otherCtx = authztest.WithAdminGrants(otherCtx)

	listed, err := ti.service.ListTemporaryGrants(otherCtx, &gen.ListTemporaryGrantsPayload{SessionToken: nil, UserID: nil, IncludeExpired: true})
	require.NoError(t, err)
	require.Empty(t, listed.Grants)
	_, err = ti.service.RevokeTemporaryGrant(otherCtx, &gen.RevokeTemporaryGrantPayload{SessionToken: nil, ID: grant.ID})
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = ti.service.RenewTemporaryGrant(otherCtx, &gen.RenewTemporaryGrantPayload{SessionToken: nil, ID: grant.ID, ExpiresAt: inOneHour()})
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = ti.service.CreateTemporaryGrant(otherCtx, &gen.CreateTemporaryGrantPayload{SessionToken: nil, UserID: temporaryGrantRecipientID, Scope: string(authz.ScopeChatRead), Selector: nil, ExpiresAt: inOneHour(), Reason: nil})
	requireOopsCode(t, err, oops.CodeNotFound)

	otherPrincipals := []urn.Principal{urn.NewPrincipal(urn.PrincipalTypeUser, temporaryGrantRecipientID)}
	otherGrants, err := authz.LoadGrants(ctx, ti.conn, otherOrgID, otherPrincipals)
	require.NoError(t, err)
	require.Empty(t, otherGrants, "the grant authorizes nothing in another organization")
}

func TestTemporaryGrant_Validation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	seedTemporaryGrantRecipient(t, ctx, ti.conn, authCtx.ActiveOrganizationID)

	cases := map[string]*gen.CreateTemporaryGrantPayload{
		"blocklist scope":   {UserID: temporaryGrantRecipientID, Scope: string(authz.ScopeMCPBlockedConnect), ExpiresAt: inOneHour()},
		"auth client scope": {UserID: temporaryGrantRecipientID, Scope: string(authz.ScopeMCPAuth), ExpiresAt: inOneHour()},
		"risk policy scope": {UserID: temporaryGrantRecipientID, Scope: string(authz.ScopeRiskPolicyBypass), ExpiresAt: inOneHour()},
		"root scope":        {UserID: temporaryGrantRecipientID, Scope: string(authz.ScopeRoot), ExpiresAt: inOneHour()},
		"too short":         {UserID: temporaryGrantRecipientID, Scope: string(authz.ScopeChatRead), ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339)},
		"too long":          {UserID: temporaryGrantRecipientID, Scope: string(authz.ScopeChatRead), ExpiresAt: time.Now().Add(31 * 24 * time.Hour).UTC().Format(time.RFC3339)},
		"past":              {UserID: temporaryGrantRecipientID, Scope: string(authz.ScopeChatRead), ExpiresAt: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)},
		"wrong selector":    {UserID: temporaryGrantRecipientID, Scope: string(authz.ScopeChatRead), ExpiresAt: inOneHour(), Selector: &gen.Selector{ResourceKind: "mcp", ResourceID: "*"}},
	}
	for name, payload := range cases {
		_, err := ti.service.CreateTemporaryGrant(ctx, payload)
		require.Error(t, err, name)
		requireOopsCode(t, err, oops.CodeBadRequest)
	}

	_, err := ti.service.CreateTemporaryGrant(ctx, &gen.CreateTemporaryGrantPayload{UserID: "local_not_a_member", Scope: string(authz.ScopeChatRead), ExpiresAt: inOneHour()})
	requireOopsCode(t, err, oops.CodeNotFound)

	rows, err := accessrepo.New(ti.conn).ListTemporaryGrants(ctx, accessrepo.ListTemporaryGrantsParams{OrganizationID: authCtx.ActiveOrganizationID, PrincipalUrn: "", IncludeExpired: true})
	require.NoError(t, err)
	require.Empty(t, rows, "rejected requests write nothing")
}

func TestTemporaryGrant_ListedInIdentityAccess(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID
	seedTemporaryGrantRecipient(t, ctx, ti.conn, orgID)
	server := seedRemoteMCPServer(t, ctx, ti.conn, orgID)

	grant, err := ti.service.CreateTemporaryGrant(ctx, &gen.CreateTemporaryGrantPayload{
		UserID:    temporaryGrantRecipientID,
		Scope:     string(authz.ScopeMCPConnect),
		Selector:  &gen.Selector{ResourceKind: "mcp", ResourceID: server},
		ExpiresAt: inOneHour(),
	})
	require.NoError(t, err)

	result, err := ti.service.ListIdentityAccess(ctx, &gen.ListIdentityAccessPayload{UserID: temporaryGrantRecipientID, SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, result.Servers, 1)
	require.Equal(t, server, result.Servers[0].ID)

	expireTemporaryGrant(t, ctx, ti.conn, grant.ID)
	result, err = ti.service.ListIdentityAccess(ctx, &gen.ListIdentityAccessPayload{UserID: temporaryGrantRecipientID, SessionToken: nil})
	require.NoError(t, err)
	require.Empty(t, result.Servers)
}
