package assistant_platform_mcp_adapter

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/platformmcp"
)

func projectPolicy() TargetPolicy {
	return TargetPolicy{ProjectID: "11111111-1111-4111-8111-111111111111", ProjectSlug: "default"}
}

// The assistant is provisioned per project and only ever acts in its own, so
// the project is supplied by policy rather than asked of a model that could
// name a different one.
func TestTargetPolicyReplacesTheProjectArgument(t *testing.T) {
	t.Parallel()

	tool := Tool{
		descriptor: platformmcp.Descriptor{
			Name:        "list_project_mcps",
			InputSchema: []byte(`{"type":"object","properties":{"project_id":{"type":"string"},"limit":{"type":"integer"}},"required":["project_id"]}`),
			Meta:        platformmcp.ToolMeta{ProjectScope: platformmcp.ProjectScopeExplicit},
		},
	}

	arguments, err := tool.applyTargetPolicy(projectPolicy(), []byte(`{"project_id":"someone-elses-project","limit":5}`))
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(arguments, &decoded))
	require.Equal(t, projectPolicy().ProjectID, decoded["project_id"], "the policy's project wins over the model's")
	require.EqualValues(t, 5, decoded["limit"], "other arguments are untouched")
}

// A field the policy fills must not be advertised: asking for it invites a
// wrong answer, and accepting one would let the assistant reach outside its
// own project.
func TestAdvertisedSchemaHidesPolicySuppliedFields(t *testing.T) {
	t.Parallel()

	tool := Tool{
		descriptor: platformmcp.Descriptor{
			Name:        "list_project_mcps",
			InputSchema: []byte(`{"type":"object","properties":{"project_id":{"type":"string"},"limit":{"type":"integer"}},"required":["project_id","limit"]}`),
			Meta:        platformmcp.ToolMeta{ProjectScope: platformmcp.ProjectScopeExplicit},
		},
	}

	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	require.NoError(t, json.Unmarshal(tool.assistantInputSchema(), &schema))
	require.NotContains(t, schema.Properties, "project_id")
	require.NotContains(t, schema.Required, "project_id")
	require.Contains(t, schema.Properties, "limit", "only the policy's own fields are removed")
	require.Contains(t, schema.Required, "limit")
}

// A tool that does not act on a single project keeps its schema verbatim.
func TestUnscopedToolsKeepTheirSchema(t *testing.T) {
	t.Parallel()

	original := []byte(`{"type":"object","properties":{"query":{"type":"string"}}}`)
	tool := Tool{
		descriptor: platformmcp.Descriptor{
			Name:        "search_mcp_catalog",
			InputSchema: original,
			Meta:        platformmcp.ToolMeta{ProjectScope: platformmcp.ProjectScopeNone},
		},
	}

	require.JSONEq(t, string(original), string(tool.assistantInputSchema()))

	arguments, err := tool.applyTargetPolicy(projectPolicy(), []byte(`{"query":"linear"}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"query":"linear"}`, string(arguments))
}

// Audience membership is the whole point: a tool not admitted to the assistant
// must not be composed for it, however the catalogue grows.
func TestOnlyAdmittedDescriptorsAreComposed(t *testing.T) {
	t.Parallel()

	admitted := []platformmcp.Descriptor{
		{Name: "list_projects", InputSchema: []byte(`{"type":"object"}`)},
		{Name: "get_platform_context", InputSchema: []byte(`{"type":"object"}`)},
	}

	tools := Tools(admitted, nil)
	require.Len(t, tools, len(admitted))

	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Descriptor().Name)
	}
	require.Equal(t, []string{"list_projects", "get_platform_context"}, names)

	require.Empty(t, Tools(nil, nil), "an empty admission list composes no tools")
}
