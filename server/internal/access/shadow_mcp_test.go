package access

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestService_CreateShadowMCPApprovalRequest_Idempotent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	project := createShadowMCPProject(t, ctx, ti, authCtx.ActiveOrganizationID)
	fullURL := "https://linear.example.com/mcp"
	host := "linear.example.com"
	serverIdentity := "linear"
	toolName := "create_issue"

	first, err := ti.service.CreateShadowMCPApprovalRequest(ctx, &gen.CreateShadowMCPApprovalRequestPayload{
		RequestToken: shadowMCPRequestToken(t, authCtx.ActiveOrganizationID, authCtx.UserID, project.ID.String(), ShadowMCPApprovalRequestTokenInput{
			ObservedName:           conv.PtrEmpty("Linear"),
			ObservedFullURL:        &fullURL,
			ObservedURLHost:        &host,
			ObservedServerIdentity: &serverIdentity,
			ToolName:               &toolName,
		}),
	})
	require.NoError(t, err)

	second, err := ti.service.CreateShadowMCPApprovalRequest(ctx, &gen.CreateShadowMCPApprovalRequestPayload{
		RequestToken: shadowMCPRequestToken(t, authCtx.ActiveOrganizationID, authCtx.UserID, project.ID.String(), ShadowMCPApprovalRequestTokenInput{
			ObservedName:           conv.PtrEmpty("Linear"),
			ObservedFullURL:        &fullURL,
			ObservedURLHost:        &host,
			ObservedServerIdentity: &serverIdentity,
			ToolName:               &toolName,
		}),
	})
	require.NoError(t, err)

	require.Equal(t, first.ID, second.ID)
	require.Equal(t, shadowMCPRequestStatusRequested, first.Status)
	require.Equal(t, project.ID.String(), first.ProjectID)
	require.Equal(t, fullURL, *first.ObservedFullURL)
	require.Equal(t, 2, second.BlockedCount)
}

func TestService_CreateShadowMCPApprovalRequest_RejectsInvalidToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	_, err := ti.service.CreateShadowMCPApprovalRequest(ctx, &gen.CreateShadowMCPApprovalRequestPayload{
		RequestToken: "not-a-shadow-mcp-token",
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestService_ApproveShadowMCPApprovalRequest_CreatesAllowRuleAndRoleGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	project := createShadowMCPProject(t, ctx, ti, authCtx.ActiveOrganizationID)
	fullURL := "https://github.example.com/org/repo/mcp"
	host := "github.example.com"

	request, err := ti.service.CreateShadowMCPApprovalRequest(ctx, &gen.CreateShadowMCPApprovalRequestPayload{
		RequestToken: shadowMCPRequestToken(t, authCtx.ActiveOrganizationID, authCtx.UserID, project.ID.String(), ShadowMCPApprovalRequestTokenInput{
			ObservedName:    conv.PtrEmpty("GitHub"),
			ObservedFullURL: &fullURL,
			ObservedURLHost: &host,
		}),
	})
	require.NoError(t, err)

	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})
	ti.roles.On("ListRoles", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Role{
		mockRole("role_ai", "AI Platform", "ai-platform", ""),
	}, nil).Once()

	result, err := ti.service.ApproveShadowMCPApprovalRequest(ctx, &gen.ApproveShadowMCPApprovalRequestPayload{
		ID:           request.ID,
		MatchBreadth: "full_url",
		MatchValue:   fullURL,
		DisplayName:  "GitHub Shadow MCP",
		RoleIds:      []string{"role_ai"},
		Reason:       conv.PtrEmpty("Approved for AI platform operators"),
	})
	require.NoError(t, err)

	require.Equal(t, shadowMCPRequestStatusApproved, result.Request.Status)
	require.Equal(t, shadowMCPRuleAllowed, result.Rule.Disposition)
	require.Equal(t, "full_url", result.Rule.MatchBreadth)
	require.Equal(t, fullURL, result.Rule.MatchValue)
	require.Equal(t, []string{"role_ai"}, result.Rule.RoleIds)
	require.Equal(t, request.ID, *result.Rule.SourceRequestID)

	grants := listPrincipalGrants(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "ai-platform"))
	require.Len(t, grants, 1)
	require.Equal(t, string(authz.ScopeShadowMCPConnect), grants[0].Scope)

	selector, err := authz.SelectorFromRow(grants[0].Selectors)
	require.NoError(t, err)
	require.Equal(t, "shadow_mcp", selector["resource_kind"])
	require.Equal(t, result.Rule.ID, selector.ResourceID())
}

