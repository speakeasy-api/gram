package platformmcp

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	accessgen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

func TestAccessReadOutputsOnlyAllowlistedFields(t *testing.T) {
	t.Parallel()

	roles := ListAccessRolesOutput{Roles: []AccessRole{{
		Name: "Operators", Type: "custom", MemberCount: NewSubjectCount(7),
		MCPAccess: MCPConnectSummary{AllServers: false, ProjectRules: 1, ServerRules: 1, ToolRules: 1, DispositionRules: []string{"read_only"}, BlockedServers: false, BlockedProjectRules: 1, BlockedServerRules: 1, BlockedToolRules: 0, BlockedDispositionRules: []string{}},
		Reference: "opaque-role",
	}}, ExpiresAt: "2026-09-04T12:10:00Z"}
	require.ElementsMatch(t, []string{"roles", "name", "type", "member_count", "mcp_access", "all_servers", "project_rules", "server_rules", "tool_rules", "disposition_rules", "blocked_servers", "blocked_project_rules", "blocked_server_rules", "blocked_tool_rules", "blocked_disposition_rules", "reference", "expires_at"}, decodeKeys(t, roles))

	members := ListAccessMembersOutput{
		Members:      []AccessMember{{MaskedIdentity: "a***@e***", Roles: []string{"Operators"}, Reference: "opaque-member"}},
		TotalMatches: NewSubjectCount(7), Suppressed: false, Truncated: false, ExpiresAt: "2026-09-04T12:10:00Z",
	}
	require.ElementsMatch(t, []string{"members", "masked_identity", "roles", "reference", "total_matches", "suppressed", "truncated", "expires_at"}, decodeKeys(t, members))

	access := GetMCPAccessOutput{
		ProjectID: uuid.NewString(),
		MCP:       MCPAccessTarget{ID: uuid.NewString(), Name: "Tasks", Backend: "remote", Visibility: "private", AuthorizationMode: "rbac", AuthorizationSurface: "configured_endpoint", AccessSummary: "by_role", ToolCatalog: "stored_metadata", Tools: []MCPAccessTool{{Name: "list_tasks", Disposition: "read_only"}}},
		Roles:     []MCPRoleCoverage{{Name: "Operators", Type: "custom", MemberCount: NewSubjectCount(7), Reference: "opaque-role", CanEnterServer: true, KnownToolAccess: "all", AllowedKnownTools: []string{"list_tasks"}, DispositionRules: []string{"read_only"}, BlockedDispositions: []string{}, UnevaluatedGrants: false}},
		ExpiresAt: "2026-09-04T12:10:00Z",
	}
	keys := decodeKeys(t, access)
	require.ElementsMatch(t, []string{
		"project_id", "mcp", "id", "name", "backend", "visibility", "authorization_mode", "authorization_surface", "access_summary", "tool_catalog", "tools", "name", "tools_truncated", "disposition",
		"roles", "name", "type", "member_count", "reference", "can_enter_server", "known_tool_access", "allowed_known_tools", "disposition_rules", "blocked_dispositions", "unevaluated_grants", "expires_at",
	}, keys)
	encoded, err := json.Marshal(access)
	require.NoError(t, err)
	for _, forbidden := range []string{"principal_urn", "user_id", "role_id", "email", "selector", "resource_id"} {
		require.NotContains(t, string(encoded), forbidden)
	}
}

func TestAccessRoleProjectionSummarizesWithoutSelectors(t *testing.T) {
	t.Parallel()

	readOnly := "read_only"
	project := uuid.NewString()
	server := uuid.NewString()
	summary := summarizeMCPConnect([]*accessgen.RoleGrant{
		{Scope: string(authz.ScopeMCPConnect), Selectors: []*accessgen.Selector{{ResourceKind: "mcp", ResourceID: server, Tool: new("list_tasks")}}},
		{Scope: string(authz.ScopeMCPRead), Selectors: []*accessgen.Selector{{ResourceKind: "mcp", ResourceID: authz.WildcardResource, ProjectID: &project, Disposition: &readOnly}}},
	})
	require.False(t, summary.AllServers)
	require.Equal(t, 1, summary.ProjectRules)
	require.Equal(t, 1, summary.ServerRules)
	require.Equal(t, 1, summary.ToolRules)
	require.Equal(t, []string{"read_only"}, summary.DispositionRules)

	blocked := "destructive"
	summary = summarizeMCPConnect([]*accessgen.RoleGrant{{Scope: string(authz.ScopeMCPBlockedConnect), Selectors: []*accessgen.Selector{{ResourceKind: "mcp", ResourceID: authz.WildcardResource, Tool: new("delete_tasks"), Disposition: &blocked}}}})
	require.False(t, summary.BlockedServers)
	require.Equal(t, 1, summary.BlockedToolRules)
	require.Equal(t, []string{"destructive"}, summary.BlockedDispositionRules)

	project = uuid.NewString()
	summary = summarizeMCPConnect([]*accessgen.RoleGrant{{Scope: string(authz.ScopeMCPBlockedConnect), Selectors: []*accessgen.Selector{{ResourceKind: "mcp", ResourceID: server, ProjectID: &project}}}})
	require.Equal(t, 1, summary.BlockedProjectRules)
	require.Equal(t, 1, summary.BlockedServerRules)
}

