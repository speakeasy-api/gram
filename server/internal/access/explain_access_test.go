package access

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func seedGrantWithSelector(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, principal urn.Principal, scope authz.Scope, selector authz.Selector) {
	t.Helper()

	selectors, err := selector.MarshalJSON()
	require.NoError(t, err)

	_, err = accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: organizationID,
		PrincipalUrn:   principal,
		Scope:          string(scope),
		Selectors:      selectors,
	})
	require.NoError(t, err)
}

// explainedRuleSummary is the comparable shape of one explained rule.
type explainedRuleSummary struct {
	displayName string
	level       string
	effect      string
}

func explainedLevel(t *testing.T, result *gen.ExplainResourceAccessResult, level string) *gen.ExplainedAccessLevel {
	t.Helper()

	for _, explained := range result.Levels {
		if explained.Level == level {
			return explained
		}
	}
	require.FailNow(t, "level not explained", level)
	return nil
}

func summarizeExplainedRules(rules []*gen.ExplainedAccessRule) []explainedRuleSummary {
	out := make([]explainedRuleSummary, 0, len(rules))
	for _, rule := range rules {
		out = append(out, explainedRuleSummary{displayName: rule.DisplayName, level: rule.Level, effect: rule.Effect})
	}
	return out
}

func explainedRule(t *testing.T, rules []*gen.ExplainedAccessRule, displayName string) *gen.ExplainedAccessRule {
	t.Helper()

	for _, rule := range rules {
		if rule.DisplayName == displayName {
			return rule
		}
	}
	require.FailNow(t, "rule not explained", displayName)
	return nil
}

// explainAccessFixture is a server and a member holding Engineer directly and
// Contractors through a directory group mapping. Engineer grants use and view
// on every server; Contractors blocks use on this one.
type explainAccessFixture struct {
	orgID    string
	serverID string
	userID   string
}

func seedExplainAccessFixture(t *testing.T, ctx context.Context, ti *testInstance) explainAccessFixture {
	t.Helper()

	orgID := testAccessAuthContext(t, ctx).ActiveOrganizationID
	serverID := seedRemoteMCPServer(t, ctx, ti.conn, orgID)

	seedRole(t, ctx, ti.conn, orgID, mockRole("role_engineer", "Engineer", "engineer", ""))
	engineer := seededRolePrincipal(t, ctx, ti.conn, orgID, "engineer")
	seedGrant(t, ctx, ti.conn, orgID, engineer, authz.ScopeMCPConnect, authz.WildcardResource)
	seedGrant(t, ctx, ti.conn, orgID, engineer, authz.ScopeMCPRead, authz.WildcardResource)

	seedRole(t, ctx, ti.conn, orgID, mockRole("role_contractors", "Contractors", "contractors", ""))
	contractors := seededRolePrincipal(t, ctx, ti.conn, orgID, "contractors")
	seedGrant(t, ctx, ti.conn, orgID, contractors, authz.ScopeMCPBlockedConnect, serverID)

	userID := "local_mateo"
	seedConnectedUser(t, ctx, ti.conn, orgID, userID, "mateo@test.com", "Mateo", "user_mateo", "membership_mateo")
	seedRoleAssignment(t, ctx, ti.conn, orgID, userID, mockMember("", "membership_mateo", "user_mateo", "engineer"))

	directoryUserID, workosUserID := seedMappingDirectoryUser(t, ctx, ti.conn, orgID, userID, "mateo@test.com", `{}`)
	groupID, workosGroupID := seedMappingDirectoryGroupWithWorkOSID(t, ctx, ti.conn, orgID, "okta/contractors")
	addMappingGroupMember(t, ctx, ti.conn, directoryUserID, workosUserID, groupID, workosGroupID)
	group := groupID.String()
	seedMappingAdministrator(t, ctx, ti)
	_, err := ti.service.SetDirectoryRoleMappings(ctx, &gen.SetDirectoryRoleMappingsPayload{
		SourceKind:       directoryRoleMappingSourceGroup,
		DirectoryGroupID: &group,
		RoleUrns:         []string{contractors.String()},
	})
	require.NoError(t, err)

	return explainAccessFixture{orgID: orgID, serverID: serverID, userID: userID}
}

