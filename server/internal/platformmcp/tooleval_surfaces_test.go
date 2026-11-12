package platformmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/tooleval"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// toolEvalRegistrar composes the deployment's catalogue with every external
// tool's live variant: PostgreSQL services live, and the other dependencies
// as stubs (see composeToolEvalRegistry). Tools never run in the eval; live
// composition matters because a missing dependency registers a placeholder
// tool with a one-line description and no input schema, which is not what
// clients see.
func toolEvalRegistrar(t *testing.T) *Registrar {
	t.Helper()
	logger := testenv.NewLogger(t)
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_tooleval")
	require.NoError(t, err)
	engine := authz.NewEngine(logger, conn, func(context.Context, string) (bool, error) { return false, nil }, nil)
	return composeToolEvalRegistry(t, logger, conn, engine)
}

// toolEvalCatalogue is every tool the external endpoint serves.
func toolEvalCatalogue(t *testing.T, registrar *Registrar) []tooleval.Tool {
	t.Helper()
	external := registrar.For(AudienceExternal)
	tools := make([]tooleval.Tool, 0, len(external))
	for _, descriptor := range external {
		readOnly := descriptor.Annotations != nil && descriptor.Annotations.ReadOnlyHint
		tools = append(tools, tooleval.Tool{Name: descriptor.Name, Description: descriptor.Description, InputSchema: descriptor.InputSchema, ReadOnly: readOnly})
	}
	return tools
}

// toolEvalDirectSurface lists the full catalogue with the full server
// instructions, as a client on the direct discovery mode sees it.
func toolEvalDirectSurface(t *testing.T, registrar *Registrar) tooleval.Surface {
	t.Helper()
	// A schema a model provider rejects fails the whole request, so the
	// direct surface leaves those tools out and the report names them.
	// TestPlatformToolSchemasFitModelProviders tracks them.
	var tools []tooleval.Tool
	var omitted []string
	for _, tool := range toolEvalCatalogue(t, registrar) {
		if tooleval.ProviderSchemaProblem(tool.InputSchema) != "" {
			omitted = append(omitted, tool.Name)
			continue
		}
		tools = append(tools, tool)
	}
	return tooleval.Surface{
		Name:         "direct",
		Instructions: registrar.instructions,
		Tools:        tools,
		Omitted:      omitted,
		Dispatch: func(_ context.Context, name string, arguments json.RawMessage) (tooleval.Dispatch, error) {
			if !slices.ContainsFunc(tools, func(tool tooleval.Tool) bool { return tool.Name == name }) {
				return tooleval.Dispatch{Output: fmt.Sprintf("unknown tool %q", name), Selected: nil}, nil
			}
			return tooleval.Dispatch{Output: "", Selected: &tooleval.Call{Tool: name, Arguments: arguments}}, nil
		},
	}
}

// toolEvalProgressiveSurface lists the search, describe and call tools next to
// the first-run tools, with the small-surface instructions. Search and
// describe run the real discovery code as an organization member who may use
// every tool, so ranking and descriptions are exactly what a client receives.
func toolEvalProgressiveSurface(t *testing.T, registrar *Registrar) tooleval.Surface {
	t.Helper()
	surface := &discoverySurface{runtime: &Runtime{registrar: registrar}, logger: testenv.NewLogger(t), principal: testPrincipal(), request: nil, next: nil}
	grants := []authz.Grant{authz.NewGrant(authz.ScopeRoot, authz.WildcardResource)}

	var tools []tooleval.Tool
	for _, tool := range discoveryTools(discoveryProgressive) {
		schema, ok := tool.InputSchema.(json.RawMessage)
		require.True(t, ok, "discovery tool %s schema", tool.Name)
		tools = append(tools, tooleval.Tool{Name: tool.Name, Description: tool.Description, InputSchema: schema, ReadOnly: tool.Annotations.ReadOnlyHint})
	}
	catalogue := toolEvalCatalogue(t, registrar)
	for _, tool := range catalogue {
		if slices.Contains(firstRunTools, tool.Name) {
			tools = append(tools, tool)
		}
	}
	require.Len(t, tools, len(discoveryTools(discoveryProgressive))+len(firstRunTools), "every first-run tool is in the catalogue")
	for _, tool := range tools {
		require.Empty(t, tooleval.ProviderSchemaProblem(tool.InputSchema), "the progressive surface lists %s directly, so a provider must accept its schema", tool.Name)
	}

	return tooleval.Surface{
		Name:         "progressive",
		Instructions: discoveryInstructions(discoveryProgressive),
		Tools:        tools,
		Dispatch: func(ctx context.Context, name string, arguments json.RawMessage) (tooleval.Dispatch, error) {
			ctx = authz.GrantsToContext(ctx, grants)
			switch name {
			case searchPlatformToolsName:
				return toolEvalDiscoveryOutput(surface.search(ctx, arguments))
			case describePlatformToolName:
				return toolEvalDiscoveryOutput(surface.describe(ctx, arguments))
			case callPlatformToolName:
				var input callPlatformToolInput
				if err := decodeDiscoveryArguments(arguments, &input); err != nil {
					return tooleval.Dispatch{Output: err.Error(), Selected: nil}, nil
				}
				if _, ok := surface.discoverable(ctx)[input.Name]; !ok {
					return tooleval.Dispatch{Output: fmt.Sprintf("unknown tool %q; find tools with %s", input.Name, searchPlatformToolsName), Selected: nil}, nil
				}
				return tooleval.Dispatch{Output: "", Selected: &tooleval.Call{Tool: input.Name, Arguments: input.Arguments}}, nil
			}
			if slices.Contains(firstRunTools, name) {
				return tooleval.Dispatch{Output: "", Selected: &tooleval.Call{Tool: name, Arguments: arguments}}, nil
			}
			return tooleval.Dispatch{Output: fmt.Sprintf("unknown tool %q", name), Selected: nil}, nil
		},
	}
}

func toolEvalDiscoveryOutput(result *mcp.CallToolResult, err error) (tooleval.Dispatch, error) {
	if err != nil {
		return tooleval.Dispatch{}, err
	}
	if len(result.Content) == 0 {
		return tooleval.Dispatch{Output: "", Selected: nil}, nil
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		return tooleval.Dispatch{}, fmt.Errorf("discovery result is %T, not text", result.Content[0])
	}
	return tooleval.Dispatch{Output: text.Text, Selected: nil}, nil
}

// toolEvalSnapshot is what clients are shown on each surface, as
// tooleval/catalogue.json stores it.
func toolEvalSnapshot(t *testing.T, registrar *Registrar) tooleval.Catalogue {
	t.Helper()
	return tooleval.Catalogue{
		Instructions: map[string]string{
			"direct":      registrar.instructions,
			"progressive": discoveryInstructions(discoveryProgressive),
		},
		Tools: toolEvalCatalogue(t, registrar),
	}
}
