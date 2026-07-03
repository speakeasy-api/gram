package access

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/accesscontrol"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestService_AllowShadowMCPInventoryServer_CanonicalizesAndIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	projectID := authCtx.ProjectID.String()
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionShadowMCPAccessRuleCreate)
	require.NoError(t, err)

	first, err := ti.service.AllowShadowMCPInventoryServer(ctx, &gen.AllowShadowMCPInventoryServerPayload{
		ProjectID:  projectID,
		ServerURL:  "HTTPS://Example.COM:443/mcp?token=secret#fragment",
		ServerName: conv.PtrEmpty("Example MCP"),
		Reason:     conv.PtrEmpty("Approved for project"),
	})
	require.NoError(t, err)
	require.Equal(t, "https://example.com/mcp", first.CanonicalServerURL)
	require.Equal(t, "example.com", first.URLHost)
	require.Equal(t, shadowMCPInventoryAccessAllowed, first.Access)
	require.NotNil(t, first.Rule)
	require.Equal(t, shadowMCPRuleAllowed, first.Rule.Disposition)
	require.Equal(t, shadowMCPAccessScopeProject, first.Rule.AccessScope)
	require.Equal(t, projectID, *first.Rule.ProjectID)
	require.Equal(t, "full_url", first.Rule.MatchBreadth)
	require.Equal(t, "https://example.com/mcp", first.Rule.MatchValue)
	require.Equal(t, "Example MCP", first.Rule.DisplayName)

	second, err := ti.service.AllowShadowMCPInventoryServer(ctx, &gen.AllowShadowMCPInventoryServerPayload{
		ProjectID: projectID,
		ServerURL: "https://example.com/mcp?another=ignored",
	})
	require.NoError(t, err)
	require.NotNil(t, second.Rule)
	require.Equal(t, first.Rule.ID, second.Rule.ID)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionShadowMCPAccessRuleCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestService_BatchAllowShadowMCPInventoryServers_AllowsSelectedURLs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	projectID := authCtx.ProjectID.String()
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionShadowMCPAccessRuleCreate)
	require.NoError(t, err)

	result, err := ti.service.BatchAllowShadowMCPInventoryServers(ctx, &gen.BatchAllowShadowMCPInventoryServersPayload{
		ProjectID: projectID,
		Servers: []*gen.ShadowMCPInventoryBatchAllowServer{
			{ServerURL: "https://batch-one.example.com/mcp?token=ignored", ServerName: conv.PtrEmpty("Batch One")},
			{ServerURL: "HTTPS://Batch-Two.Example.Com:443/mcp#fragment", ServerName: conv.PtrEmpty("Batch Two")},
			{ServerURL: "https://batch-one.example.com/mcp?different=ignored", ServerName: conv.PtrEmpty("Batch One Duplicate")},
		},
		Reason: conv.PtrEmpty("Allow selected servers during setup"),
	})
	require.NoError(t, err)
	require.Len(t, result.Results, 3)

	first := result.Results[0]
	require.True(t, first.Success)
	require.Nil(t, first.ErrorCode)
	require.NotNil(t, first.AccessState)
	require.Equal(t, "https://batch-one.example.com/mcp", first.AccessState.CanonicalServerURL)
	require.Equal(t, shadowMCPInventoryAccessAllowed, first.AccessState.Access)
	require.NotNil(t, first.AccessState.Rule)
	require.Equal(t, "Batch One", first.AccessState.Rule.DisplayName)

	second := result.Results[1]
	require.True(t, second.Success)
	require.NotNil(t, second.AccessState)
	require.Equal(t, "https://batch-two.example.com/mcp", second.AccessState.CanonicalServerURL)
	require.Equal(t, shadowMCPInventoryAccessAllowed, second.AccessState.Access)

	duplicate := result.Results[2]
	require.True(t, duplicate.Success)
	require.NotNil(t, duplicate.AccessState)
	require.Equal(t, first.AccessState.Rule.ID, duplicate.AccessState.Rule.ID)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionShadowMCPAccessRuleCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+2, afterCount)
}