func explainAccess(t *testing.T, ctx context.Context, ti *testInstance, serverID, userID string) *gen.ExplainResourceAccessResult {
	t.Helper()

	result, err := ti.service.ExplainResourceAccess(ctx, &gen.ExplainResourceAccessPayload{
		ResourceKind: "mcp",
		ResourceID:   serverID,
		UserID:       userID,
		SessionToken: nil,
	})
	require.NoError(t, err)
	return result
}

func TestService_ExplainResourceAccess_BlockFromMappedRoleWins(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	fixture := seedExplainAccessFixture(t, ctx, ti)

	result := explainAccess(t, ctx, ti, fixture.serverID, fixture.userID)
	require.Equal(t, "private", result.Visibility)
	require.Len(t, result.Levels, 3)

	use := explainedLevel(t, result, audienceLevelUse)
	require.False(t, use.Allowed)
	require.Equal(t, explainedToolAccessNone, *use.ToolAccess)
	require.Equal(t, []explainedRuleSummary{
		{displayName: "Contractors", level: audienceLevelBlocked, effect: string(authz.GrantEffectBlocks)},
		{displayName: "Engineer", level: audienceLevelUse, effect: string(authz.GrantEffectBlocked)},
		{displayName: "Engineer", level: audienceLevelView, effect: string(authz.GrantEffectBlocked)},
	}, summarizeExplainedRules(use.Rules))

	contractors := explainedRule(t, use.Rules, "Contractors")
	require.Equal(t, "role", contractors.Kind)
	require.Equal(t, audienceAppliesToResource, contractors.AppliesTo)
	require.True(t, contractors.ViaDirectoryMapping)
	require.Len(t, contractors.DirectorySources, 1)
	require.Equal(t, directoryRoleMappingSourceGroup, contractors.DirectorySources[0].SourceKind)
	require.Equal(t, "okta/contractors", *contractors.DirectorySources[0].DirectoryGroupName)

	engineer := explainedRule(t, use.Rules, "Engineer")
	require.False(t, engineer.ViaDirectoryMapping)
	require.Equal(t, audienceAppliesToAllResources, engineer.AppliesTo)

	view := explainedLevel(t, result, audienceLevelView)
	require.True(t, view.Allowed, "blocking use leaves view alone")
	require.Nil(t, view.ToolAccess)
	require.Equal(t, []explainedRuleSummary{
		{displayName: "Engineer", level: audienceLevelView, effect: string(authz.GrantEffectAllows)},
	}, summarizeExplainedRules(view.Rules))

	manage := explainedLevel(t, result, audienceLevelManage)
	require.False(t, manage.Allowed)
	require.Empty(t, manage.Rules)
}

func TestService_ExplainResourceAccess_ShowsEachRoleFromOneGroup(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	enableDirectoryRoleSetsForTest(t, ctx, ti)
	fixture := seedExplainAccessFixture(t, ctx, ti)
	mappings, err := ti.service.ListDirectoryRoleMappings(ctx, &gen.ListDirectoryRoleMappingsPayload{})
	require.NoError(t, err)
	require.Len(t, mappings.Mappings, 1)
	engineer := seededRolePrincipal(t, ctx, ti.conn, fixture.orgID, "engineer")
	contractors := seededRolePrincipal(t, ctx, ti.conn, fixture.orgID, "contractors")
	_, err = ti.service.SetDirectoryRoleMappings(ctx, &gen.SetDirectoryRoleMappingsPayload{
		SourceKind:       directoryRoleMappingSourceGroup,
		DirectoryGroupID: mappings.Mappings[0].DirectoryGroupID,
		RoleUrns:         []string{engineer.String(), contractors.String()},
	})
	require.NoError(t, err)
	use := explainedLevel(t, explainAccess(t, ctx, ti, fixture.serverID, fixture.userID), audienceLevelUse)
	for _, name := range []string{"Engineer", "Contractors"} {
		rule := explainedRule(t, use.Rules, name)
		require.Equal(t, name == "Contractors", rule.ViaDirectoryMapping, "unexpected directory-only marker for %s", name)
		require.Len(t, rule.DirectorySources, 1)
		require.Equal(t, "okta/contractors", *rule.DirectorySources[0].DirectoryGroupName)
	}
}

