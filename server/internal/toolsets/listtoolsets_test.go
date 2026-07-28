package toolsets_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestToolsetsService_ListToolsets_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create deployment with petstore fixture
	dep := createPetstoreDeployment(t, ctx, ti)

	// Get tools from the deployment
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment tools")
	require.GreaterOrEqual(t, len(tools), 4, "expected at least 4 tools from petstore")

	// Create a few toolsets
	toolset1, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "First Toolset",
		Description:            new("First test toolset"),
		ToolUrns:               []string{tools[0].ToolUrn.String(), tools[1].ToolUrn.String()},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	toolset2, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Second Toolset",
		Description:            new("Second test toolset"),
		ToolUrns:               []string{tools[2].ToolUrn.String(), tools[3].ToolUrn.String()},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	beforeCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)

	// List toolsets
	result, err := ti.service.ListToolsets(ctx, &gen.ListToolsetsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Toolsets, 2)

	// Check that both toolsets are present and have HTTP tools populated
	toolsetIDs := make(map[string]bool)
	for _, ts := range result.Toolsets {
		toolsetIDs[ts.ID] = true
		require.NotEmpty(t, ts.Tools, "HTTP tools should be populated")
		require.Len(t, ts.Tools, 2, "each toolset should have 2 tools")
	}
	require.True(t, toolsetIDs[toolset1.ID])
	require.True(t, toolsetIDs[toolset2.ID])
	afterCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestToolsetsService_ListToolsets_IncludesOrigin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	_, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Origin Toolset",
		Description:            new("A toolset with origin"),
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		Origin: &types.ToolsetOrigin{
			RegistrySpecifier: "com.speakeasy.example/server",
		},
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	result, err := ti.service.ListToolsets(ctx, &gen.ListToolsetsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Toolsets, 1)
	require.NotNil(t, result.Toolsets[0].Origin)
	require.Equal(
		t,
		"com.speakeasy.example/server",
		result.Toolsets[0].Origin.RegistrySpecifier,
	)
}

