package access

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// The point of the endpoint is to answer "what may this other person do",
// so the grants returned must be the subject's, direct and role-inherited,
// not the caller's.
func TestService_ListMemberGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	const subjectUserID = "user_subject"

	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, subjectUserID, "subject@example.com", "Subject User", "workos_user_subject", "membership_subject")
	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_custom", "Custom Builder", "custom-builder", ""))
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, subjectUserID, mockMember("", "membership_subject", "workos_user_subject", "custom-builder"))
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, subjectUserID), authz.ScopeProjectRead, "project_123")
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, seededRolePrincipal(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "custom-builder"), authz.ScopeMCPConnect, "tool_456")

	result, err := ti.service.ListMemberGrants(ctx, &gen.ListMemberGrantsPayload{UserID: subjectUserID, ApikeyToken: nil, SessionToken: nil})
	require.NoError(t, err)

	byScope := make(map[string]*gen.ListRoleGrant, len(result.Grants))
	for _, grant := range result.Grants {
		byScope[grant.Scope] = grant
	}
	require.Len(t, byScope["project:read"].Selectors, 1)
	require.Equal(t, "project_123", byScope["project:read"].Selectors[0].ResourceID)
	require.Len(t, byScope["mcp:connect"].Selectors, 1)
	require.Equal(t, "tool_456", byScope["mcp:connect"].Selectors[0].ResourceID)
}

func TestService_ListMemberGrants_RequiresOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	const subjectUserID = "user_subject"
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, subjectUserID, "subject@example.com", "Subject User", "workos_user_subject", "membership_subject")

	readOnly := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authCtx.ActiveOrganizationID))

	_, err := ti.service.ListMemberGrants(readOnly, &gen.ListMemberGrantsPayload{UserID: subjectUserID, ApikeyToken: nil, SessionToken: nil})
	require.Error(t, err)

	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeForbidden, shareable.Code)
}

// An id belonging to nobody is not an error: principal resolution still
// produces the org-wide subject set, so the answer is the grants that would
// apply to anyone. It just never includes grants held by a real member.
func TestService_ListMemberGrants_UnknownMemberSeesOnlyOrgWideGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	const subjectUserID = "user_subject"
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, subjectUserID, "subject@example.com", "Subject User", "workos_user_subject", "membership_subject")
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, subjectUserID), authz.ScopeProjectRead, "project_123")
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authz.AllUsersPrincipal(), authz.ScopeRiskPolicyEvaluate, "policy-for-everyone")

	result, err := ti.service.ListMemberGrants(ctx, &gen.ListMemberGrantsPayload{UserID: "user_nobody", ApikeyToken: nil, SessionToken: nil})
	require.NoError(t, err)

	scopes := make([]string, 0, len(result.Grants))
	for _, grant := range result.Grants {
		scopes = append(scopes, grant.Scope)
	}
	require.Equal(t, []string{string(authz.ScopeRiskPolicyEvaluate)}, scopes)
}