func TestService_ExplainResourceAccess_DirectGrantOutranksRoleBlock(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	fixture := seedExplainAccessFixture(t, ctx, ti)
	seedGrant(t, ctx, ti.conn, fixture.orgID, urn.NewPrincipal(urn.PrincipalTypeUser, fixture.userID), authz.ScopeMCPConnect, fixture.serverID)

	use := explainedLevel(t, explainAccess(t, ctx, ti, fixture.serverID, fixture.userID), audienceLevelUse)
	require.True(t, use.Allowed)
	require.Equal(t, explainedToolAccessAll, *use.ToolAccess)
	require.Equal(t, []explainedRuleSummary{
		{displayName: "Mateo", level: audienceLevelUse, effect: string(authz.GrantEffectOverrides)},
		{displayName: "Contractors", level: audienceLevelBlocked, effect: string(authz.GrantEffectOverridden)},
		{displayName: "Engineer", level: audienceLevelUse, effect: string(authz.GrantEffectBlocked)},
		{displayName: "Engineer", level: audienceLevelView, effect: string(authz.GrantEffectBlocked)},
	}, summarizeExplainedRules(use.Rules))
	require.Equal(t, "user", explainedRule(t, use.Rules, "Mateo").Kind)
}

func TestService_ExplainResourceAccess_WildcardDirectGrantDoesNotOutrankRoleBlock(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	fixture := seedExplainAccessFixture(t, ctx, ti)
	seedGrant(t, ctx, ti.conn, fixture.orgID, urn.NewPrincipal(urn.PrincipalTypeUser, fixture.userID), authz.ScopeMCPConnect, authz.WildcardResource)

	use := explainedLevel(t, explainAccess(t, ctx, ti, fixture.serverID, fixture.userID), audienceLevelUse)
	require.False(t, use.Allowed)
	mateo := explainedRule(t, use.Rules, "Mateo")
	require.Equal(t, string(authz.GrantEffectBlocked), mateo.Effect)
	require.Equal(t, string(authz.BlockedReasonWildcardDirectGrant), *mateo.Reason)
	require.Equal(t, audienceAppliesToAllResources, mateo.AppliesTo)
}

func TestService_ExplainResourceAccess_HidesDirectorySourcesWithoutOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	fixture := seedExplainAccessFixture(t, ctx, ti)

	readerCtx := withRBACGrants(t, ctx,
		authz.NewGrant(authz.ScopeOrgRead, fixture.orgID),
		authz.NewGrant(authz.ScopeMCPRead, fixture.serverID),
	)
	use := explainedLevel(t, explainAccess(t, readerCtx, ti, fixture.serverID, fixture.userID), audienceLevelUse)
	contractors := explainedRule(t, use.Rules, "Contractors")
	require.True(t, contractors.ViaDirectoryMapping, "the role is still marked as directory mapped")
	require.Empty(t, contractors.DirectorySources, "which mapping produced it needs org:admin")
}