func TestToolsetsService_ListToolsets_EmptyList(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	beforeCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)

	// List toolsets when none exist
	result, err := ti.service.ListToolsets(ctx, &gen.ListToolsetsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.Toolsets)
	afterCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestToolsetsService_ListToolsets_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestToolsetsService(t)
	beforeCount, err := audittest.AuditLogCount(t.Context(), ti.conn)
	require.NoError(t, err)

	// Test with context that has no auth context
	ctx := t.Context()

	_, err = ti.service.ListToolsets(ctx, &gen.ListToolsetsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
	afterCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestToolsetsService_ListToolsets_NoProjectID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	beforeCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)

	// Create auth context without project ID
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.ProjectID = nil
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	_, err = ti.service.ListToolsets(ctx, &gen.ListToolsetsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
	afterCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestToolsetsService_ListToolsets_VerifyDetails(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create a toolset with specific details
	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Detailed Toolset",
		Description:            new("A toolset with details"),
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	beforeCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)

	// List toolsets and verify details
	result, err := ti.service.ListToolsets(ctx, &gen.ListToolsetsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Toolsets, 1)

	toolset := result.Toolsets[0]
	require.Equal(t, created.ID, toolset.ID)
	require.Equal(t, "Detailed Toolset", toolset.Name)
	require.Equal(t, "detailed-toolset", string(toolset.Slug))
	require.Equal(t, "A toolset with details", *toolset.Description)
	require.Empty(t, toolset.Tools)
	require.Nil(t, toolset.SecurityVariables, "toolsets with no tool URNs should have nil SecurityVariables, not an empty slice")
	require.Nil(t, toolset.ServerVariables, "toolsets with no tool URNs should have nil ServerVariables, not an empty slice")
	require.NotNil(t, toolset.CreatedAt)
	require.NotNil(t, toolset.UpdatedAt)
	afterCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestToolsetsService_ListToolsets_WithResources(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create deployment with functions that include resources
	dep := createFunctionsDeploymentWithResources(t, ctx, ti)

	// Get resources from the deployment
	repo := testrepo.New(ti.conn)
	resources, err := repo.ListDeploymentFunctionsResources(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment resources")
	require.Len(t, resources, 3, "expected 3 resources from manifest")

	// Create toolset with resources
	resourceUrns := make([]string, len(resources))
	for i, r := range resources {
		resourceUrns[i] = r.ResourceUrn.String()
	}

	toolset, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Toolset With Resources",
		Description:            new("A toolset that includes resources"),
		ToolUrns:               []string{},
		ResourceUrns:           resourceUrns,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	beforeCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)

	// List toolsets and verify resources are populated
	result, err := ti.service.ListToolsets(ctx, &gen.ListToolsetsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Toolsets, 1)

	ts := result.Toolsets[0]
	require.Equal(t, toolset.ID, ts.ID)
	require.Len(t, ts.Resources, 3, "resources should be populated")
	require.Len(t, ts.ResourceUrns, 3, "resource URNs should be populated")

	// Verify resource names match what we expect from the manifest
	resourceNames := make(map[string]bool)
	for _, r := range ts.Resources {
		resourceNames[r.Name] = true
	}
	require.True(t, resourceNames["user_guide"], "user_guide resource should be present")
	require.True(t, resourceNames["api_reference"], "api_reference resource should be present")
	require.True(t, resourceNames["data_source"], "data_source resource should be present")
	afterCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestToolsetsService_ListToolsets_MixedToolsAndResources(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create deployment with OpenAPI (for tools)
	petstoreDep := createPetstoreDeployment(t, ctx, ti)

	// Create deployment with functions that include resources
	resourceDep := createFunctionsDeploymentWithResources(t, ctx, ti)

	// Get tools from petstore
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(petstoreDep.Deployment.ID))
	require.NoError(t, err, "list deployment tools")
	require.GreaterOrEqual(t, len(tools), 2, "expected at least 2 tools from petstore")

	// Get resources from functions deployment
	resources, err := repo.ListDeploymentFunctionsResources(ctx, uuid.MustParse(resourceDep.Deployment.ID))
	require.NoError(t, err, "list deployment resources")
	require.Len(t, resources, 3, "expected 3 resources from manifest")

	// Create toolset with both tools and resources
	toolUrns := []string{tools[0].ToolUrn.String(), tools[1].ToolUrn.String()}
	resourceUrns := make([]string, len(resources))
	for i, r := range resources {
		resourceUrns[i] = r.ResourceUrn.String()
	}

	toolset, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Mixed Toolset",
		Description:            new("A toolset with both tools and resources"),
		ToolUrns:               toolUrns,
		ResourceUrns:           resourceUrns,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	beforeCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)

	// List toolsets and verify both tools and resources are populated
	result, err := ti.service.ListToolsets(ctx, &gen.ListToolsetsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Toolsets, 1)

	ts := result.Toolsets[0]
	require.Equal(t, toolset.ID, ts.ID)
	require.Len(t, ts.ToolUrns, 2, "tool URNs should be populated")
	require.Len(t, ts.Resources, 3, "resources should be populated")
	require.Len(t, ts.ResourceUrns, 3, "resource URNs should be populated")

	// Note: Tools field may or may not be populated depending on tool type
	// The important check is that ToolUrns are present
	afterCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestToolsetsService_ListToolsets_MultipleToolsetsWithResources(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create deployment with functions that include resources
	dep := createFunctionsDeploymentWithResources(t, ctx, ti)

	// Get resources from the deployment
	repo := testrepo.New(ti.conn)
	resources, err := repo.ListDeploymentFunctionsResources(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment resources")
	require.Len(t, resources, 3, "expected 3 resources from manifest")

	// Create first toolset with first 2 resources
	toolset1, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "First Resource Toolset",
		Description:            new("First toolset with resources"),
		ToolUrns:               []string{},
		ResourceUrns:           []string{resources[0].ResourceUrn.String(), resources[1].ResourceUrn.String()},
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// Create second toolset with last resource
	toolset2, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Second Resource Toolset",
		Description:            new("Second toolset with resources"),
		ToolUrns:               []string{},
		ResourceUrns:           []string{resources[2].ResourceUrn.String()},
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	beforeCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)

	// List toolsets
	result, err := ti.service.ListToolsets(ctx, &gen.ListToolsetsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Toolsets, 2)

	// Verify both toolsets have resources populated correctly
	toolsetMap := make(map[string]*types.ToolsetEntry)
	for _, ts := range result.Toolsets {
		toolsetMap[ts.ID] = ts
	}

	ts1 := toolsetMap[toolset1.ID]
	require.NotNil(t, ts1)
	require.Len(t, ts1.Resources, 2, "first toolset should have 2 resources")
	require.Len(t, ts1.ResourceUrns, 2, "first toolset should have 2 resource URNs")

	ts2 := toolsetMap[toolset2.ID]
	require.NotNil(t, ts2)
	require.Len(t, ts2.Resources, 1, "second toolset should have 1 resource")
	require.Len(t, ts2.ResourceUrns, 1, "second toolset should have 1 resource URN")
	afterCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestToolsetsService_ListToolsets_ManyToolsetsNoCrossContamination(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	dep := createPetstoreDeployment(t, ctx, ti)
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment tools")
	require.GreaterOrEqual(t, len(tools), 4, "expected at least 4 tools from petstore")

	// 4 toolsets, each with a disjoint single tool — if the batch rewrite
	// leaks results across toolsets, one of these will end up with the
	// wrong tool or an extra one.
	created := make([]*types.Toolset, 0, 4)
	for i := range 4 {
		ts, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
			SessionToken:           nil,
			Name:                   fmt.Sprintf("Toolset %d", i),
			Description:            new(fmt.Sprintf("Toolset %d description", i)),
			ToolUrns:               []string{tools[i].ToolUrn.String()},
			ResourceUrns:           nil,
			DefaultEnvironmentSlug: nil,
			ProjectSlugInput:       nil,
		})
		require.NoError(t, err)
		created = append(created, ts)
	}

	result, err := ti.service.ListToolsets(ctx, &gen.ListToolsetsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Toolsets, 4)

	byID := make(map[string]*types.ToolsetEntry, len(result.Toolsets))
	for _, ts := range result.Toolsets {
		byID[ts.ID] = ts
	}

	for i, ts := range created {
		entry := byID[ts.ID]
		require.NotNil(t, entry, "toolset %d missing from result", i)
		require.Len(t, entry.Tools, 1, "toolset %d should have exactly 1 tool", i)
		require.Equal(t, tools[i].ToolUrn.String(), entry.Tools[0].ToolUrn, "toolset %d has the wrong tool", i)
		require.Len(t, entry.ToolUrns, 1)
		require.Equal(t, tools[i].ToolUrn.String(), entry.ToolUrns[0])
	}
}

