package access

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	accessRequestAdminID     = "local_second_admin"
	accessRequestNonAdminID  = "local_plain_member"
	accessRequestAdminEmail  = "second-admin@example.com"
	accessRequestMemberEmail = "member@example.com"
)

// newAccessRequestService is the access service with the access requests
// rollout on, a second administrator who can decide the signed-in member's
// requests, and a member who is not an administrator.
func newAccessRequestService(t *testing.T) (context.Context, *testInstance, string) {
	t.Helper()
	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagAccessRequests, orgID, true)
	ti.service.WithFeatures(flags)

	seedConnectedUser(t, ctx, ti.conn, orgID, accessRequestAdminID, accessRequestAdminEmail, "Second Admin", "user_second_admin", "membership_second_admin")
	seedRole(t, ctx, ti.conn, orgID, mockSystemRole("role_admin", "Admin", "admin"))
	seedRoleAssignment(t, ctx, ti.conn, orgID, accessRequestAdminID, mockMember("", "membership_second_admin", "user_second_admin", "admin"))
	seedInternalGrant(t, ctx, ti.conn, orgID, urn.NewPrincipal(urn.PrincipalTypeUser, accessRequestAdminID), string(authz.ScopeOrgAdmin), orgID)
	seedConnectedUser(t, ctx, ti.conn, orgID, accessRequestNonAdminID, accessRequestMemberEmail, "Plain Member", "user_plain_member", "membership_plain_member")

	return ctx, ti, orgID
}

func requestChatRead(t *testing.T, ctx context.Context, ti *testInstance) *gen.RequestAccessResult {
	t.Helper()
	result, err := ti.service.RequestAccess(ctx, &gen.RequestAccessPayload{
		Scope:        string(authz.ScopeChatRead),
		ResourceID:   nil,
		ResourceName: nil,
		Message:      new("Investigating a reported session"),
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	return result
}

func decideAs(ctx context.Context, ti *testInstance, orgID, requestID, deciderID string, approve bool) (*gen.AccessRequest, error) {
	return ti.service.DecideAccessRequestAs(ctx, DecideAccessRequestInput{
		OrganizationID: orgID,
		RequestID:      uuid.MustParse(requestID),
		DeciderUserID:  deciderID,
		DeciderEmail:   new(accessRequestAdminEmail),
		Approve:        approve,
		GrantExpiresAt: inOneHour(),
		Via:            AccessRequestDecidedViaSlack,
	})
}

func TestAccessRequest_NotStoredWithoutRollout(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)

	result := requestChatRead(t, ctx, ti)
	require.Nil(t, result.Request, "without the rollout requestAccess only emails")

	_, err := ti.service.ListAccessRequests(ctx, &gen.ListAccessRequestsPayload{SessionToken: nil, Status: nil, Mine: false})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestAccessRequest_StoredOnceWhilePending(t *testing.T) {
	t.Parallel()
	ctx, ti, _ := newAccessRequestService(t)
	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessRequestCreate)
	require.NoError(t, err)

	first := requestChatRead(t, ctx, ti)
	require.NotNil(t, first.Request)
	require.Equal(t, AccessRequestStatusPending, first.Request.Status)
	require.Equal(t, string(authz.ScopeChatRead), first.Request.Scope)
	require.Equal(t, authz.WildcardResource, first.Request.Selector.ResourceID)
	require.Equal(t, "Investigating a reported session", *first.Request.Message)
	require.Equal(t, 1, first.SentToCount, "the second administrator is emailed")
	require.Contains(t, ti.emailSender.Sent()[0].DataVariables["manage_access_link"], "request="+first.Request.ID)

	second := requestChatRead(t, ctx, ti)
	require.Equal(t, first.Request.ID, second.Request.ID, "asking again returns the pending request")
	require.Zero(t, second.SentToCount, "nobody is notified twice")
	require.Len(t, ti.emailSender.Sent(), 1)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessRequestCreate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

func TestAccessRequest_RefusesScopesThatCannotBeGivenTemporarily(t *testing.T) {
	t.Parallel()
	ctx, ti, _ := newAccessRequestService(t)

	_, err := ti.service.RequestAccess(ctx, &gen.RequestAccessPayload{
		Scope:        string(authz.ScopeOrgAdmin),
		ResourceID:   nil,
		ResourceName: nil,
		Message:      nil,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.Empty(t, ti.emailSender.Sent())
}

func TestAccessRequest_ApproveGivesTemporaryGrant(t *testing.T) {
	t.Parallel()
	ctx, ti, orgID := newAccessRequestService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	request := requestChatRead(t, ctx, ti).Request
	approvals, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessRequestApprove)
	require.NoError(t, err)
	grants, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantCreateTemporary)
	require.NoError(t, err)

	decided, err := decideAs(ctx, ti, orgID, request.ID, accessRequestAdminID, true)
	require.NoError(t, err)
	require.Equal(t, AccessRequestStatusApproved, decided.Status)
	require.Equal(t, string(AccessRequestDecidedViaSlack), *decided.DecidedVia)
	require.Equal(t, accessRequestAdminID, *decided.DecidedByUserID)
	require.Equal(t, "Second Admin", *decided.DecidedByName)
	require.NotNil(t, decided.TemporaryGrantID)
	require.NotNil(t, decided.GrantExpiresAt)

	held, err := ti.service.ListTemporaryGrants(ctx, &gen.ListTemporaryGrantsPayload{SessionToken: nil, PrincipalUrn: userPrincipalURN(authCtx.UserID), IncludeExpired: false})
	require.NoError(t, err)
	require.Len(t, held.Grants, 1)
	require.Equal(t, *decided.TemporaryGrantID, held.Grants[0].ID)
	require.Equal(t, string(authz.ScopeChatRead), held.Grants[0].Scope)

	afterApprovals, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessRequestApprove)
	require.NoError(t, err)
	require.Equal(t, approvals+1, afterApprovals)
	afterGrants, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantCreateTemporary)
	require.NoError(t, err)
	require.Equal(t, grants+1, afterGrants, "the grant is audited like any temporary grant")

	_, err = decideAs(ctx, ti, orgID, request.ID, accessRequestAdminID, false)
	require.ErrorIs(t, err, ErrAccessRequestNotPending, "a decided request cannot be decided again")

	again := requestChatRead(t, ctx, ti)
	require.NotEqual(t, request.ID, again.Request.ID, "a decided request does not block a new one")
}

