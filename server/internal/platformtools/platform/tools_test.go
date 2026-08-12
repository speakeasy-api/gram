package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type stubReader struct {
	principal      platformmcp.Principal
	projectsInput  platformmcp.ListProjectsInput
	mcpsInput      platformmcp.ListProjectMCPsInput
	getInput       platformmcp.GetMCPInput
	projectsOutput platformmcp.ListProjectsOutput
	mcpsOutput     platformmcp.ListProjectMCPsOutput
	getOutput      platformmcp.MCP
}

func (s *stubReader) ListProjects(_ context.Context, principal platformmcp.Principal, input platformmcp.ListProjectsInput) (platformmcp.ListProjectsOutput, error) {
	s.principal = principal
	s.projectsInput = input
	return s.projectsOutput, nil
}

func (s *stubReader) ListProjectMCPs(_ context.Context, principal platformmcp.Principal, input platformmcp.ListProjectMCPsInput) (platformmcp.ListProjectMCPsOutput, error) {
	s.principal = principal
	s.mcpsInput = input
	return s.mcpsOutput, nil
}

func (s *stubReader) GetMCP(_ context.Context, principal platformmcp.Principal, input platformmcp.GetMCPInput) (platformmcp.MCP, error) {
	s.principal = principal
	s.getInput = input
	return s.getOutput, nil
}

func testToolCallEnv() toolconfig.ToolCallEnv {
	return toolconfig.ToolCallEnv{
		UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
		GramChatID: "",
	}
}

func orgAuthContext(t *testing.T, orgID string, projectID *uuid.UUID) context.Context {
	t.Helper()
	return contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: orgID,
		ProjectID:            projectID,
	})
}

// Limit clamping is the Reader's job (platformmcp.PostgresReader applies
// boundedLimit internally); the adapter passes the caller's limit through.
func TestListProjectsToolPassesOrgPrincipalAndInputThrough(t *testing.T) {
	t.Parallel()

	reader := &stubReader{
		projectsOutput: platformmcp.ListProjectsOutput{
			Projects:  []platformmcp.Project{{ID: "p1", Name: "One", Slug: "one"}},
			Truncated: false,
		},
	}
	ctx := orgAuthContext(t, "org_123", nil)

	var out bytes.Buffer
	require.NoError(t, NewListProjectsTool(reader).Call(ctx, testToolCallEnv(), bytes.NewBufferString(`{"limit": 500}`), &out))
	require.Equal(t, "org_123", reader.principal.OrganizationID)
	require.Equal(t, 500, reader.projectsInput.Limit)

	var result platformmcp.ListProjectsOutput
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Len(t, result.Projects, 1)
	require.Equal(t, "one", result.Projects[0].Slug)
}

func TestListProjectsToolRejectsMissingAuthContext(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	err := NewListProjectsTool(&stubReader{}).Call(t.Context(), testToolCallEnv(), nil, &out)
	require.ErrorContains(t, err, "organization auth context")
}

func TestListProjectMCPsToolRequiresProjectID(t *testing.T) {
	t.Parallel()

	ctx := orgAuthContext(t, "org_123", nil)

	var out bytes.Buffer
	err := NewListProjectMCPsTool(&stubReader{}).Call(ctx, testToolCallEnv(), bytes.NewBufferString(`{}`), &out)
	require.ErrorContains(t, err, "project_id is required")
}

func TestListProjectMCPsToolPassesInputThrough(t *testing.T) {
	t.Parallel()

	reader := &stubReader{
		mcpsOutput: platformmcp.ListProjectMCPsOutput{
			MCPs:      []platformmcp.MCP{{ID: "m1", ProjectID: "p1", Visibility: "public"}},
			Truncated: false,
		},
	}
	ctx := orgAuthContext(t, "org_123", nil)

	var out bytes.Buffer
	require.NoError(t, NewListProjectMCPsTool(reader).Call(ctx, testToolCallEnv(), bytes.NewBufferString(`{"project_id":"p1"}`), &out))
	require.Equal(t, "org_123", reader.principal.OrganizationID)
	require.Equal(t, "p1", reader.mcpsInput.ProjectID)
	require.Equal(t, 0, reader.mcpsInput.Limit, "unset limit reaches the reader untouched; the reader owns clamping")
}

func TestGetMCPToolRequiresBothIDs(t *testing.T) {
	t.Parallel()

	ctx := orgAuthContext(t, "org_123", nil)

	var out bytes.Buffer
	err := NewGetMCPTool(&stubReader{}).Call(ctx, testToolCallEnv(), bytes.NewBufferString(`{"project_id":"p1"}`), &out)
	require.ErrorContains(t, err, "project_id and mcp_id are required")
}

func TestGetMCPToolReturnsReaderOutput(t *testing.T) {
	t.Parallel()

	reader := &stubReader{
		getOutput: platformmcp.MCP{ID: "m1", ProjectID: "p1", Name: "My MCP", Slug: "my-mcp", Visibility: "public"},
	}
	ctx := orgAuthContext(t, "org_123", nil)

	var out bytes.Buffer
	require.NoError(t, NewGetMCPTool(reader).Call(ctx, testToolCallEnv(), bytes.NewBufferString(`{"project_id":"p1","mcp_id":"m1"}`), &out))
	require.Equal(t, platformmcp.GetMCPInput{ProjectID: "p1", MCPID: "m1"}, reader.getInput)

	var result platformmcp.MCP
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Equal(t, "my-mcp", result.Slug)
}

func TestGetPlatformContextToolReportsOrgProjectAndReadOnly(t *testing.T) {
	t.Parallel()

	projectID := uuid.New()
	ctx := orgAuthContext(t, "org_123", &projectID)

	var out bytes.Buffer
	require.NoError(t, NewGetPlatformContextTool().Call(ctx, testToolCallEnv(), nil, &out))

	var result map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Equal(t, "org_123", result["organization_id"])
	require.Equal(t, projectID.String(), result["project_id"])
	require.Equal(t, true, result["read_only"])
	require.NotContains(t, result, "connection_id")
}

func TestToolDescriptorsCarrySchemasAndReadOnlyAnnotations(t *testing.T) {
	t.Parallel()

	reader := &stubReader{}
	for name, descriptor := range map[string]func() ([]byte, *bool){
		"get_platform_context": func() ([]byte, *bool) {
			d := NewGetPlatformContextTool().Descriptor()
			return d.InputSchema, d.Annotations.ReadOnlyHint
		},
		"list_projects": func() ([]byte, *bool) {
			d := NewListProjectsTool(reader).Descriptor()
			return d.InputSchema, d.Annotations.ReadOnlyHint
		},
		"list_project_mcps": func() ([]byte, *bool) {
			d := NewListProjectMCPsTool(reader).Descriptor()
			return d.InputSchema, d.Annotations.ReadOnlyHint
		},
		"get_mcp": func() ([]byte, *bool) {
			d := NewGetMCPTool(reader).Descriptor()
			return d.InputSchema, d.Annotations.ReadOnlyHint
		},
	} {
		schema, readOnly := descriptor()
		require.NotEmpty(t, schema, "%s must publish an input schema", name)
		require.NotNil(t, readOnly, "%s must carry annotations", name)
		require.True(t, *readOnly, "%s must be read-only", name)
	}
}
