package policyaccess

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/policyaccess/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestPolicyAccessRequestE2E(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPolicyAccess(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	requesterEmail := conv.PtrValOrEmpty(authCtx.Email, "")

	policyID := uuid.NewString()
	serverURL := "mcp.example.com"

	req, err := RecordRequest(ctx, ti.conn, RecordRequestParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		ProjectID:       authCtx.ProjectID.String(),
		PolicyID:        policyID,
		Target:          ShadowMCPServerTarget(serverURL),
		RequesterUserID: authCtx.UserID,
		RequesterEmail:  requesterEmail,
		Note:            "first block",
	})
	require.NoError(t, err)

	req2, err := RecordRequest(ctx, ti.conn, RecordRequestParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		ProjectID:       authCtx.ProjectID.String(),
		PolicyID:        policyID,
		Target:          ShadowMCPServerTarget(serverURL),
		RequesterUserID: authCtx.UserID,
		RequesterEmail:  requesterEmail,
		Note:            "second block",
	})
	require.NoError(t, err)
	require.Equal(t, req.ID, req2.ID)
	require.Equal(t, "second block", req2.Note)

	role := seedPolicyAccessRole(t, ctx, ti, authCtx.ActiveOrganizationID, "org-risk-reviewers")

	decided, err := DecideRequest(ctx, ti.conn, Decision{
		OrganizationID: authCtx.ActiveOrganizationID,
		RequestID:      req.ID.String(),
		Status:         "approved",
		GrantType:      GrantTypeRoles,
		RoleSlugs:      []string{role.WorkosSlug},
		DecidedBy:      "user:" + authCtx.UserID,
	})
	require.NoError(t, err)
	require.Equal(t, "approved", decided.Status)
	require.Equal(t, []string{role.RoleUrn}, decided.GrantedPrincipalUrns)

	_, err = DecideRequest(ctx, ti.conn, Decision{
		OrganizationID: authCtx.ActiveOrganizationID,
		RequestID:      req.ID.String(),
		Status:         "denied",
		DecidedBy:      "user:" + authCtx.UserID,
	})
	require.ErrorIs(t, err, ErrRequestNotFound)

	grants, err := authz.ListGrantsForResource(ctx, ti.conn, authCtx.ActiveOrganizationID, authz.ScopeRiskPolicyBypass, policyID)
	require.NoError(t, err)
	require.Len(t, grants, 1)
	require.Equal(t, role.RoleUrn, grants[0].PrincipalUrn)
	require.Equal(t, authz.ScopeRiskPolicyBypass, grants[0].Scope)
	require.Equal(t, serverURL, grants[0].Selector[authz.SelectorKeyServerURL])

	rolePrincipal, err := urn.ParsePrincipal(role.RoleUrn)
	require.NoError(t, err)
	check := authz.Selector{
		authz.SelectorKeyResourceKind: authz.ResourceKindRiskPolicy,
		authz.SelectorKeyResourceID:   policyID,
		authz.SelectorKeyServerURL:    serverURL,
	}
	require.True(t, grants[0].Selector.Matches(check))
	require.Equal(t, role.RoleUrn, rolePrincipal.String())

	bypasses, err := repo.New(ti.conn).ListPolicyBypasses(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.Len(t, bypasses, 1)
	require.Equal(t, role.RoleUrn, bypasses[0].PrincipalUrn.String())

	_, err = repo.New(ti.conn).DeletePolicyBypass(ctx, repo.DeletePolicyBypassParams{
		GrantID:        bypasses[0].ID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)

	bypasses, err = repo.New(ti.conn).ListPolicyBypasses(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.Empty(t, bypasses)

	refreshed, err := RecordRequest(ctx, ti.conn, RecordRequestParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		ProjectID:       authCtx.ProjectID.String(),
		PolicyID:        policyID,
		Target:          ShadowMCPServerTarget(serverURL),
		RequesterUserID: authCtx.UserID,
		RequesterEmail:  requesterEmail,
		Note:            "blocked again",
	})
	require.NoError(t, err)
	require.Equal(t, req.ID, refreshed.ID)
	require.Equal(t, "requested", refreshed.Status)
	require.Empty(t, refreshed.GrantedPrincipalUrns)
	require.False(t, refreshed.DecidedAt.Valid)
	require.False(t, refreshed.Deleted)
}

func TestRevokeBypassSoftDeletesApprovedRequest(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPolicyAccess(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	policyID := uuid.NewString()
	req, err := RecordRequest(ctx, ti.conn, RecordRequestParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		ProjectID:       authCtx.ProjectID.String(),
		PolicyID:        policyID,
		Target:          WholePolicyTarget(),
		RequesterUserID: authCtx.UserID,
		RequesterEmail:  conv.PtrValOrEmpty(authCtx.Email, ""),
		Note:            "",
	})
	require.NoError(t, err)

	decided, err := DecideRequest(ctx, ti.conn, Decision{
		OrganizationID: authCtx.ActiveOrganizationID,
		RequestID:      req.ID.String(),
		Status:         "approved",
		GrantType:      GrantTypeRequester,
		DecidedBy:      "user:" + authCtx.UserID,
	})
	require.NoError(t, err)
	require.Equal(t, "approved", decided.Status)

	service := NewService(ti.logger, ti.tracerProvider, ti.conn, ti.sessions, ti.authz)
	bypasses, err := repo.New(ti.conn).ListPolicyBypasses(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.Len(t, bypasses, 1)
	require.NoError(t, service.RevokeBypass(ctx, &gen.RevokeBypassPayload{GrantID: bypasses[0].ID.String()}))

	_, err = repo.New(ti.conn).GetPolicyAccessRequest(ctx, repo.GetPolicyAccessRequestParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ID:             req.ID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)

	raw, err := repo.New(ti.conn).GetRequestedPolicyAccessRequestForUpdate(ctx, repo.GetRequestedPolicyAccessRequestForUpdateParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ID:             req.ID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.Empty(t, raw.ID)

	revived, err := RecordRequest(ctx, ti.conn, RecordRequestParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		ProjectID:       authCtx.ProjectID.String(),
		PolicyID:        policyID,
		Target:          WholePolicyTarget(),
		RequesterUserID: authCtx.UserID,
		RequesterEmail:  conv.PtrValOrEmpty(authCtx.Email, ""),
		Note:            "re-request",
	})
	require.NoError(t, err)
	require.Equal(t, req.ID, revived.ID)
	require.Equal(t, "requested", revived.Status)
	require.False(t, revived.Deleted)
	require.False(t, revived.DeletedAt.Valid)
}

func TestRevokeBypassKeepsApprovedRequestWhenOtherPrincipalsRemain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPolicyAccess(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	policyID := uuid.NewString()
	req, err := RecordRequest(ctx, ti.conn, RecordRequestParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		ProjectID:       authCtx.ProjectID.String(),
		PolicyID:        policyID,
		Target:          WholePolicyTarget(),
		RequesterUserID: authCtx.UserID,
		RequesterEmail:  conv.PtrValOrEmpty(authCtx.Email, ""),
		Note:            "",
	})
	require.NoError(t, err)

	roleA := seedPolicyAccessRole(t, ctx, ti, authCtx.ActiveOrganizationID, "org-risk-reviewers-a")
	roleB := seedPolicyAccessRole(t, ctx, ti, authCtx.ActiveOrganizationID, "org-risk-reviewers-b")
	decided, err := DecideRequest(ctx, ti.conn, Decision{
		OrganizationID: authCtx.ActiveOrganizationID,
		RequestID:      req.ID.String(),
		Status:         "approved",
		GrantType:      GrantTypeRoles,
		RoleSlugs:      []string{roleA.WorkosSlug, roleB.WorkosSlug},
		DecidedBy:      "user:" + authCtx.UserID,
	})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{roleA.RoleUrn, roleB.RoleUrn}, decided.GrantedPrincipalUrns)

	service := NewService(ti.logger, ti.tracerProvider, ti.conn, ti.sessions, ti.authz)
	bypasses, err := repo.New(ti.conn).ListPolicyBypasses(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.Len(t, bypasses, 2)

	var revokeID uuid.UUID
	var remainingPrincipal string
	for _, bypass := range bypasses {
		if bypass.PrincipalUrn.String() == roleA.RoleUrn {
			revokeID = bypass.ID
			remainingPrincipal = roleB.RoleUrn
		}
	}
	require.NotEqual(t, uuid.Nil, revokeID)
	require.NoError(t, service.RevokeBypass(ctx, &gen.RevokeBypassPayload{GrantID: revokeID.String()}))

	stored, err := repo.New(ti.conn).GetPolicyAccessRequest(ctx, repo.GetPolicyAccessRequestParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ID:             req.ID,
	})
	require.NoError(t, err)
	require.Equal(t, "approved", stored.Status)
	require.False(t, stored.Deleted)
	require.Equal(t, []string{remainingPrincipal}, stored.GrantedPrincipalUrns)
}

