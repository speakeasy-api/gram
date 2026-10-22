package platformmcp

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// Every audience gets readOnlyHint, destructiveHint, and openWorldHint as
// explicit booleans. The SDK always writes readOnlyHint but omits a nil
// destructiveHint or openWorldHint, and some clients refuse a tool that
// leaves one implicit. Speakeasy reads an unset hint as undeclared, so each
// tool must set the hints from its real behaviour. A tool keeps the same
// hints whether it is switched on or not, so a rollout flag never changes
// what a client sees.
//
// The test checks three things: each tool sets all three hints, a read-only
// tool is not destructive, and a tool has the same hints in both branches. It
// does not check that each openWorldHint value is correct. Each tool sets that
// value from what its handler does.
func TestEveryAdvertisedToolDeclaresExplicitHints(t *testing.T) {
	t.Parallel()

	seen := map[string]*mcp.ToolAnnotations{}
	for _, reader := range []Reader{nil, NewPostgresReader(testenv.NewLogger(t), nil)} {
		server, registrar := newServer(reader, nil, nil, "", nil, nil, nil, nil, nil, nil, nil, nil, nil, CatalogDescriptor{})
		bindExternalTestPrincipal(server)
		registrar.withExternalAuthorizer(allowExternalCallAuthorizer{})

		branch := map[string]*mcp.ToolAnnotations{}
		for _, descriptor := range registrar.Descriptors() {
			assertExplicitHints(t, descriptor.Name, descriptor.Annotations)
			branch[descriptor.Name] = descriptor.Annotations
		}

		tools := listAdvertisedTools(t, server)
		require.NotEmpty(t, tools)
		for _, tool := range tools {
			assertExplicitHints(t, tool.Name, tool.Annotations)
			branch[tool.Name] = tool.Annotations
		}

		for name, annotations := range branch {
			if previous, ok := seen[name]; ok {
				assert.Equal(t, previous, annotations, "%s changes its annotations when it is switched on", name)
				continue
			}
			seen[name] = annotations
		}
	}
}

func assertExplicitHints(t *testing.T, name string, annotations *mcp.ToolAnnotations) {
	t.Helper()

	if !assert.NotNil(t, annotations, "%s declares no annotations", name) {
		return
	}
	destructive := assert.NotNil(t, annotations.DestructiveHint, "%s leaves destructiveHint implicit", name)
	assert.NotNil(t, annotations.OpenWorldHint, "%s leaves openWorldHint implicit", name)
	if destructive && annotations.ReadOnlyHint {
		assert.False(t, *annotations.DestructiveHint, "%s is read-only and destructive", name)
	}
}
