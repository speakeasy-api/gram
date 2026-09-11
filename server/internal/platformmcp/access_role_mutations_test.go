package platformmcp

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	accessgen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
)

func TestAccessRoleMutationRequiresConfirmationBeforeServiceValidation(t *testing.T) {
	t.Parallel()

	_, err := (*AccessRoleMutationService)(nil).Create(t.Context(), Principal{}, CreateMCPAccessRoleInput{})
	var mutation *AccessRoleMutationError
	require.ErrorAs(t, err, &mutation)
	require.Equal(t, "confirmation_required", mutation.Code)
}

func TestAccessRoleRuleGrantsAreServerGeneratedAndProjectScoped(t *testing.T) {
	t.Parallel()

	projectID := uuid.New()
	mcpID := uuid.NewString()
	grants := accessRoleRulesToGenGrants([]normalizedMCPAccessRoleRule{{
		MCPID: mcpID, Tool: "list_tasks", Disposition: authz.DispositionReadOnly,
	}}, projectID)
	require.Len(t, grants, 1)
	require.Equal(t, string(authz.ScopeMCPConnect), grants[0].Scope)
	require.Len(t, grants[0].Selectors, 1)
	selector := grants[0].Selectors[0]
	require.Equal(t, authz.ResourceKindMCP, selector.ResourceKind)
	require.Equal(t, mcpID, selector.ResourceID)
	require.Equal(t, projectID.String(), *selector.ProjectID)
	require.Equal(t, "list_tasks", *selector.Tool)
	require.Equal(t, authz.DispositionReadOnly, *selector.Disposition)
}

func TestAccessRoleVersionIsCanonicalAndCoversNonMCPGrants(t *testing.T) {
	t.Parallel()

	service := &AccessRoleMutationService{versionKey: make([]byte, 32)}
	projectID := uuid.NewString()
	mcpID := uuid.NewString()
	project := &accessgen.RoleGrant{Scope: string(authz.ScopeProjectRead), Selectors: []*accessgen.Selector{{ResourceKind: authz.ResourceKindProject, ResourceID: projectID}}}
	mcp := &accessgen.RoleGrant{Scope: string(authz.ScopeMCPConnect), Selectors: []*accessgen.Selector{{ResourceKind: authz.ResourceKindMCP, ResourceID: mcpID, ProjectID: &projectID}}}
	roleID := uuid.NewString()

	first, err := service.roleVersion(&accessgen.Role{ID: roleID, Name: "Operators", Grants: []*accessgen.RoleGrant{project, mcp}})
	require.NoError(t, err)
	second, err := service.roleVersion(&accessgen.Role{ID: roleID, Name: "Operators", Grants: []*accessgen.RoleGrant{mcp, project}})
	require.NoError(t, err)
	require.Equal(t, first, second)

	changed, err := service.roleVersion(&accessgen.Role{ID: roleID, Name: "Operators", Grants: []*accessgen.RoleGrant{mcp}})
	require.NoError(t, err)
	require.NotEqual(t, first, changed, "non-MCP grants must participate in optimistic concurrency")
}

func TestAccessRoleMutationInputHashCanonicalizesRuleOrder(t *testing.T) {
	t.Parallel()

	a := normalizedMCPAccessRoleRule{MCPID: uuid.NewString()}
	b := normalizedMCPAccessRoleRule{MCPID: uuid.NewString(), Disposition: authz.DispositionDestructive}
	ordered := []normalizedMCPAccessRoleRule{a, b}
	reversed := []normalizedMCPAccessRoleRule{b, a}
	if compareNormalizedAccessRoleRule(ordered[0], ordered[1]) > 0 {
		ordered[0], ordered[1] = ordered[1], ordered[0]
	}
	if compareNormalizedAccessRoleRule(reversed[0], reversed[1]) > 0 {
		reversed[0], reversed[1] = reversed[1], reversed[0]
	}

	first, err := accessRoleMutationInputHash(operationCreateMCPAccessRole, normalizedCreateMCPAccessRole{ProjectID: uuid.NewString(), Name: "Operators", Rules: ordered})
	require.NoError(t, err)
	second, err := accessRoleMutationInputHash(operationCreateMCPAccessRole, normalizedCreateMCPAccessRole{ProjectID: uuid.NewString(), Name: "Operators", Rules: reversed})
	require.NoError(t, err)
	require.NotEqual(t, first, second, "the explicit project must bind the input hash")

	projectID := uuid.NewString()
	first, err = accessRoleMutationInputHash(operationCreateMCPAccessRole, normalizedCreateMCPAccessRole{ProjectID: projectID, Name: "Operators", Rules: ordered})
	require.NoError(t, err)
	second, err = accessRoleMutationInputHash(operationCreateMCPAccessRole, normalizedCreateMCPAccessRole{ProjectID: projectID, Name: "Operators", Rules: reversed})
	require.NoError(t, err)
	require.Equal(t, first, second)
}

