package platformmcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// slugProjects resolves a fixed set of project slugs the way RegistrationStore
// resolves them from the database: an unknown slug is ErrRegistrationInvalid.
type slugProjects map[string]uuid.UUID

func (p slugProjects) ResolveProject(_ context.Context, _, projectSlug string) (ResolvedProject, error) {
	id, ok := p[projectSlug]
	if !ok {
		return ResolvedProject{}, ErrRegistrationInvalid
	}
	return ResolvedProject{ID: id, Name: projectSlug, Slug: projectSlug}, nil
}

type projectSlugTargetInput struct {
	ProjectSlug string `json:"project_slug,omitempty"`
	ProjectID   string `json:"-"`
}

type projectSlugTargetOutput struct {
	ProjectID   string `json:"project_id"`
	ProjectSlug string `json:"project_slug"`
}

// Every project-scoped tool names its project by slug. A project ID is data a
// tool may return, never an argument it takes.
func TestProjectSlugIsTheOnlyProjectSelector(t *testing.T) {
	t.Parallel()

	// The signals and sensors tools declare their inputs in tool_signals.go,
	// which moves them to project_slug in its own change.
	exempt := map[string]bool{"find_signals": true, "find_sensors": true, "get_sensor": true, "preview_sensor_match": true}
	for _, descriptor := range toolEvalRegistrar(t).Descriptors() {
		if exempt[descriptor.Name] {
			continue
		}
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		require.NoError(t, json.Unmarshal(descriptor.InputSchema, &schema), descriptor.Name)
		require.NotContains(t, schema.Properties, "project_id", "%s must take project_slug, not project_id", descriptor.Name)
	}
}

// A handler acting on a project ID receives the ID of the project the caller
// named by slug, in place of the slug. An unknown slug reaches the handler as a project that exists
// nowhere, so it answers as it does for any project it cannot find; an omitted
// slug is left for the handler to default or refuse.
func TestToolsResolveTheProjectSlugBeforeTheHandler(t *testing.T) {
	t.Parallel()

	projectID := uuid.New()
	reg := newRegistrar(newTestMCPServer())
	reg.withProjects(slugProjects{"payments": projectID})
	addTool(reg, &mcp.Tool{Name: "project_target"}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, func(_ context.Context, _ *mcp.CallToolRequest, input projectSlugTargetInput) (*mcp.CallToolResult, projectSlugTargetOutput, error) {
		return nil, projectSlugTargetOutput{ProjectID: input.ProjectID, ProjectSlug: input.ProjectSlug}, nil
	})
	ctx := ContextWithPrincipal(t.Context(), testPrincipal())

	for slug, want := range map[string]string{"payments": projectID.String(), "no-such-project": uuid.Max.String(), "": ""} {
		output, err := invokeTool[projectSlugTargetOutput](t, ctx, reg, "project_target", map[string]any{"project_slug": slug})
		require.NoError(t, err, slug)
		require.Equal(t, want, output.ProjectID, slug)
		require.Empty(t, output.ProjectSlug, "the handler sees the project named once, by ID")
	}

	unresolved := newRegistrar(newTestMCPServer())
	addTool(unresolved, &mcp.Tool{Name: "project_target"}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, func(_ context.Context, _ *mcp.CallToolRequest, input projectSlugTargetInput) (*mcp.CallToolResult, projectSlugTargetOutput, error) {
		return nil, projectSlugTargetOutput{ProjectID: input.ProjectID}, nil
	})
	_, err := invokeTool[projectSlugTargetOutput](t, ctx, unresolved, "project_target", map[string]any{"project_slug": "payments"})
	require.ErrorIs(t, err, ErrUnavailable)
}

// A hidden ProjectID with no project_slug to fill it from would reach the
// handler empty on every call, so registration refuses it.
func TestHiddenProjectIDRequiresProjectSlug(t *testing.T) {
	t.Parallel()

	type input struct {
		Name      string `json:"name"`
		ProjectID string `json:"-"`
	}
	require.Panics(t, func() {
		addTool(newRegistrar(newTestMCPServer()), &mcp.Tool{Name: "no_slug"}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, func(context.Context, *mcp.CallToolRequest, input) (*mcp.CallToolResult, projectSlugTargetOutput, error) {
			return nil, projectSlugTargetOutput{}, nil
		})
	})
}
