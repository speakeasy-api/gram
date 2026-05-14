package access

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
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

func TestService_ApproveShadowMCPApprovalRequest_CreatesProjectScopedAllowRule(t *testing.T) {
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

	result, err := ti.service.ApproveShadowMCPApprovalRequest(ctx, &gen.ApproveShadowMCPApprovalRequestPayload{
		ID:           request.ID,
		AccessScope:  shadowMCPAccessScopeProject,
		MatchBreadth: "full_url",
		MatchValue:   fullURL,
		DisplayName:  "GitHub Shadow MCP",
		Reason:       conv.PtrEmpty("Approved for AI platform operators"),
	})
	require.NoError(t, err)

	require.Equal(t, shadowMCPRequestStatusApproved, result.Request.Status)
	require.Equal(t, shadowMCPRuleAllowed, result.Rule.Disposition)
	require.Equal(t, "full_url", result.Rule.MatchBreadth)
	require.Equal(t, fullURL, result.Rule.MatchValue)
	require.Equal(t, shadowMCPAccessScopeProject, result.Rule.AccessScope)
	require.Equal(t, project.ID.String(), *result.Rule.ProjectID)
	require.Equal(t, request.ID, *result.Rule.SourceRequestID)
}

func TestService_ApproveShadowMCPApprovalRequest_CreatesOrganizationScopedAllowRule(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	project := createShadowMCPProject(t, ctx, ti, authCtx.ActiveOrganizationID)
	fullURL := "https://github.example.com/org/repo/mcp"

	request, err := ti.service.CreateShadowMCPApprovalRequest(ctx, &gen.CreateShadowMCPApprovalRequestPayload{
		RequestToken: shadowMCPRequestToken(t, authCtx.ActiveOrganizationID, authCtx.UserID, project.ID.String(), ShadowMCPApprovalRequestTokenInput{
			ObservedName:    conv.PtrEmpty("GitHub"),
			ObservedFullURL: &fullURL,
		}),
	})
	require.NoError(t, err)

	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})
	result, err := ti.service.ApproveShadowMCPApprovalRequest(ctx, &gen.ApproveShadowMCPApprovalRequestPayload{
		ID:           request.ID,
		AccessScope:  shadowMCPAccessScopeOrganization,
		MatchBreadth: "full_url",
		MatchValue:   fullURL,
		DisplayName:  "GitHub Shadow MCP",
	})
	require.NoError(t, err)

	require.Equal(t, shadowMCPAccessScopeOrganization, result.Rule.AccessScope)
	require.Nil(t, result.Rule.ProjectID)
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

func TestService_ListShadowMCPApprovalRequests_CursorPagination(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	project := createShadowMCPProject(t, ctx, ti, authCtx.ActiveOrganizationID)

	for _, server := range []struct {
		name string
		host string
	}{
		{name: "First", host: "first.example.com"},
		{name: "Second", host: "second.example.com"},
		{name: "Third", host: "third.example.com"},
	} {
		_, err := ti.service.CreateShadowMCPApprovalRequest(ctx, &gen.CreateShadowMCPApprovalRequestPayload{
			RequestToken: shadowMCPRequestToken(t, authCtx.ActiveOrganizationID, authCtx.UserID, project.ID.String(), ShadowMCPApprovalRequestTokenInput{
				ObservedName:    conv.PtrEmpty(server.name),
				ObservedURLHost: &server.host,
			}),
		})
		require.NoError(t, err)
	}

	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})
	firstPage, err := ti.service.ListShadowMCPApprovalRequests(ctx, &gen.ListShadowMCPApprovalRequestsPayload{Limit: 2})
	require.NoError(t, err)
	require.Len(t, firstPage.Requests, 2)
	require.NotNil(t, firstPage.NextCursor)

	secondPage, err := ti.service.ListShadowMCPApprovalRequests(ctx, &gen.ListShadowMCPApprovalRequestsPayload{
		Limit:  2,
		Cursor: firstPage.NextCursor,
	})
	require.NoError(t, err)
	require.Len(t, secondPage.Requests, 1)
	require.Nil(t, secondPage.NextCursor)
}

func TestShadowMCPLimit_RequiresPositiveLimit(t *testing.T) {
	t.Parallel()

	_, err := shadowMCPLimit(0)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestShadowMCPLimit_RejectsExcessiveLimit(t *testing.T) {
	t.Parallel()

	_, err := shadowMCPLimit(shadowMCPMaxPageLimit + 1)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestService_ShadowMCPAccessRule_ManualLifecycle(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
		authz.Grant{Scope: authz.ScopeOrgRead, Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID)},
	)

	rule, err := ti.service.CreateShadowMCPAccessRule(ctx, &gen.CreateShadowMCPAccessRulePayload{
		Disposition:  shadowMCPRuleAllowed,
		AccessScope:  shadowMCPAccessScopeOrganization,
		MatchBreadth: "url_host",
		MatchValue:   "notion.example.com",
		DisplayName:  "Notion Shadow MCP",
		Reason:       conv.PtrEmpty("Manual approval"),
	})
	require.NoError(t, err)
	require.Equal(t, shadowMCPRuleAllowed, rule.Disposition)
	require.Equal(t, shadowMCPAccessScopeOrganization, rule.AccessScope)
	require.Nil(t, rule.ProjectID)

	updated, err := ti.service.UpdateShadowMCPAccessRule(ctx, &gen.UpdateShadowMCPAccessRulePayload{
		ID:           rule.ID,
		Disposition:  shadowMCPRuleDenied,
		AccessScope:  shadowMCPAccessScopeOrganization,
		MatchBreadth: "url_host",
		MatchValue:   "notion.example.com",
		DisplayName:  "Notion Shadow MCP",
		Reason:       conv.PtrEmpty("Denied after review"),
	})
	require.NoError(t, err)
	require.Equal(t, shadowMCPRuleDenied, updated.Disposition)

	err = ti.service.DeleteShadowMCPAccessRule(ctx, &gen.DeleteShadowMCPAccessRulePayload{ID: rule.ID})
	require.NoError(t, err)

	result, err := ti.service.ListShadowMCPAccessRules(ctx, &gen.ListShadowMCPAccessRulesPayload{Limit: 10})
	require.NoError(t, err)
	require.Empty(t, result.Rules)
	require.Nil(t, result.NextCursor)
}