func TestService_BatchAllowShadowMCPInventoryServers_ReturnsPerURLErrors(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	projectID := authCtx.ProjectID.String()
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	result, err := ti.service.BatchAllowShadowMCPInventoryServers(ctx, &gen.BatchAllowShadowMCPInventoryServersPayload{
		ProjectID: projectID,
		Servers: []*gen.ShadowMCPInventoryBatchAllowServer{
			{ServerURL: "not a url"},
			{ServerURL: "https://valid-batch.example.com/mcp"},
		},
	})
	require.NoError(t, err)
	require.Len(t, result.Results, 2)
	require.False(t, result.Results[0].Success)
	require.Nil(t, result.Results[0].AccessState)
	require.Equal(t, "invalid_url", *result.Results[0].ErrorCode)
	require.NotEmpty(t, *result.Results[0].ErrorMessage)
	require.True(t, result.Results[1].Success)
	require.NotNil(t, result.Results[1].AccessState)
	require.Equal(t, "https://valid-batch.example.com/mcp", result.Results[1].AccessState.CanonicalServerURL)
}

func TestService_BatchAllowShadowMCPInventoryServers_RejectsTooManyURLs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})
	servers := make([]*gen.ShadowMCPInventoryBatchAllowServer, shadowMCPInventoryBatchAllowMaxSize+1)
	for i := range servers {
		servers[i] = &gen.ShadowMCPInventoryBatchAllowServer{ServerURL: "https://too-many.example.com/mcp"}
	}

	_, err := ti.service.BatchAllowShadowMCPInventoryServers(ctx, &gen.BatchAllowShadowMCPInventoryServersPayload{
		ProjectID: authCtx.ProjectID.String(),
		Servers:   servers,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestService_BlockShadowMCPInventoryServer_UpdatesExistingURLRule(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	projectID := authCtx.ProjectID.String()
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	allowed, err := ti.service.AllowShadowMCPInventoryServer(ctx, &gen.AllowShadowMCPInventoryServerPayload{
		ProjectID:  projectID,
		ServerURL:  "https://switch.example.com/mcp",
		ServerName: conv.PtrEmpty("Switch MCP"),
	})
	require.NoError(t, err)
	require.NotNil(t, allowed.Rule)
	updateBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionShadowMCPAccessRuleUpdate)
	require.NoError(t, err)

	blocked, err := ti.service.BlockShadowMCPInventoryServer(ctx, &gen.BlockShadowMCPInventoryServerPayload{
		ProjectID:  projectID,
		ServerURL:  "https://switch.example.com/mcp?ignored=true",
		ServerName: conv.PtrEmpty("Switch MCP"),
		Reason:     conv.PtrEmpty("Block this server"),
	})
	require.NoError(t, err)
	require.Equal(t, shadowMCPInventoryAccessDenied, blocked.Access)
	require.NotNil(t, blocked.Rule)
	require.Equal(t, allowed.Rule.ID, blocked.Rule.ID)
	require.Equal(t, shadowMCPRuleDenied, blocked.Rule.Disposition)

	updateAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionShadowMCPAccessRuleUpdate)
	require.NoError(t, err)
	require.Equal(t, updateBefore+1, updateAfter)
}

func TestService_ClearShadowMCPInventoryServerAccess_RemovesURLRule(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	projectID := authCtx.ProjectID.String()
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	_ = createShadowMCPAccessRuleForTest(t, ctx, ti, &gen.CreateShadowMCPAccessRulePayload{
		Disposition:  shadowMCPRuleDenied,
		AccessScope:  shadowMCPAccessScopeOrganization,
		MatchBreadth: "url_host",
		MatchValue:   "clear.example.com",
		DisplayName:  "Broader deny",
	})
	allowed, err := ti.service.AllowShadowMCPInventoryServer(ctx, &gen.AllowShadowMCPInventoryServerPayload{
		ProjectID:  projectID,
		ServerURL:  "https://clear.example.com/mcp",
		ServerName: conv.PtrEmpty("Clear MCP"),
	})
	require.NoError(t, err)
	require.Equal(t, shadowMCPInventoryAccessAllowed, allowed.Access)
	deleteBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionShadowMCPAccessRuleDelete)
	require.NoError(t, err)

	cleared, err := ti.service.ClearShadowMCPInventoryServerAccess(ctx, &gen.ClearShadowMCPInventoryServerAccessPayload{
		ProjectID: projectID,
		ServerURL: "https://clear.example.com/mcp?token=ignored",
	})
	require.NoError(t, err)
	require.Equal(t, shadowMCPInventoryAccessNone, cleared.Access)
	require.Nil(t, cleared.Rule)

	clearedAgain, err := ti.service.ClearShadowMCPInventoryServerAccess(ctx, &gen.ClearShadowMCPInventoryServerAccessPayload{
		ProjectID: projectID,
		ServerURL: "https://clear.example.com/mcp",
	})
	require.NoError(t, err)
	require.Equal(t, shadowMCPInventoryAccessNone, clearedAgain.Access)
	deleteAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionShadowMCPAccessRuleDelete)
	require.NoError(t, err)
	require.Equal(t, deleteBefore+1, deleteAfter)
}