func TestRoleAuthzGrantsUseEffectiveExclusions(t *testing.T) {
	t.Parallel()

	server := uuid.NewString()
	tool := "delete_tasks"
	role := &accessgen.Role{PrincipalUrn: "role:organization:" + uuid.NewString(), Grants: []*accessgen.RoleGrant{
		{Scope: string(authz.ScopeMCPConnect), Selectors: []*accessgen.Selector{{ResourceKind: "mcp", ResourceID: server}}},
		{Scope: string(authz.ScopeMCPBlockedConnect), Selectors: []*accessgen.Selector{{ResourceKind: "mcp", ResourceID: server, Tool: &tool}}},
	}}
	grants, unevaluated := roleAuthzGrants(role)
	require.False(t, unevaluated)

	serverAllowed, err := authz.GrantsAuthorize(grants, authz.MCPCheck(authz.ScopeMCPConnect, server, uuid.NewString()))
	require.NoError(t, err)
	require.True(t, serverAllowed, "a tool-specific exclusion does not block the server-level check")

	toolAllowed, err := authz.GrantsAuthorize(grants, authz.MCPToolCallCheck(server, authz.MCPToolCallDimensions{Tool: tool}))
	require.NoError(t, err)
	require.False(t, toolAllowed)
}

func TestAccessSummaryAndKnownToolCoverageCannotOverclaim(t *testing.T) {
	t.Parallel()

	require.Equal(t, "everyone", accessSummary(platformrepo.GetPlatformMCPInventoryItemRow{Visibility: "public"}))
	require.Equal(t, "nobody", accessSummary(platformrepo.GetPlatformMCPInventoryItemRow{Visibility: "disabled"}))
	require.Equal(t, "nobody", accessSummary(platformrepo.GetPlatformMCPInventoryItemRow{Visibility: "private", UnproxiedMcpServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}))
	require.Equal(t, "nobody", accessSummary(platformrepo.GetPlatformMCPInventoryItemRow{Visibility: "public", UnproxiedMcpServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}))
	require.Equal(t, "nobody", accessSummary(platformrepo.GetPlatformMCPInventoryItemRow{Visibility: "future_value"}))
	require.Equal(t, "by_role", accessSummary(platformrepo.GetPlatformMCPInventoryItemRow{Visibility: "private"}))

	require.Equal(t, "all", knownToolAccess(2, 2, "authoritative", false))
	require.Equal(t, "some_known_tools", knownToolAccess(100, 100, "authoritative", true))
	require.Equal(t, "not_enumerable", knownToolAccess(0, 0, "dynamic", false))
}

func TestAmbiguousAccessToolsAreOmitted(t *testing.T) {
	t.Parallel()

	tools := omitAmbiguousAccessTools([]MCPAccessTool{
		{Name: "alpha", Disposition: "read_only"},
		{Name: "shared", Disposition: "destructive"},
		{Name: "shared", Disposition: "read_only"},
		{Name: "zeta", Disposition: ""},
	})
	require.Equal(t, []MCPAccessTool{{Name: "alpha", Disposition: "read_only"}, {Name: "zeta", Disposition: ""}}, tools)
}

func TestRoleAuthzGrantsSkipsLegacyMalformedSelectors(t *testing.T) {
	t.Parallel()

	role := &accessgen.Role{PrincipalUrn: "role:organization:" + uuid.NewString(), Grants: []*accessgen.RoleGrant{{
		Scope: string(authz.ScopeMCPConnect), Selectors: []*accessgen.Selector{{ResourceKind: "project", ResourceID: uuid.NewString()}},
	}}}
	grants, unevaluated := roleAuthzGrants(role)
	require.True(t, unevaluated)
	require.Empty(t, grants)
}

func TestAccessReferencesAreBoundByKindPrincipalAndExpiry(t *testing.T) {
	t.Parallel()

	codec, err := newSubjectReferenceCodec("access-read-test-key")
	require.NoError(t, err)
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	principal := Principal{UserID: "user-a", OrganizationID: "org-a", ConnectionID: "connection-a", Generation: "generation-a"}
	reference, err := codec.Encode(principal, subjectKindAccessMember, "internal-user-id", now)
	require.NoError(t, err)
	require.NotContains(t, reference, "internal-user-id")

	value, err := codec.Decode(reference, principal, subjectKindAccessMember, now)
	require.NoError(t, err)
	require.Equal(t, "internal-user-id", value)
	_, err = codec.Decode(reference, principal, subjectKindAccessRole, now)
	require.ErrorIs(t, err, ErrSubjectReferenceNotFound)
	_, err = codec.Decode(reference, Principal{UserID: "user-b", OrganizationID: "org-a", ConnectionID: "connection-b", Generation: "generation-b"}, subjectKindAccessMember, now)
	require.ErrorIs(t, err, ErrSubjectReferenceNotFound)
	_, err = codec.Decode(reference, principal, subjectKindAccessMember, now.Add(SubjectReferenceTTL))
	require.ErrorIs(t, err, ErrSubjectReferenceNotFound)
}