func TestService_ExplainResourceAccess_ToolBlockLimitsUse(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	orgID := testAccessAuthContext(t, ctx).ActiveOrganizationID
	serverID := seedRemoteMCPServer(t, ctx, ti.conn, orgID)

	seedRole(t, ctx, ti.conn, orgID, mockRole("role_analyst", "Analyst", "analyst", ""))
	analyst := seededRolePrincipal(t, ctx, ti.conn, orgID, "analyst")
	seedGrant(t, ctx, ti.conn, orgID, analyst, authz.ScopeMCPConnect, serverID)
	destructive := authz.NewSelector(authz.ScopeMCPBlockedConnect, serverID)
	destructive[authz.SelectorKeyDisposition] = authz.DispositionDestructive
	seedGrantWithSelector(t, ctx, ti.conn, orgID, analyst, authz.ScopeMCPBlockedConnect, destructive)

	seedConnectedUser(t, ctx, ti.conn, orgID, "local_amara", "amara@test.com", "Amara", "user_amara", "membership_amara")
	seedRoleAssignment(t, ctx, ti.conn, orgID, "local_amara", mockMember("", "membership_amara", "user_amara", "analyst"))

	use := explainedLevel(t, explainAccess(t, ctx, ti, serverID, "local_amara"), audienceLevelUse)
	require.True(t, use.Allowed)
	require.Equal(t, explainedToolAccessSome, *use.ToolAccess)
	require.Equal(t, []explainedRuleSummary{
		{displayName: "Analyst", level: audienceLevelUse, effect: string(authz.GrantEffectAllows)},
		{displayName: "Analyst", level: audienceLevelBlocked, effect: explainedEffectLimits},
	}, summarizeExplainedRules(use.Rules))
	require.Equal(t, []string{authz.DispositionDestructive}, use.Rules[1].Dispositions)
}

func TestService_ExplainResourceAccess_NarrowedGrantGivesSomeTools(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	orgID := testAccessAuthContext(t, ctx).ActiveOrganizationID
	serverID := seedRemoteMCPServer(t, ctx, ti.conn, orgID)

	readOnly := authz.NewSelector(authz.ScopeMCPConnect, serverID)
	readOnly[authz.SelectorKeyDisposition] = authz.DispositionReadOnly
	seedConnectedUser(t, ctx, ti.conn, orgID, "local_hana", "hana@test.com", "Hana", "user_hana", "membership_hana")
	seedGrantWithSelector(t, ctx, ti.conn, orgID, urn.NewPrincipal(urn.PrincipalTypeUser, "local_hana"), authz.ScopeMCPConnect, readOnly)

	use := explainedLevel(t, explainAccess(t, ctx, ti, serverID, "local_hana"), audienceLevelUse)
	require.True(t, use.Allowed)
	require.Equal(t, explainedToolAccessSome, *use.ToolAccess)
	require.Len(t, use.Rules, 1)
	require.Equal(t, []string{authz.DispositionReadOnly}, use.Rules[0].Dispositions)
}

func TestService_ExplainResourceAccess_NoRule(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	orgID := testAccessAuthContext(t, ctx).ActiveOrganizationID
	serverID := seedRemoteMCPServer(t, ctx, ti.conn, orgID)
	seedConnectedUser(t, ctx, ti.conn, orgID, "local_daniel", "daniel@test.com", "Daniel", "user_daniel", "membership_daniel")

	result := explainAccess(t, ctx, ti, serverID, "local_daniel")
	for _, level := range result.Levels {
		require.False(t, level.Allowed, level.Level)
		require.Empty(t, level.Rules, level.Level)
	}
	require.Equal(t, explainedToolAccessNone, *explainedLevel(t, result, audienceLevelUse).ToolAccess)
}

