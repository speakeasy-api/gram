package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type stubReader struct {
	principal      platformmcp.Principal
	projectsInput  platformmcp.ListProjectsInput
	findInput      platformmcp.FindMCPInput
	getInput       platformmcp.GetMCPInput
	projectsOutput platformmcp.ListProjectsOutput
	findOutput     platformmcp.FindMCPOutput
	getOutput      platformmcp.MCP
	getErr         error
}

func (s *stubReader) ListProjects(_ context.Context, principal platformmcp.Principal, input platformmcp.ListProjectsInput) (platformmcp.ListProjectsOutput, error) {
	s.principal = principal
	s.projectsInput = input
	return s.projectsOutput, nil
}

func (s *stubReader) FindMCP(_ context.Context, principal platformmcp.Principal, input platformmcp.FindMCPInput) (platformmcp.FindMCPOutput, error) {
	s.principal = principal
	s.findInput = input
	return s.findOutput, nil
}

func (s *stubReader) GetMCP(_ context.Context, principal platformmcp.Principal, input platformmcp.GetMCPInput) (platformmcp.MCP, error) {
	s.principal = principal
	s.getInput = input
	return s.getOutput, s.getErr
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

func TestListProjectsToolPassesOrgPrincipalAndInputThrough(t *testing.T) {
	t.Parallel()
	reader := &stubReader{projectsOutput: platformmcp.ListProjectsOutput{Projects: []platformmcp.Project{{ID: "p1", Name: "One", Slug: "one"}}}}
	ctx := orgAuthContext(t, "org_123", nil)

	var out bytes.Buffer
	require.NoError(t, NewListProjectsTool(reader).Call(ctx, testToolCallEnv(), bytes.NewBufferString(`{"limit":500}`), &out))
	require.Equal(t, "org_123", reader.principal.OrganizationID)
	require.Equal(t, 500, reader.projectsInput.Limit)
}

func TestFindMCPToolInjectsAssistantProject(t *testing.T) {
	t.Parallel()
	projectID := uuid.New()
	reader := &stubReader{findOutput: platformmcp.FindMCPOutput{MCPs: []platformmcp.MCP{{ID: "m1", ProjectID: projectID.String(), Model: "legacy", Readiness: platformmcp.MCPReadiness{State: "unsupported"}}}}}
	ctx := orgAuthContext(t, "org_123", &projectID)

	var out bytes.Buffer
	require.NoError(t, NewFindMCPTool(reader).Call(ctx, testToolCallEnv(), bytes.NewBufferString(`{"project_id":"another-project","query":"server","limit":12,"readiness":"ready"}`), &out))
	require.Equal(t, "org_123", reader.principal.OrganizationID)
	require.Equal(t, platformmcp.AssistantClientID, reader.principal.ClientID)
	require.Equal(t, projectID.String(), reader.findInput.ProjectID)
	require.Empty(t, reader.findInput.ProjectSlug)
	require.Equal(t, "server", reader.findInput.Query)
	require.Equal(t, 12, reader.findInput.Limit)
	require.Equal(t, "ready", reader.findInput.Readiness)

	var result platformmcp.FindMCPOutput
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Len(t, result.MCPs, 1)
}

func TestFindMCPToolRequiresProjectAuthContext(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	err := NewFindMCPTool(&stubReader{}).Call(orgAuthContext(t, "org_123", nil), testToolCallEnv(), nil, &out)
	require.ErrorContains(t, err, "project auth context")
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
	reader := &stubReader{getOutput: platformmcp.MCP{ID: "m1", ProjectID: "p1", Name: "My MCP", Slug: "my-mcp", Visibility: "private", Model: "legacy", Readiness: platformmcp.MCPReadiness{State: "unsupported"}}}
	ctx := orgAuthContext(t, "org_123", nil)
	var out bytes.Buffer
	require.NoError(t, NewGetMCPTool(reader).Call(ctx, testToolCallEnv(), bytes.NewBufferString(`{"project_id":"p1","mcp_id":"m1"}`), &out))
	require.Equal(t, platformmcp.GetMCPInput{ProjectID: "p1", MCPID: "m1"}, reader.getInput)
}

// A stale tool_cursor fails with a code the caller can recognise, the same
// distinction the Platform MCP get_mcp keeps, so a paging agent knows to start
// the read again rather than treat a failure body as the server's summary.
func TestGetMCPToolFailsAToolCursorRefusalWithItsCode(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		exposureCode string
		want         oops.Code
	}{
		{exposureCode: "conflict", want: oops.CodeConflict},
		{exposureCode: "invalid_request", want: oops.CodeBadRequest},
	} {
		t.Run(tc.exposureCode, func(t *testing.T) {
			t.Parallel()
			reader := &stubReader{getErr: fmt.Errorf("read platform MCP tool exposure: %w", &platformmcp.MCPToolExposureError{
				Code: tc.exposureCode, Message: "read the server again", Cause: platformmcp.ErrMCPToolExposureConflict,
			})}
			ctx := orgAuthContext(t, "org_123", nil)
			var out bytes.Buffer
			err := NewGetMCPTool(reader).Call(ctx, testToolCallEnv(), bytes.NewBufferString(`{"project_id":"p1","mcp_id":"m1","tool_cursor":"stale"}`), &out)
			require.Equal(t, "stale", reader.getInput.ToolCursor)
			shareable, ok := errors.AsType[*oops.ShareableError](err)
			require.True(t, ok, "a cursor refusal is an error, not a result body")
			require.Equal(t, tc.want, shareable.Code)
			require.Contains(t, shareable.Error(), "read the server again")
			require.Zero(t, out.Len())
		})
	}
}

func TestGetMCPToolKeepsOtherReadFailuresAsErrors(t *testing.T) {
	t.Parallel()
	reader := &stubReader{getErr: platformmcp.ErrUnavailable}
	ctx := orgAuthContext(t, "org_123", nil)
	var out bytes.Buffer
	require.ErrorIs(t, NewGetMCPTool(reader).Call(ctx, testToolCallEnv(), bytes.NewBufferString(`{"project_id":"p1","mcp_id":"m1"}`), &out), platformmcp.ErrUnavailable)
	require.Zero(t, out.Len())
}

func TestGetPlatformContextToolReportsOrgProjectAndReadOnly(t *testing.T) {
	t.Parallel()
	projectID := uuid.New()
	ctx := orgAuthContext(t, "org_123", &projectID)
	var out bytes.Buffer
	require.NoError(t, NewGetPlatformContextTool().Call(ctx, testToolCallEnv(), nil, &out))
	var result map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Equal(t, projectID.String(), result["project_id"])
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
		"find_mcp": func() ([]byte, *bool) {
			d := NewFindMCPTool(reader).Descriptor()
			var schema struct {
				Properties map[string]json.RawMessage `json:"properties"`
			}
			require.NoError(t, json.Unmarshal(d.InputSchema, &schema))
			require.NotContains(t, schema.Properties, "project_id")
			require.NotContains(t, schema.Properties, "project_slug")
			require.Contains(t, schema.Properties, "limit")
			require.Contains(t, schema.Properties, "readiness")
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