func TestService_ApproveShadowMCPApprovalRequest_MergesExistingRuleRoleGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	project := createShadowMCPProject(t, ctx, ti, authCtx.ActiveOrganizationID)
	fullURL := "https://github.example.com/org/repo/mcp"
	roles := []thirdpartyworkos.Role{
		mockRole("role_ops", "Operations", "operations", ""),
		mockRole("role_ai", "AI Platform", "ai-platform", ""),
	}

	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})
	ti.roles.On("ListRoles", mock.Anything, mockidp.MockOrgID).Return(roles, nil).Once()
	existingRule, err := ti.service.CreateShadowMCPAccessRule(ctx, &gen.CreateShadowMCPAccessRulePayload{
		Disposition:  shadowMCPRuleAllowed,
		MatchBreadth: "full_url",
		MatchValue:   fullURL,
		DisplayName:  "GitHub Shadow MCP",
		RoleIds:      []string{"role_ops"},
	})
	require.NoError(t, err)

	request, err := ti.service.CreateShadowMCPApprovalRequest(ctx, &gen.CreateShadowMCPApprovalRequestPayload{
		RequestToken: shadowMCPRequestToken(t, authCtx.ActiveOrganizationID, authCtx.UserID, project.ID.String(), ShadowMCPApprovalRequestTokenInput{
			ObservedName:    conv.PtrEmpty("GitHub"),
			ObservedFullURL: &fullURL,
		}),
	})
	require.NoError(t, err)

	ti.roles.On("ListRoles", mock.Anything, mockidp.MockOrgID).Return(roles, nil).Once()
	result, err := ti.service.ApproveShadowMCPApprovalRequest(ctx, &gen.ApproveShadowMCPApprovalRequestPayload{
		ID:           request.ID,
		MatchBreadth: "full_url",
		MatchValue:   fullURL,
		DisplayName:  "GitHub Shadow MCP",
		RoleIds:      []string{"role_ai"},
	})
	require.NoError(t, err)

	require.Equal(t, existingRule.ID, result.Rule.ID)
	require.ElementsMatch(t, []string{"role_ops", "role_ai"}, result.Rule.RoleIds)
	require.Len(t, listPrincipalGrants(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "operations")), 1)
	require.Len(t, listPrincipalGrants(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "ai-platform")), 1)
}

func TestService_DenyShadowMCPApprovalRequest_CanSkipDenyRule(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	project := createShadowMCPProject(t, ctx, ti, authCtx.ActiveOrganizationID)
	host := "unknown.example.com"

	request, err := ti.service.CreateShadowMCPApprovalRequest(ctx, &gen.CreateShadowMCPApprovalRequestPayload{
		RequestToken: shadowMCPRequestToken(t, authCtx.ActiveOrganizationID, authCtx.UserID, project.ID.String(), ShadowMCPApprovalRequestTokenInput{
			ObservedName:    conv.PtrEmpty("Unknown MCP"),
			ObservedURLHost: &host,
		}),
	})
	require.NoError(t, err)

	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})
	result, err := ti.service.DenyShadowMCPApprovalRequest(ctx, &gen.DenyShadowMCPApprovalRequestPayload{
		ID:             request.ID,
		CreateDenyRule: false,
		Reason:         conv.PtrEmpty("Not enough context"),
	})
	require.NoError(t, err)

	require.Equal(t, shadowMCPRequestStatusDenied, result.Request.Status)
	require.Nil(t, result.Rule)
	require.Equal(t, "Not enough context", *result.Request.DecisionNote)
}

func TestService_ListShadowMCPApprovalRequests_RequiresOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgRead, Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID)})

	_, err := ti.service.ListShadowMCPApprovalRequests(ctx, &gen.ListShadowMCPApprovalRequestsPayload{Limit: 10})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestService_ShadowMCPAccessRule_ManualLifecycle(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
		authz.Grant{Scope: authz.ScopeOrgRead, Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID)},
	)
	roles := []thirdpartyworkos.Role{
		mockRole("role_ops", "Operations", "operations", ""),
		mockRole("role_security", "Security", "security", ""),
	}
	ti.roles.On("ListRoles", mock.Anything, mockidp.MockOrgID).Return(roles, nil).Once()

	rule, err := ti.service.CreateShadowMCPAccessRule(ctx, &gen.CreateShadowMCPAccessRulePayload{
		Disposition:  shadowMCPRuleAllowed,
		MatchBreadth: "url_host",
		MatchValue:   "notion.example.com",
		DisplayName:  "Notion Shadow MCP",
		RoleIds:      []string{"role_ops"},
		Reason:       conv.PtrEmpty("Manual approval"),
	})
	require.NoError(t, err)
	require.Equal(t, shadowMCPRuleAllowed, rule.Disposition)
	require.Equal(t, []string{"role_ops"}, rule.RoleIds)

	ti.roles.On("ListRoles", mock.Anything, mockidp.MockOrgID).Return(roles, nil).Once()
	updated, err := ti.service.UpdateShadowMCPAccessRule(ctx, &gen.UpdateShadowMCPAccessRulePayload{
		ID:           rule.ID,
		Disposition:  shadowMCPRuleDenied,
		MatchBreadth: "url_host",
		MatchValue:   "notion.example.com",
		DisplayName:  "Notion Shadow MCP",
		RoleIds:      []string{"role_security"},
		Reason:       conv.PtrEmpty("Denied after review"),
	})
	require.NoError(t, err)
	require.Equal(t, shadowMCPRuleDenied, updated.Disposition)
	require.Empty(t, updated.RoleIds)

	grants := listPrincipalGrants(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "operations"))
	require.Empty(t, grants)

	err = ti.service.DeleteShadowMCPAccessRule(ctx, &gen.DeleteShadowMCPAccessRulePayload{ID: rule.ID})
	require.NoError(t, err)

	result, err := ti.service.ListShadowMCPAccessRules(ctx, &gen.ListShadowMCPAccessRulesPayload{Limit: 10})
	require.NoError(t, err)
	require.Zero(t, result.Total)
	require.Empty(t, result.Rules)
}