func TestService_AllowShadowMCPInventoryServer_RejectsInvalidURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	_, err := ti.service.AllowShadowMCPInventoryServer(ctx, &gen.AllowShadowMCPInventoryServerPayload{
		ProjectID: authCtx.ProjectID.String(),
		ServerURL: "not a url",
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestService_CreateShadowMCPInventoryServerAccessRule_ReportsLookupFailureAfterConflict(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ti.service.accessStore = shadowMCPInventoryConflictLookupFailStore{
		Store:     ti.service.accessStore,
		lookupErr: accesscontrol.ErrNotFound,
	}

	_, err := ti.service.createShadowMCPInventoryServerAccessRule(ctx, accesscontrol.AccessRule{
		ID:              "",
		OrganizationID:  "org-1",
		ProjectID:       "project-1",
		AccessScope:     accesscontrol.AccessScopeProject,
		ResourceType:    accesscontrol.ResourceTypeShadowMCP,
		Disposition:     accesscontrol.DispositionAllowed,
		MatchKind:       accesscontrol.MatchKindFullURL,
		MatchValue:      "https://example.com/mcp",
		DisplayName:     "Example MCP",
		ObservedSummary: accesscontrol.ObservedSummary{},
		SourceRequestID: "",
		CreatedBy:       "user-1",
		UpdatedBy:       "user-1",
		Reason:          "",
		CreatedAt:       time.Time{},
		UpdatedAt:       time.Time{},
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestService_ShadowMCPInventoryServerAccess_ForbiddenWithoutOrgAdminGrant(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		run  func(ctx context.Context, ti *testInstance, projectID string) error
	}{
		{
			name: "allow",
			run: func(ctx context.Context, ti *testInstance, projectID string) error {
				_, err := ti.service.AllowShadowMCPInventoryServer(ctx, &gen.AllowShadowMCPInventoryServerPayload{
					ProjectID: projectID,
					ServerURL: "https://forbidden.example.com/mcp",
				})
				return err
			},
		},
		{
			name: "block",
			run: func(ctx context.Context, ti *testInstance, projectID string) error {
				_, err := ti.service.BlockShadowMCPInventoryServer(ctx, &gen.BlockShadowMCPInventoryServerPayload{
					ProjectID: projectID,
					ServerURL: "https://forbidden.example.com/mcp",
				})
				return err
			},
		},
		{
			name: "clear",
			run: func(ctx context.Context, ti *testInstance, projectID string) error {
				_, err := ti.service.ClearShadowMCPInventoryServerAccess(ctx, &gen.ClearShadowMCPInventoryServerAccessPayload{
					ProjectID: projectID,
					ServerURL: "https://forbidden.example.com/mcp",
				})
				return err
			},
		},
		{
			name: "batch allow",
			run: func(ctx context.Context, ti *testInstance, projectID string) error {
				_, err := ti.service.BatchAllowShadowMCPInventoryServers(ctx, &gen.BatchAllowShadowMCPInventoryServersPayload{
					ProjectID: projectID,
					Servers: []*gen.ShadowMCPInventoryBatchAllowServer{
						{ServerURL: "https://forbidden.example.com/mcp"},
					},
				})
				return err
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestAccessService(t)
			authCtx := testAccessAuthContext(t, ctx)
			ctx = withRBACGrants(t, ctx)

			err := tt.run(ctx, ti, authCtx.ProjectID.String())
			var oopsErr *oops.ShareableError
			require.ErrorAs(t, err, &oopsErr)
			require.Equal(t, oops.CodeForbidden, oopsErr.Code)
		})
	}
}

type shadowMCPInventoryConflictLookupFailStore struct {
	accesscontrol.Store
	lookupErr error
}

func (s shadowMCPInventoryConflictLookupFailStore) GetOrCreateRules(context.Context, []accesscontrol.AccessRule) ([]accesscontrol.RuleUpsertResult, error) {
	return nil, accesscontrol.ErrConflict
}

func (s shadowMCPInventoryConflictLookupFailStore) GetRuleByMatch(context.Context, string, string, string, string, string, string) (accesscontrol.AccessRule, error) {
	return accesscontrol.AccessRule{}, s.lookupErr
}