func TestAccessRequest_DenyGivesNoAccess(t *testing.T) {
	t.Parallel()
	ctx, ti, orgID := newAccessRequestService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	request := requestChatRead(t, ctx, ti).Request

	decided, err := decideAs(ctx, ti, orgID, request.ID, accessRequestAdminID, false)
	require.NoError(t, err)
	require.Equal(t, AccessRequestStatusDenied, decided.Status)
	require.Nil(t, decided.TemporaryGrantID)

	held, err := ti.service.ListTemporaryGrants(ctx, &gen.ListTemporaryGrantsPayload{SessionToken: nil, PrincipalUrn: userPrincipalURN(authCtx.UserID), IncludeExpired: true})
	require.NoError(t, err)
	require.Empty(t, held.Grants)
}

func TestAccessRequest_DeciderRules(t *testing.T) {
	t.Parallel()
	ctx, ti, orgID := newAccessRequestService(t)
	request := requestChatRead(t, ctx, ti).Request

	_, err := ti.service.DecideAccessRequest(ctx, &gen.DecideAccessRequestPayload{SessionToken: nil, ID: request.ID, Approve: true, ExpiresAt: new(inOneHour())})
	requireOopsCode(t, err, oops.CodeForbidden)

	_, err = decideAs(ctx, ti, orgID, request.ID, accessRequestNonAdminID, true)
	require.ErrorIs(t, err, ErrAccessRequestNotAdmin)

	_, err = decideAs(ctx, ti, orgID, uuid.NewString(), accessRequestAdminID, true)
	require.ErrorIs(t, err, ErrAccessRequestNotFound)

	memberCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, orgID))
	_, err = ti.service.DecideAccessRequest(memberCtx, &gen.DecideAccessRequestPayload{SessionToken: nil, ID: request.ID, Approve: false, ExpiresAt: nil})
	requireOopsCode(t, err, oops.CodeForbidden)

	_, err = ti.service.DecideAccessRequest(ctx, &gen.DecideAccessRequestPayload{SessionToken: nil, ID: request.ID, Approve: true, ExpiresAt: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestAccessRequest_ApprovalKeepsTemporaryGrantLimits(t *testing.T) {
	t.Parallel()
	ctx, ti, orgID := newAccessRequestService(t)
	request := requestChatRead(t, ctx, ti).Request

	_, err := ti.service.DecideAccessRequestAs(ctx, DecideAccessRequestInput{
		OrganizationID: orgID,
		RequestID:      uuid.MustParse(request.ID),
		DeciderUserID:  accessRequestAdminID,
		DeciderEmail:   nil,
		Approve:        true,
		GrantExpiresAt: time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339),
		Via:            AccessRequestDecidedViaDashboard,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)

	current, err := ti.service.GetAccessRequest(ctx, orgID, uuid.MustParse(request.ID))
	require.NoError(t, err)
	require.Equal(t, AccessRequestStatusPending, current.Status, "a refused approval leaves the request pending")
}

func TestAccessRequest_Withdraw(t *testing.T) {
	t.Parallel()
	ctx, ti, orgID := newAccessRequestService(t)
	request := requestChatRead(t, ctx, ti).Request

	withdrawn, err := ti.service.WithdrawAccessRequest(ctx, &gen.WithdrawAccessRequestPayload{SessionToken: nil, ID: request.ID})
	require.NoError(t, err)
	require.Equal(t, AccessRequestStatusWithdrawn, withdrawn.Status)

	_, err = ti.service.WithdrawAccessRequest(ctx, &gen.WithdrawAccessRequestPayload{SessionToken: nil, ID: request.ID})
	requireOopsCode(t, err, oops.CodeConflict)

	_, err = decideAs(ctx, ti, orgID, request.ID, accessRequestAdminID, true)
	require.ErrorIs(t, err, ErrAccessRequestNotPending)
}

func TestAccessRequest_List(t *testing.T) {
	t.Parallel()
	ctx, ti, orgID := newAccessRequestService(t)
	request := requestChatRead(t, ctx, ti).Request

	all, err := ti.service.ListAccessRequests(ctx, &gen.ListAccessRequestsPayload{SessionToken: nil, Status: new(AccessRequestStatusPending), Mine: false})
	require.NoError(t, err)
	require.Len(t, all.Requests, 1)
	require.Equal(t, request.ID, all.Requests[0].ID)

	none, err := ti.service.ListAccessRequests(ctx, &gen.ListAccessRequestsPayload{SessionToken: nil, Status: new(AccessRequestStatusApproved), Mine: false})
	require.NoError(t, err)
	require.Empty(t, none.Requests)

	// A member who is not an administrator only sees their own requests, not
	// another person's.
	otherSelector, err := json.Marshal(authz.NewSelector(authz.ScopeChatRead, authz.WildcardResource))
	require.NoError(t, err)
	_, err = repo.New(ti.conn).InsertAccessRequest(ctx, repo.InsertAccessRequestParams{
		OrganizationID:  orgID,
		RequesterUserID: accessRequestAdminID,
		Scope:           string(authz.ScopeChatRead),
		Selector:        otherSelector,
		ResourceName:    pgtype.Text{String: "", Valid: false},
		Message:         pgtype.Text{String: "", Valid: false},
		ChallengeIds:    []string{},
		ExpiresAt:       pgtype.Timestamptz{Time: time.Now().Add(time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
	})
	require.NoError(t, err)
	memberCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, orgID))
	own, err := ti.service.ListAccessRequests(memberCtx, &gen.ListAccessRequestsPayload{SessionToken: nil, Status: nil, Mine: false})
	require.NoError(t, err)
	require.Len(t, own.Requests, 1)
	require.Equal(t, request.ID, own.Requests[0].ID)

	_, err = ti.service.WithdrawAccessRequest(ctx, &gen.WithdrawAccessRequestPayload{SessionToken: nil, ID: request.ID})
	require.NoError(t, err)
	withdrawn, err := ti.service.ListAccessRequests(ctx, &gen.ListAccessRequestsPayload{SessionToken: nil, Status: new(AccessRequestStatusWithdrawn), Mine: true})
	require.NoError(t, err)
	require.Len(t, withdrawn.Requests, 1)
	require.Equal(t, request.ID, withdrawn.Requests[0].ID)
}
