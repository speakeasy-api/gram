package mcpmetadata_test

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	deployments_repo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	remotemcp_repo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	tools_repo "github.com/speakeasy-api/gram/server/internal/tools/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	variations_repo "github.com/speakeasy-api/gram/server/internal/variations/repo"
)

// createPublicToolsetWithTools creates a completed deployment with the named
// HTTP tools and a public, MCP-enabled toolset (slug == mcp slug) containing
// them. It returns the toolset slug and the tool URNs in the order of names.
func createPublicToolsetWithTools(t *testing.T, ctx context.Context, ti *testInstance, slug string, toolNames []string) (string, []urn.Tool) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset, err := ti.toolsetRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   slug,
		Slug:                   slug,
		McpSlug:                conv.ToPGText(slug),
		Description:            conv.ToPGText("scopes test toolset"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpEnabled:             true,
	})
	require.NoError(t, err)

	err = toolsets_repo.New(ti.conn).SetToolsetMCPPublicByID(ctx, toolsets_repo.SetToolsetMCPPublicByIDParams{
		McpIsPublic: true,
		ID:          toolset.ID,
		ProjectID:   toolset.ProjectID,
	})
	require.NoError(t, err)

	deploymentID, err := deployments_repo.New(ti.conn).InsertDeployment(ctx, deployments_repo.InsertDeploymentParams{
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         "test-user",
		IdempotencyKey: uuid.New().String(),
	})
	require.NoError(t, err)

	err = deployments_repo.New(ti.conn).CreateDeploymentStatus(ctx, deployments_repo.CreateDeploymentStatusParams{
		DeploymentID: deploymentID,
		Status:       "completed",
	})
	require.NoError(t, err)

	toolURNs := make([]urn.Tool, 0, len(toolNames))
	for _, name := range toolNames {
		toolURN := urn.NewTool(urn.ToolKindHTTP, "scopes-api", uuid.New().String()[:8])
		err = tools_repo.New(ti.conn).CreateHTTPToolDefinition(ctx, tools_repo.CreateHTTPToolDefinitionParams{
			ProjectID:       *authCtx.ProjectID,
			DeploymentID:    deploymentID,
			ToolUrn:         toolURN,
			Name:            name,
			UntruncatedName: pgtype.Text{String: "", Valid: false},
			Summary:         name,
			Description:     "Description for " + name,
			Tags:            []string{},
			HttpMethod:      "GET",
			Path:            "/api/" + name,
			SchemaVersion:   "3.0.0",
			Schema:          []byte(`{}`),
			ServerEnvVar:    "TEST_SERVER_URL",
			Security:        []byte(`[]`),
			HeaderSettings:  []byte(`{}`),
			QuerySettings:   []byte(`{}`),
			PathSettings:    []byte(`{}`),
			ReadOnlyHint:    pgtype.Bool{Bool: false, Valid: false},
			IdempotentHint:  pgtype.Bool{Bool: false, Valid: false},
			DestructiveHint: pgtype.Bool{Bool: false, Valid: false},
			OpenWorldHint:   pgtype.Bool{Bool: false, Valid: false},
		})
		require.NoError(t, err)
		toolURNs = append(toolURNs, toolURN)
	}

	_, err = toolsets_repo.New(ti.conn).CreateToolsetVersion(ctx, toolsets_repo.CreateToolsetVersionParams{
		ToolsetID:     toolset.ID,
		Version:       1,
		ToolUrns:      toolURNs,
		ResourceUrns:  []urn.Resource{},
		PredecessorID: uuid.NullUUID{},
	})
	require.NoError(t, err)

	return slug, toolURNs
}