// TestToolsetsService_ListToolsets_SecurityVariablesNotMixedAcrossDocuments is
// a regression test for DNO-375: http_security rows are only unique per
// (deployment, OpenAPI document, key), not per bare key. todo-valid.yaml
// declares an "ApiKeyAuth"/"BearerAuth" pair, so attaching that same fixture
// twice under different document slugs within ONE deployment produces two
// http_security rows that share the same bare keys but resolve to
// different, document-specific env vars (the env var name is derived from
// the document's slug, see internal/openapi/extract_speakeasy.go). Both
// documents must live in the same deployment because
// FindHttpToolEntriesByUrn (used by DescribeToolsetEntries) only ever
// resolves tools from a project's single latest completed deployment. If the
// batched lookup in mv.fetchHTTPSecurityDefinitions ever regresses to keying
// by bare key again, one of these toolsets would end up advertising the
// other document's environment variable.
func TestToolsetsService_ListToolsets_SecurityVariablesNotMixedAcrossDocuments(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	dep := createTodoDeploymentWithDocs(t, ctx, ti, "test-todo-multi-doc", "todo-doc-a", "todo-doc-b")

	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment tools")
	require.NotEmpty(t, tools, "expected tools from todo deployment")

	var toolUrnsA, toolUrnsB []string
	for _, tool := range tools {
		urn := tool.ToolUrn.String()
		switch {
		case strings.Contains(urn, ":todo-doc-a:"):
			toolUrnsA = append(toolUrnsA, urn)
		case strings.Contains(urn, ":todo-doc-b:"):
			toolUrnsB = append(toolUrnsB, urn)
		}
	}
	require.NotEmpty(t, toolUrnsA, "expected tools from todo-doc-a")
	require.NotEmpty(t, toolUrnsB, "expected tools from todo-doc-b")

	toolsetA, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Todo Doc A Toolset",
		Description:            new("Toolset backed by todo document A"),
		ToolUrns:               toolUrnsA,
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	toolsetB, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Todo Doc B Toolset",
		Description:            new("Toolset backed by todo document B"),
		ToolUrns:               toolUrnsB,
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	result, err := ti.service.ListToolsets(ctx, &gen.ListToolsetsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Toolsets, 2)

	byID := make(map[string]*types.ToolsetEntry, len(result.Toolsets))
	for _, ts := range result.Toolsets {
		byID[ts.ID] = ts
	}

	entryA := byID[toolsetA.ID]
	require.NotNil(t, entryA)
	entryB := byID[toolsetB.ID]
	require.NotNil(t, entryB)

	envVarsA := collectSecurityEnvVars(entryA.SecurityVariables)
	envVarsB := collectSecurityEnvVars(entryB.SecurityVariables)

	require.Contains(t, envVarsA, "TODO_DOC_A_API_KEY_AUTH", "toolset A should surface its own document's api key env var")
	require.Contains(t, envVarsA, "TODO_DOC_A_BEARER_AUTH", "toolset A should surface its own document's bearer env var")
	require.NotContains(t, envVarsA, "TODO_DOC_B_API_KEY_AUTH", "toolset A must not surface document B's env var")
	require.NotContains(t, envVarsA, "TODO_DOC_B_BEARER_AUTH", "toolset A must not surface document B's env var")

	require.Contains(t, envVarsB, "TODO_DOC_B_API_KEY_AUTH", "toolset B should surface its own document's api key env var")
	require.Contains(t, envVarsB, "TODO_DOC_B_BEARER_AUTH", "toolset B should surface its own document's bearer env var")
	require.NotContains(t, envVarsB, "TODO_DOC_A_API_KEY_AUTH", "toolset B must not surface document A's env var")
	require.NotContains(t, envVarsB, "TODO_DOC_A_BEARER_AUTH", "toolset B must not surface document A's env var")
}