func TestAccessRoleReceiptRejectsUnknownOrUnsafePayloads(t *testing.T) {
	t.Parallel()

	result := AccessRoleMutationReceiptResult{
		RoleID: uuid.NewString(), RoleSlug: "org-operators", Name: "Operators", Version: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		MCPAccess:      MCPConnectSummary{DispositionRules: []string{}, BlockedDispositionRules: []string{}},
		ResultCategory: "created", Reconciliation: "pending",
	}
	payload, err := encodeAccessRoleMutationReceipt(operationCreateMCPAccessRole, result)
	require.NoError(t, err)
	require.True(t, validAccessRoleMutationReceiptPayload(operationCreateMCPAccessRole, payload))
	require.False(t, validAccessRoleMutationReceiptPayload(operationUpdateMCPAccessRole, payload))

	_, err = encodeAccessRoleMutationReceipt(operationUpdateMCPAccessRole, result)
	require.Error(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(payload, &decoded))
	decoded["principal_urn"] = "role:organization:hidden"
	unsafe, err := json.Marshal(decoded)
	require.NoError(t, err)
	require.False(t, validAccessRoleMutationReceiptPayload(operationCreateMCPAccessRole, unsafe))

	result.Reconciliation = "scheduled"
	require.False(t, validAccessRoleMutationReceiptResult(operationCreateMCPAccessRole, result))

	result.Reconciliation = "pending"
	result.Name = "Support (EU)"
	result.MCPAccess = MCPConnectSummary{ServerRules: maxAccessRoleMutationRules * 4, DispositionRules: []string{"future_annotation"}, BlockedDispositionRules: []string{}}
	require.True(t, validAccessRoleMutationReceiptResult(operationCreateMCPAccessRole, result), "receipt validation must accept legitimate pre-existing dashboard role state")

	result.ResultCategory = "updated"
	require.True(t, validAccessRoleMutationReceiptResult(operationUpdateMCPAccessRole, result))
	require.False(t, validAccessRoleMutationReceiptResult(operationCreateMCPAccessRole, result))
}

func TestAccessRoleRemovalDoesNotRequireCurrentCatalog(t *testing.T) {
	t.Parallel()

	service := &AccessRoleMutationService{}
	mcpID := uuid.NewString()
	rules, err := service.resolveRules(t.Context(), Principal{}, ResolvedProject{}, []MCPAccessRoleRule{{MCPID: mcpID, Tool: "retired_tool"}}, false, make(map[uuid.UUID]accessRoleRuleTarget))
	require.NoError(t, err)
	require.Equal(t, []normalizedMCPAccessRoleRule{{MCPID: mcpID, Tool: "retired_tool", Disposition: ""}}, rules)
}

func TestAccessMemberRoleVersionIsCanonical(t *testing.T) {
	t.Parallel()

	key := make([]byte, 32)
	memberID := "member-1"
	first, err := accessMemberRoleVersion(key, memberID, []string{"role-b", "role-a", "role-a"})
	require.NoError(t, err)
	second, err := accessMemberRoleVersion(key, memberID, []string{"role-a", "role-b"})
	require.NoError(t, err)
	require.Equal(t, first, second)
	changed, err := accessMemberRoleVersion(key, memberID, []string{"role-a"})
	require.NoError(t, err)
	require.NotEqual(t, first, changed)
}

func TestAccessRoleAssignmentReceiptIsClosed(t *testing.T) {
	t.Parallel()

	result := AccessRoleAssignmentReceiptResult{MaskedIdentity: "m***@example.com", Roles: []string{"Operators"}, Version: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", AssignedRole: "Operators", ResultCategory: "assigned", Reconciliation: "pending"}
	payload, err := encodeAccessRoleAssignmentReceipt(result)
	require.NoError(t, err)
	decoded, err := decodeAccessRoleAssignmentReceipt(payload)
	require.NoError(t, err)
	require.Equal(t, result, decoded)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(payload, &raw))
	raw["member_id"] = "private"
	unsafe, err := json.Marshal(raw)
	require.NoError(t, err)
	require.False(t, validAccessRoleAssignmentReceiptPayload(unsafe))
}

func TestAccessRoleMutationOutputsExposeNoRawIdentifiersOrSelectors(t *testing.T) {
	t.Parallel()

	output := CreateMCPAccessRoleOutput{Role: AccessRoleMutationSummary{
		Name: "Operators", Description: "MCP operators", Reference: "opaque-role", Version: "opaque-version",
		MCPAccess: MCPConnectSummary{ServerRules: 1, DispositionRules: []string{}, BlockedDispositionRules: []string{}},
	}, Reconciliation: "pending", Receipt: RiskMutationToolReceipt{ID: uuid.NewString()}}
	payload, err := json.Marshal(output)
	require.NoError(t, err)
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(payload, &raw))
	keys := make([]string, 0, len(raw))
	for key := range raw {
		keys = append(keys, key)
	}
	require.ElementsMatch(t, []string{"role", "reconciliation", "receipt"}, keys)
	var role map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw["role"], &role))
	roleKeys := make([]string, 0, len(role))
	for key := range role {
		roleKeys = append(roleKeys, key)
	}
	require.ElementsMatch(t, []string{"name", "description", "reference", "version", "mcp_access"}, roleKeys)
}