// seedScopesToolVariationsGroup creates the project-default tool variations
// group through the generated repo.
func seedScopesToolVariationsGroup(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID) uuid.UUID {
	t.Helper()

	groupID, err := variations_repo.New(ti.conn).InitGlobalToolVariationsGroup(ctx, variations_repo.InitGlobalToolVariationsGroupParams{
		ProjectID:   projectID,
		Name:        "Global tool variations",
		Description: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	return groupID
}

// seedScopesTaggedVariation upserts a variation carrying the given tags (and an
// optional rename) for a tool URN into the group.
func seedScopesTaggedVariation(t *testing.T, ctx context.Context, ti *testInstance, groupID uuid.UUID, toolURN urn.Tool, srcName, nameOverride string, tags []string) {
	t.Helper()

	_, err := variations_repo.New(ti.conn).UpsertToolVariation(ctx, variations_repo.UpsertToolVariationParams{
		GroupID:         groupID,
		SrcToolUrn:      toolURN,
		SrcToolName:     srcName,
		Confirm:         pgtype.Text{String: "", Valid: false},
		ConfirmPrompt:   pgtype.Text{String: "", Valid: false},
		Name:            pgtype.Text{String: nameOverride, Valid: nameOverride != ""},
		Summary:         pgtype.Text{String: "", Valid: false},
		Description:     pgtype.Text{String: "", Valid: false},
		Tags:            tags,
		Summarizer:      pgtype.Text{String: "", Valid: false},
		Title:           pgtype.Text{String: "", Valid: false},
		ReadOnlyHint:    pgtype.Bool{Bool: false, Valid: false},
		DestructiveHint: pgtype.Bool{Bool: false, Valid: false},
		IdempotentHint:  pgtype.Bool{Bool: false, Valid: false},
		OpenWorldHint:   pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)
}

func assignToolsetVariationsGroup(t *testing.T, ctx context.Context, ti *testInstance, slug string, projectID, groupID uuid.UUID) {
	t.Helper()

	_, err := toolsets_repo.New(ti.conn).UpdateToolsetToolVariationsGroup(ctx, toolsets_repo.UpdateToolsetToolVariationsGroupParams{
		ToolVariationsGroupID: uuid.NullUUID{UUID: groupID, Valid: true},
		Slug:                  slug,
		ProjectID:             projectID,
	})
	require.NoError(t, err)
}

func serveInstallPageBody(t *testing.T, ctx context.Context, ti *testInstance, mcpSlug string) string {
	t.Helper()

	req := httptest.NewRequest("GET", "/mcp/"+mcpSlug+"/install", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	require.NoError(t, ti.service.ServeInstallPage(rr, req))
	require.Equal(t, http.StatusOK, rr.Code)

	return rr.Body.String()
}

// extractScopeVariants pulls the JSON map out of the install page's
// scope-variants data-* attribute (HTML-escaped) and unmarshals it.
func extractScopeVariants(t *testing.T, body string) map[string]map[string]string {
	t.Helper()

	marker := `data-variants="`
	start := strings.Index(body, marker)
	require.GreaterOrEqual(t, start, 0, "scope-variants attribute must be present")
	start += len(marker)
	end := strings.Index(body[start:], `"`)
	require.GreaterOrEqual(t, end, 0)

	raw := html.UnescapeString(body[start : start+end])

	var variants map[string]map[string]string
	require.NoError(t, json.Unmarshal([]byte(raw), &variants))
	return variants
}

// TestServeInstallPage_Scopes_NoGroup_RendersUnchanged verifies that a toolset
// with no assigned variations group renders without any scope UI (regression
// guard for the existing single-list page).
func TestServeInstallPage_Scopes_NoGroup_RendersUnchanged(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPMetadataService(t)

	slug, _ := createPublicToolsetWithTools(t, ctx, ti, "scopes-no-group", []string{"search_records"})

	body := serveInstallPageBody(t, ctx, ti, slug)

	require.Contains(t, body, "search_records", "tools should still render")
	require.NotContains(t, body, `class="scopes container"`, "no scope panel without a group")
	require.NotContains(t, body, `id="scope-variants"`, "no scope variants without a group")
	// "scope-chip" also appears in the always-present CSS, so assert on the chip
	// markup instead.
	require.NotContains(t, body, "data-scope-tag", "no scope chips without a group")
}

// TestServeInstallPage_Scopes_WithGroup verifies that a toolset assigned a
// variations group with tags renders the scope chips, group name, per-tool tag
// attributes, variation renames, and a scope-variants map whose URLs carry the
// ?tags= filter.
func TestServeInstallPage_Scopes_WithGroup(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPMetadataService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug, toolURNs := createPublicToolsetWithTools(t, ctx, ti, "scopes-with-group", []string{"search_records", "delete_record"})

	groupID := seedScopesToolVariationsGroup(t, ctx, ti, *authCtx.ProjectID)
	// First tool: renamed, tagged read+admin. Second tool: explicit empty tag
	// set, so it is excluded from every scope.
	seedScopesTaggedVariation(t, ctx, ti, groupID, toolURNs[0], "search_records", "Renamed Search", []string{"read", "admin"})
	seedScopesTaggedVariation(t, ctx, ti, groupID, toolURNs[1], "delete_record", "", []string{})
	assignToolsetVariationsGroup(t, ctx, ti, slug, *authCtx.ProjectID, groupID)

	body := serveInstallPageBody(t, ctx, ti, slug)

	require.Contains(t, body, `class="scopes container"`, "scope panel should render")
	require.Contains(t, body, `>All tools<`, "an unfiltered chip should render")
	require.Contains(t, body, `data-scope-tag="read"`, "read scope chip should render")
	require.Contains(t, body, `data-scope-tag="admin"`, "admin scope chip should render")
	require.Contains(t, body, `data-tool-tags="read,admin"`, "tool rows should carry effective tags")
	require.Contains(t, body, "Renamed Search", "variation rename should be reflected")

	variants := extractScopeVariants(t, body)
	require.Contains(t, variants, "", "unfiltered default variant must exist")
	require.Contains(t, variants, "read")
	require.Contains(t, variants, "admin")
	require.NotContains(t, variants[""]["url"], "tags=", "default URL is unfiltered")
	require.Contains(t, variants["read"]["url"], "tags=read", "scope URL carries the ?tags= filter")
	require.Contains(t, variants["admin"]["url"], "tags=admin")
	require.NotEmpty(t, variants["read"]["config"], "config snippet should be built per scope")
	require.NotEmpty(t, variants["read"]["cursor"], "cursor deep link should be built per scope")
	require.NotEmpty(t, variants["read"]["vscode"], "vscode deep link should be built per scope")
	require.Contains(t, variants["read"]["config"], "tags=read", "config snippet URL carries the filter")
}

// TestServeInstallPage_Scopes_EmptyGroup_FallsBack verifies that assigning a
// group that yields no tags (no tagged variations) falls back to the unfiltered
// view with no scope UI.
func TestServeInstallPage_Scopes_EmptyGroup_FallsBack(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPMetadataService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug, _ := createPublicToolsetWithTools(t, ctx, ti, "scopes-empty-group", []string{"search_records"})

	groupID := seedScopesToolVariationsGroup(t, ctx, ti, *authCtx.ProjectID)
	assignToolsetVariationsGroup(t, ctx, ti, slug, *authCtx.ProjectID, groupID)

	body := serveInstallPageBody(t, ctx, ti, slug)

	require.Contains(t, body, "search_records", "tools should still render")
	require.NotContains(t, body, `class="scopes container"`, "empty group must not render a scope panel")
	require.NotContains(t, body, `id="scope-variants"`, "empty group must not emit scope variants")
}

// TestServeInstallPage_Scopes_RemoteBacked_NoScopes verifies that a
// Remote-MCP-backed install (no toolset, no tools) renders no scope UI.
func TestServeInstallPage_Scopes_RemoteBacked_NoScopes(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPMetadataService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	remoteServer := remotemcptest.SeedServer(t, ctx, ti.conn, remotemcp_repo.CreateServerParams{
		ProjectID:     *authCtx.ProjectID,
		TransportType: "streamable-http",
		Url:           "https://upstream.example.com/mcp",
	})

	endpointSlug := "scopes-remote-" + uuid.NewString()[:8]
	createMcpServerWithEndpoint(t, ctx, ti, mcpServerFixtureOptions{
		name:              "Remote MCP Scopes",
		visibility:        mcpservers.VisibilityPublic,
		endpointSlug:      endpointSlug,
		remoteMcpServerID: uuid.NullUUID{UUID: remoteServer.ID, Valid: true},
	})

	body := serveInstallPageBody(t, ctx, ti, endpointSlug)

	require.NotContains(t, body, `class="scopes container"`, "remote-backed installs have no scope panel")
	require.NotContains(t, body, `id="scope-variants"`, "remote-backed installs emit no scope variants")
}