func TestToolsetsService_ListToolsets_FunctionEnvVarsPreserveDefinitionOrder(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	dep := createFunctionsDeployment(t, ctx, ti)

	repo := testrepo.New(ti.conn)
	functionTools, err := repo.ListDeploymentFunctionsTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment function tools")
	require.GreaterOrEqual(t, len(functionTools), 2, "expected at least two function tools")

	newer, older := functionTools[0], functionTools[1]
	if bytes.Compare(newer.ID[:], older.ID[:]) < 0 {
		newer, older = older, newer
	}

	err = repo.SetFunctionToolVariables(ctx, testrepo.SetFunctionToolVariablesParams{
		Variables: []byte(`{"SHARED_API_KEY":{"description":"newer definition"}}`),
		ID:        newer.ID,
		ProjectID: newer.ProjectID,
	})
	require.NoError(t, err, "set newer function environment variable")

	err = repo.SetFunctionToolVariables(ctx, testrepo.SetFunctionToolVariablesParams{
		Variables: []byte(`{"SHARED_API_KEY":{"description":"older definition"}}`),
		ID:        older.ID,
		ProjectID: older.ProjectID,
	})
	require.NoError(t, err, "set older function environment variable")

	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Function Environment Variable Order",
		Description:            nil,
		ToolUrns:               []string{older.ToolUrn.String(), newer.ToolUrn.String()},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	result, err := ti.service.ListToolsets(ctx, &gen.ListToolsetsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Toolsets, 1)
	require.Equal(t, created.ID, result.Toolsets[0].ID)

	var sharedVariable *types.FunctionEnvironmentVariable
	for _, variable := range result.Toolsets[0].FunctionEnvironmentVariables {
		if variable.Name == "SHARED_API_KEY" {
			sharedVariable = variable
			break
		}
	}
	require.NotNil(t, sharedVariable)
	require.NotNil(t, sharedVariable.Description)
	require.Equal(t, "newer definition", *sharedVariable.Description)
}

// TestToolsetsService_ListToolsets_DanglingToolURN covers the empty-slice-vs-nil
// contract for Tools: a toolset with a tool URN that doesn't resolve to any
// known tool (e.g. stale after a deployment change) must still get a non-nil,
// empty Tools slice — matching the old DescribeToolsetEntry's contract, where
// Tools was only ever nil when the toolset had zero tool URNs at all.
func TestToolsetsService_ListToolsets_DanglingToolURN(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	danglingURN := urn.NewTool(urn.ToolKindHTTP, "does-not-exist", "does-not-exist").String()

	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Dangling URN Toolset",
		Description:            new("toolset with an unresolvable tool URN"),
		ToolUrns:               []string{danglingURN},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	result, err := ti.service.ListToolsets(ctx, &gen.ListToolsetsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Toolsets, 1)

	entry := result.Toolsets[0]
	require.Equal(t, created.ID, entry.ID)
	require.Len(t, entry.ToolUrns, 1, "the dangling URN is still recorded on the toolset")
	require.NotNil(t, entry.Tools, "Tools must be [] (not null) when the toolset has tool URNs, even if none resolve")
	require.Empty(t, entry.Tools)
}

func collectSecurityEnvVars(vars []*types.SecurityVariable) []string {
	var out []string
	for _, v := range vars {
		if v == nil {
			continue
		}
		out = append(out, v.EnvVariables...)
	}
	return out
}