func TestService_ExplainResourceAccess_RejectsNonMember(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	orgID := testAccessAuthContext(t, ctx).ActiveOrganizationID
	serverID := seedRemoteMCPServer(t, ctx, ti.conn, orgID)

	_, err := ti.service.ExplainResourceAccess(ctx, &gen.ExplainResourceAccessPayload{
		ResourceKind: "mcp",
		ResourceID:   serverID,
		UserID:       "local_stranger",
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestService_ExplainResourceAccess_RequiresOrgRead(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	fixture := seedExplainAccessFixture(t, ctx, ti)

	serverOnlyCtx := withRBACGrants(t, ctx, authz.NewGrant(authz.ScopeMCPRead, fixture.serverID))
	_, err := ti.service.ExplainResourceAccess(serverOnlyCtx, &gen.ExplainResourceAccessPayload{
		ResourceKind: "mcp",
		ResourceID:   fixture.serverID,
		UserID:       fixture.userID,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestService_ExplainResourceAccess_RequiresServerRead(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	fixture := seedExplainAccessFixture(t, ctx, ti)

	orgOnlyCtx := withRBACGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, fixture.orgID))
	_, err := ti.service.ExplainResourceAccess(orgOnlyCtx, &gen.ExplainResourceAccessPayload{
		ResourceKind: "mcp",
		ResourceID:   fixture.serverID,
		UserID:       fixture.userID,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestService_ExplainResourceAccess_NarrowedDirectGrantLeavesBlockOnOtherTools(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	fixture := seedExplainAccessFixture(t, ctx, ti)
	oneTool := authz.NewSelector(authz.ScopeMCPConnect, fixture.serverID)
	oneTool[authz.SelectorKeyTool] = "get_issue"
	seedGrantWithSelector(t, ctx, ti.conn, fixture.orgID, urn.NewPrincipal(urn.PrincipalTypeUser, fixture.userID), authz.ScopeMCPConnect, oneTool)

	use := explainedLevel(t, explainAccess(t, ctx, ti, fixture.serverID, fixture.userID), audienceLevelUse)
	require.True(t, use.Allowed, "a direct grant for one tool proves server-level access")
	require.Equal(t, explainedToolAccessSome, *use.ToolAccess)
	contractors := explainedRule(t, use.Rules, "Contractors")
	require.Equal(t, explainedEffectLimits, contractors.Effect, "the block still takes every other tool away")
	mateo := explainedRule(t, use.Rules, "Mateo")
	require.Equal(t, string(authz.GrantEffectOverrides), mateo.Effect)
	require.Equal(t, []string{"get_issue"}, mateo.Tools)
}

func TestService_ExplainResourceAccess_ReportsVisibility(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	orgID := testAccessAuthContext(t, ctx).ActiveOrganizationID
	seedConnectedUser(t, ctx, ti.conn, orgID, "local_viewer", "viewer@test.com", "Viewer", "user_viewer", "membership_viewer")

	for _, visibility := range []string{"public", "private", "disabled"} {
		serverID := seedRemoteMCPServerWithVisibility(t, ctx, ti.conn, orgID, visibility)
		require.Equal(t, visibility, explainAccess(t, ctx, ti, serverID, "local_viewer").Visibility)
	}
}

func TestService_ExplainResourceAccess_ToolsetVisibilityFollowsItsMCPEndpoint(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	orgID := testAccessAuthContext(t, ctx).ActiveOrganizationID
	seedConnectedUser(t, ctx, ti.conn, orgID, "local_viewer", "viewer@test.com", "Viewer", "user_viewer", "membership_viewer")
	toolsetID := seedMCPServer(t, ctx, ti.conn, orgID)
	id := uuid.MustParse(toolsetID)
	projectID, err := accessrepo.New(ti.conn).FindMCPResourceProject(ctx, accessrepo.FindMCPResourceProjectParams{OrganizationID: orgID, ResourceID: id})
	require.NoError(t, err)

	require.Equal(t, "disabled", explainAccess(t, ctx, ti, toolsetID, "local_viewer").Visibility, "a switched-off MCP endpoint serves nobody")

	toolsets := toolsetsrepo.New(ti.conn)
	require.NoError(t, toolsets.SetToolsetMCPEnabledByID(ctx, toolsetsrepo.SetToolsetMCPEnabledByIDParams{McpEnabled: true, ID: id, ProjectID: projectID}))
	require.Equal(t, "private", explainAccess(t, ctx, ti, toolsetID, "local_viewer").Visibility)

	require.NoError(t, toolsets.SetToolsetMCPPublicByID(ctx, toolsetsrepo.SetToolsetMCPPublicByIDParams{McpIsPublic: true, ID: id, ProjectID: projectID}))
	require.Equal(t, "public", explainAccess(t, ctx, ti, toolsetID, "local_viewer").Visibility)
}

func TestService_ExplainResourceAccess_RejectsGateway(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	orgID := testAccessAuthContext(t, ctx).ActiveOrganizationID
	gatewayID := seedMCPGateway(t, ctx, ti.conn, orgID)
	seedConnectedUser(t, ctx, ti.conn, orgID, "local_viewer", "viewer@test.com", "Viewer", "user_viewer", "membership_viewer")

	_, err := ti.service.ExplainResourceAccess(ctx, &gen.ExplainResourceAccessPayload{
		ResourceKind: "mcp",
		ResourceID:   gatewayID,
		UserID:       "local_viewer",
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestService_ExplainResourceAccess_ProjectScopedGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	orgID := testAccessAuthContext(t, ctx).ActiveOrganizationID
	serverID := seedRemoteMCPServer(t, ctx, ti.conn, orgID)
	projectID, err := accessrepo.New(ti.conn).FindMCPResourceProject(ctx, accessrepo.FindMCPResourceProjectParams{OrganizationID: orgID, ResourceID: uuid.MustParse(serverID)})
	require.NoError(t, err)

	seedRole(t, ctx, ti.conn, orgID, mockRole("role_project_team", "Project Team", "project-team", ""))
	team := seededRolePrincipal(t, ctx, ti.conn, orgID, "project-team")
	projectWide := authz.NewSelector(authz.ScopeMCPConnect, authz.WildcardResource)
	projectWide[authz.SelectorKeyProjectID] = projectID.String()
	seedGrantWithSelector(t, ctx, ti.conn, orgID, team, authz.ScopeMCPConnect, projectWide)

	seedConnectedUser(t, ctx, ti.conn, orgID, "local_jonas", "jonas@test.com", "Jonas", "user_jonas", "membership_jonas")
	seedRoleAssignment(t, ctx, ti.conn, orgID, "local_jonas", mockMember("", "membership_jonas", "user_jonas", "project-team"))

	use := explainedLevel(t, explainAccess(t, ctx, ti, serverID, "local_jonas"), audienceLevelUse)
	require.True(t, use.Allowed)
	require.Len(t, use.Rules, 1)
	require.Equal(t, explainedAppliesToProject, use.Rules[0].AppliesTo)
}

func TestService_ExplainResourceAccess_KeepsMappingForDirectlyHeldRole(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	fixture := seedExplainAccessFixture(t, ctx, ti)
	seedRoleAssignment(t, ctx, ti.conn, fixture.orgID, fixture.userID, mockMember("", "membership_mateo", "user_mateo", "contractors"))

	use := explainedLevel(t, explainAccess(t, ctx, ti, fixture.serverID, fixture.userID), audienceLevelUse)
	contractors := explainedRule(t, use.Rules, "Contractors")
	require.False(t, contractors.ViaDirectoryMapping, "the role is also held directly")
	require.Len(t, contractors.DirectorySources, 1, "taking the role away means undoing the mapping too")
	require.Equal(t, "okta/contractors", *contractors.DirectorySources[0].DirectoryGroupName)
}

func TestService_ExplainResourceAccess_WildcardToolRuleCoversEveryTool(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	orgID := testAccessAuthContext(t, ctx).ActiveOrganizationID
	serverID := seedRemoteMCPServer(t, ctx, ti.conn, orgID)
	everyTool := authz.NewSelector(authz.ScopeMCPConnect, serverID)
	everyTool[authz.SelectorKeyTool] = authz.WildcardResource
	seedConnectedUser(t, ctx, ti.conn, orgID, "local_hana", "hana@test.com", "Hana", "user_hana", "membership_hana")
	seedGrantWithSelector(t, ctx, ti.conn, orgID, urn.NewPrincipal(urn.PrincipalTypeUser, "local_hana"), authz.ScopeMCPConnect, everyTool)

	use := explainedLevel(t, explainAccess(t, ctx, ti, serverID, "local_hana"), audienceLevelUse)
	require.True(t, use.Allowed)
	require.Equal(t, explainedToolAccessAll, *use.ToolAccess)
}