func TestService_CreateShadowMCPAccessRule_NormalizesMatchValue(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})
	ti.roles.On("ListRoles", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Role{}, nil).Once()

	rule, err := ti.service.CreateShadowMCPAccessRule(ctx, &gen.CreateShadowMCPAccessRulePayload{
		Disposition:  shadowMCPRuleDenied,
		MatchBreadth: "url_host",
		MatchValue:   "HTTPS://Example.COM:443/path",
		DisplayName:  "Example Shadow MCP",
	})
	require.NoError(t, err)
	require.Equal(t, "example.com", rule.MatchValue)

	ti.roles.On("ListRoles", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Role{}, nil).Once()
	_, err = ti.service.CreateShadowMCPAccessRule(ctx, &gen.CreateShadowMCPAccessRulePayload{
		Disposition:  shadowMCPRuleDenied,
		MatchBreadth: "url_host",
		MatchValue:   "example.com",
		DisplayName:  "Duplicate Example Shadow MCP",
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeConflict, oopsErr.Code)
}

func TestService_CreateShadowMCPAccessRule_AuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionShadowMCPAccessRuleCreate)
	require.NoError(t, err)

	ti.roles.On("ListRoles", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Role{}, nil).Once()
	rule, err := ti.service.CreateShadowMCPAccessRule(ctx, &gen.CreateShadowMCPAccessRulePayload{
		Disposition:  shadowMCPRuleDenied,
		MatchBreadth: "url_host",
		MatchValue:   "audit.example.com",
		DisplayName:  "Audit Shadow MCP",
		Reason:       conv.PtrEmpty("Audit coverage"),
	})
	require.NoError(t, err)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionShadowMCPAccessRuleCreate)
	require.NoError(t, err)
	require.Equal(t, string(audit.ActionShadowMCPAccessRuleCreate), record.Action)
	require.Equal(t, "shadow_mcp_access_rule", record.SubjectType)
	require.Equal(t, "Audit Shadow MCP", record.SubjectDisplay)
	require.Equal(t, rule.MatchValue, record.SubjectSlug)
	require.Nil(t, record.BeforeSnapshot)
	require.NotNil(t, record.AfterSnapshot)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionShadowMCPAccessRuleCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestService_CreateShadowMCPAccessRule_ForbiddenWithoutOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx = withRBACGrants(t, ctx)

	_, err := ti.service.CreateShadowMCPAccessRule(ctx, &gen.CreateShadowMCPAccessRulePayload{
		Disposition:  shadowMCPRuleAllowed,
		MatchBreadth: "full_url",
		MatchValue:   "https://blocked.example.com/mcp",
		DisplayName:  "Blocked",
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func createShadowMCPProject(t *testing.T, ctx context.Context, ti *testInstance, organizationID string) projectsrepo.Project {
	t.Helper()

	projectSlug := uuid.NewString()
	project, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           projectSlug,
		Slug:           projectSlug,
		OrganizationID: organizationID,
	})
	require.NoError(t, err)

	return project
}

func shadowMCPRequestToken(t *testing.T, organizationID string, requesterUserID string, projectID string, input ShadowMCPApprovalRequestTokenInput) string {
	t.Helper()

	input.OrganizationID = organizationID
	input.ProjectID = projectID
	input.RequesterUserID = requesterUserID
	token, _, err := GenerateShadowMCPApprovalRequestToken("test-jwt-secret", input, 5*time.Minute)
	require.NoError(t, err)
	return token
}