func TestDecideRequestRequiresRecipientOnApprove(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPolicyAccess(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	requesterEmail := conv.PtrValOrEmpty(authCtx.Email, "")

	req, err := RecordRequest(ctx, ti.conn, RecordRequestParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		ProjectID:       authCtx.ProjectID.String(),
		PolicyID:        uuid.NewString(),
		Target:          WholePolicyTarget(),
		RequesterUserID: authCtx.UserID,
		RequesterEmail:  requesterEmail,
		Note:            "",
	})
	require.NoError(t, err)

	_, err = DecideRequest(ctx, ti.conn, Decision{
		OrganizationID: authCtx.ActiveOrganizationID,
		RequestID:      req.ID.String(),
		Status:         "approved",
		GrantType:      GrantTypeRoles,
		DecidedBy:      "user:" + authCtx.UserID,
	})
	require.ErrorIs(t, err, ErrGrantRecipientRequired)
}

func TestDecideRequestCanGrantRequester(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPolicyAccess(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	policyID := uuid.NewString()
	req, err := RecordRequest(ctx, ti.conn, RecordRequestParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		ProjectID:       authCtx.ProjectID.String(),
		PolicyID:        policyID,
		Target:          WholePolicyTarget(),
		RequesterUserID: authCtx.UserID,
		RequesterEmail:  conv.PtrValOrEmpty(authCtx.Email, ""),
		Note:            "",
	})
	require.NoError(t, err)

	decided, err := DecideRequest(ctx, ti.conn, Decision{
		OrganizationID: authCtx.ActiveOrganizationID,
		RequestID:      req.ID.String(),
		Status:         "approved",
		GrantType:      GrantTypeRequester,
		DecidedBy:      "user:" + authCtx.UserID,
	})
	require.NoError(t, err)
	require.Equal(t, []string{urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID).String()}, decided.GrantedPrincipalUrns)

	grants, err := authz.ListGrantsForResource(ctx, ti.conn, authCtx.ActiveOrganizationID, authz.ScopeRiskPolicyBypass, policyID)
	require.NoError(t, err)
	require.Len(t, grants, 1)
	require.Equal(t, urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID).String(), grants[0].PrincipalUrn)
	require.NotContains(t, grants[0].Selector, authz.SelectorKeyServerURL)
}

func TestDecideRequestRejectsInvalidID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPolicyAccess(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	_, err := DecideRequest(ctx, ti.conn, Decision{
		OrganizationID: authCtx.ActiveOrganizationID,
		RequestID:      "not-a-uuid",
		Status:         "denied",
		DecidedBy:      "user:" + authCtx.UserID,
	})
	require.ErrorIs(t, err, ErrInvalidRequestID)
}

func seedPolicyAccessRole(t *testing.T, ctx context.Context, ti *testInstance, organizationID string, slug string) accessrepo.UpsertOrganizationRoleRow {
	t.Helper()

	now := pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	role, err := accessrepo.New(ti.conn).UpsertOrganizationRole(ctx, accessrepo.UpsertOrganizationRoleParams{
		OrganizationID:    organizationID,
		WorkosSlug:        slug,
		WorkosName:        "Risk Reviewers",
		WorkosDescription: pgtype.Text{String: "Risk policy reviewers", Valid: true},
		WorkosCreatedAt:   now,
		WorkosUpdatedAt:   now,
		WorkosLastEventID: pgtype.Text{},
	})
	require.NoError(t, err)
	return role
}