func TestService_ListShadowMCPAccessRules_CursorPagination(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
		authz.Grant{Scope: authz.ScopeOrgRead, Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID)},
	)

	for _, host := range []string{"one.example.com", "two.example.com", "three.example.com"} {
		_, err := ti.service.CreateShadowMCPAccessRule(ctx, &gen.CreateShadowMCPAccessRulePayload{
			Disposition:  shadowMCPRuleDenied,
			AccessScope:  shadowMCPAccessScopeOrganization,
			MatchBreadth: "url_host",
			MatchValue:   host,
			DisplayName:  host,
		})
		require.NoError(t, err)
	}

	firstPage, err := ti.service.ListShadowMCPAccessRules(ctx, &gen.ListShadowMCPAccessRulesPayload{Limit: 2})
	require.NoError(t, err)
	require.Len(t, firstPage.Rules, 2)
	require.NotNil(t, firstPage.NextCursor)

	secondPage, err := ti.service.ListShadowMCPAccessRules(ctx, &gen.ListShadowMCPAccessRulesPayload{
		Limit:  2,
		Cursor: firstPage.NextCursor,
	})
	require.NoError(t, err)
	require.Len(t, secondPage.Rules, 1)
	require.Nil(t, secondPage.NextCursor)
}

func TestService_CreateShadowMCPAccessRule_NormalizesMatchValue(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	rule, err := ti.service.CreateShadowMCPAccessRule(ctx, &gen.CreateShadowMCPAccessRulePayload{
		Disposition:  shadowMCPRuleDenied,
		AccessScope:  shadowMCPAccessScopeOrganization,
		MatchBreadth: "url_host",
		MatchValue:   "HTTPS://Example.COM:443/path",
		DisplayName:  "Example Shadow MCP",
	})
	require.NoError(t, err)
	require.Equal(t, "example.com", rule.MatchValue)

	_, err = ti.service.CreateShadowMCPAccessRule(ctx, &gen.CreateShadowMCPAccessRulePayload{
		Disposition:  shadowMCPRuleDenied,
		AccessScope:  shadowMCPAccessScopeOrganization,
		MatchBreadth: "url_host",
		MatchValue:   "example.com",
		DisplayName:  "Duplicate Example Shadow MCP",
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeConflict, oopsErr.Code)
}

func TestService_CreateShadowMCPAccessRule_RequiresProjectIDForProjectScope(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	_, err := ti.service.CreateShadowMCPAccessRule(ctx, &gen.CreateShadowMCPAccessRulePayload{
		Disposition:  shadowMCPRuleAllowed,
		AccessScope:  shadowMCPAccessScopeProject,
		MatchBreadth: "url_host",
		MatchValue:   "missing-project.example.com",
		DisplayName:  "Missing Project",
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestService_CreateShadowMCPAccessRule_CreatesProjectScopedRule(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	project := createShadowMCPProject(t, ctx, ti, authCtx.ActiveOrganizationID)
	projectID := project.ID.String()
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	rule, err := ti.service.CreateShadowMCPAccessRule(ctx, &gen.CreateShadowMCPAccessRulePayload{
		Disposition:  shadowMCPRuleAllowed,
		AccessScope:  shadowMCPAccessScopeProject,
		ProjectID:    &projectID,
		MatchBreadth: "url_host",
		MatchValue:   "project.example.com",
		DisplayName:  "Project Rule",
	})
	require.NoError(t, err)
	require.Equal(t, shadowMCPAccessScopeProject, rule.AccessScope)
	require.Equal(t, project.ID.String(), *rule.ProjectID)
}

func TestService_UpdateShadowMCPAccessRule_RequiresProjectIDForProjectScope(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	rule, err := ti.service.CreateShadowMCPAccessRule(ctx, &gen.CreateShadowMCPAccessRulePayload{
		Disposition:  shadowMCPRuleDenied,
		AccessScope:  shadowMCPAccessScopeOrganization,
		MatchBreadth: "url_host",
		MatchValue:   "update-missing-project.example.com",
		DisplayName:  "Update Missing Project",
	})
	require.NoError(t, err)

	_, err = ti.service.UpdateShadowMCPAccessRule(ctx, &gen.UpdateShadowMCPAccessRulePayload{
		ID:           rule.ID,
		Disposition:  shadowMCPRuleAllowed,
		AccessScope:  shadowMCPAccessScopeProject,
		MatchBreadth: "url_host",
		MatchValue:   "update-missing-project.example.com",
		DisplayName:  "Update Missing Project",
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestService_CreateShadowMCPAccessRule_AuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionShadowMCPAccessRuleCreate)
	require.NoError(t, err)

	rule, err := ti.service.CreateShadowMCPAccessRule(ctx, &gen.CreateShadowMCPAccessRulePayload{
		Disposition:  shadowMCPRuleDenied,
		AccessScope:  shadowMCPAccessScopeOrganization,
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
		AccessScope:  shadowMCPAccessScopeOrganization,
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
