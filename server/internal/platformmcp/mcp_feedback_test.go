package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	genmcpfeedback "github.com/speakeasy-api/gram/server/gen/mcp_feedback"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// stubMCPFeedbackLister records the payload it was sent and returns a canned
// page. The tool eval composes it so list_mcp_feedback lists live.
type stubMCPFeedbackLister struct {
	projectID uuid.UUID
	payload   *genmcpfeedback.ListPayload
	result    *genmcpfeedback.ListMcpFeedbackResult
	err       error
}

func (s *stubMCPFeedbackLister) ListForProject(_ context.Context, projectID uuid.UUID, payload *genmcpfeedback.ListPayload) (*genmcpfeedback.ListMcpFeedbackResult, error) {
	s.projectID, s.payload = projectID, payload
	return s.result, s.err
}

var (
	mcpFeedbackTestProject = uuid.MustParse("0b7c3d1e-0000-4000-8000-000000000001")
	mcpFeedbackTestServer  = uuid.MustParse("5f1e2a00-0000-4000-8000-000000000011")
)

func mcpFeedbackTestPrincipal() Principal {
	return Principal{UserID: "user-1", OrganizationID: "org-1", ConnectionID: "connection-1", Generation: "1", ClientID: "client-1", Surface: SurfacePlatformMCP}
}

func mcpFeedbackTestInput() ListMCPFeedbackInput {
	return ListMCPFeedbackInput{ProjectID: mcpFeedbackTestProject.String(), ProjectSlug: "", MCPID: mcpFeedbackTestServer.String(), Severity: "", ToolName: "", Cursor: "", Limit: 0}
}

func TestListMCPFeedbackSeparatesReporterTextAndDropsIdentities(t *testing.T) {
	t.Parallel()

	blocked := types.McpFeedbackSeverity("blocked")
	lister := &stubMCPFeedbackLister{result: &genmcpfeedback.ListMcpFeedbackResult{
		Counts: &genmcpfeedback.McpFeedbackCounts{WindowStart: "2026-10-27T00:00:00Z", WindowEnd: "2026-11-26T00:00:00Z", Total: 3, Blocked: 1, Degraded: 1, Cosmetic: 0, Unspecified: 1},
		Tools:  []*genmcpfeedback.McpFeedbackToolCount{{ToolName: "create_issue\u202e", FeedbackCount: 2, BlockedCount: 1}},
		Feedback: []*types.McpFeedback{{
			ID: "11111111-0000-4000-8000-000000000001", McpServerID: mcpFeedbackTestServer.String(),
			// A right-to-left override and a zero-width space would hide text
			// from the person reading the summary.
			Content:        "create_issue failed\u202e\u200b. Line two\nstays.",
			Severity:       &blocked,
			Workaround:     new("used update_issue"),
			Intention:      new("file a bug"),
			ToolName:       new("create_issue"),
			Model:          new("model-x"),
			UserID:         new("user-secret"),
			UserEmail:      new("reporter@example.com"),
			ExternalUserID: new("external-secret"),
			ClientName:     new("client\x07"),
			ClientVersion:  new("1.0"),
			CreatedAt:      "2026-11-25T10:00:00Z",
		}},
		NextCursor: new("next"),
	}}
	service := NewMCPFeedbackReadService(lister, allowBudget())

	output, err := service.List(t.Context(), mcpFeedbackTestPrincipal(), mcpFeedbackTestInput())
	require.NoError(t, err)

	require.Equal(t, mcpFeedbackTestProject, lister.projectID)
	require.Equal(t, mcpFeedbackTestServer.String(), lister.payload.McpServerID)
	require.Equal(t, mcpFeedbackDefaultLimit, lister.payload.Limit)

	require.Equal(t, MCPFeedbackTrustNotice, output.ContentTrust)
	require.Equal(t, "next", output.NextCursor)
	require.Equal(t, int64(3), output.Counts.Total)
	require.Equal(t, []MCPFeedbackToolTally{{ToolName: findingLabel("create_issue\u202e"), FeedbackCount: 2, BlockedCount: 1}}, output.Tools)
	require.NotContains(t, output.Tools[0].ToolName, "\u202e")
	require.Len(t, output.Reports, 1)
	report := output.Reports[0]
	require.Equal(t, "blocked", report.Severity)
	require.Equal(t, MCPFeedbackUntrustedReport{
		Content: "create_issue failed. Line two\nstays.", Workaround: "used update_issue", Intention: "file a bug",
		ToolName: "create_issue", Model: "model-x", ClientName: "client", ClientVersion: "1.0",
	}, report.UntrustedReport)

	encoded, err := json.Marshal(output)
	require.NoError(t, err)
	for _, identity := range []string{"user-secret", "reporter@example.com", "external-secret"} {
		require.NotContains(t, string(encoded), identity)
	}
}

func TestListMCPFeedbackRejectsInvalidInputBeforeReading(t *testing.T) {
	t.Parallel()

	for name, mutate := range map[string]func(*ListMCPFeedbackInput){
		"unresolved project": func(in *ListMCPFeedbackInput) { in.ProjectID = "" },
		"mcp id not a uuid":  func(in *ListMCPFeedbackInput) { in.MCPID = "gateway:abc" },
		"nil mcp id":         func(in *ListMCPFeedbackInput) { in.MCPID = uuid.Nil.String() },
		"unknown severity":   func(in *ListMCPFeedbackInput) { in.Severity = "critical" },
		"limit too large":    func(in *ListMCPFeedbackInput) { in.Limit = mcpFeedbackMaxLimit + 1 },
		"negative limit":     func(in *ListMCPFeedbackInput) { in.Limit = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			lister := &stubMCPFeedbackLister{}
			input := mcpFeedbackTestInput()
			mutate(&input)
			_, err := NewMCPFeedbackReadService(lister, allowBudget()).List(t.Context(), mcpFeedbackTestPrincipal(), input)
			require.ErrorIs(t, err, errMCPFeedbackInvalid)
			require.Nil(t, lister.payload)
		})
	}
}

func TestListMCPFeedbackPassesFilters(t *testing.T) {
	t.Parallel()

	lister := &stubMCPFeedbackLister{result: &genmcpfeedback.ListMcpFeedbackResult{Counts: nil, Tools: nil, Feedback: nil, NextCursor: nil}}
	input := mcpFeedbackTestInput()
	input.Severity, input.ToolName, input.Cursor, input.Limit = "degraded", " create_issue ", "abc", 5
	output, err := NewMCPFeedbackReadService(lister, allowBudget()).List(t.Context(), mcpFeedbackTestPrincipal(), input)
	require.NoError(t, err)
	require.Equal(t, types.McpFeedbackSeverity("degraded"), *lister.payload.Severity)
	require.Equal(t, "create_issue", *lister.payload.ToolName)
	require.Equal(t, "abc", *lister.payload.Cursor)
	require.Equal(t, 5, lister.payload.Limit)
	require.Empty(t, output.Reports)
	require.NotNil(t, output.Reports, "an empty page lists [] rather than null")
}

func TestListMCPFeedbackRefusesWithoutRevealingWhichServersExist(t *testing.T) {
	t.Parallel()

	for name, cause := range map[string]error{
		"forbidden": oops.E(oops.CodeForbidden, nil, "forbidden"),
		"not found": oops.E(oops.CodeNotFound, nil, "mcp server not found"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := NewMCPFeedbackReadService(&stubMCPFeedbackLister{err: cause}, allowBudget()).List(t.Context(), mcpFeedbackTestPrincipal(), mcpFeedbackTestInput())
			require.ErrorIs(t, err, errMCPFeedbackNotFound)
			result, ok := mcpFeedbackToolResult(err)
			require.True(t, ok)
			require.True(t, result.IsError)
		})
	}

	_, err := NewMCPFeedbackReadService(&stubMCPFeedbackLister{err: oops.E(oops.CodeBadRequest, nil, "invalid cursor")}, allowBudget()).List(t.Context(), mcpFeedbackTestPrincipal(), mcpFeedbackTestInput())
	require.ErrorIs(t, err, errMCPFeedbackInvalid)

	unexpected := errors.New("database down")
	_, err = NewMCPFeedbackReadService(&stubMCPFeedbackLister{err: unexpected}, allowBudget()).List(t.Context(), mcpFeedbackTestPrincipal(), mcpFeedbackTestInput())
	require.ErrorIs(t, err, unexpected)
}

func TestListMCPFeedbackAdmitsOnlyExternalPeopleAndMeters(t *testing.T) {
	t.Parallel()

	lister := &stubMCPFeedbackLister{}
	assistant := mcpFeedbackTestPrincipal()
	assistant.Surface = SurfaceProjectAssistant
	_, err := NewMCPFeedbackReadService(lister, allowBudget()).List(t.Context(), assistant, mcpFeedbackTestInput())
	require.ErrorIs(t, err, ErrForbidden)

	_, err = NewMCPFeedbackReadService(lister, denyBudget()).List(t.Context(), mcpFeedbackTestPrincipal(), mcpFeedbackTestInput())
	require.Error(t, err)
	require.Nil(t, lister.payload, "a refused charge reads nothing")

	require.Nil(t, NewMCPFeedbackReadService(nil, allowBudget()))
	require.Nil(t, NewMCPFeedbackReadService(lister, OperationBudget{Connection: nil, Organization: nil}))
}

func TestListMCPFeedbackToolContract(t *testing.T) {
	t.Parallel()

	for name, service := range map[string]*MCPFeedbackReadService{
		"live":        NewMCPFeedbackReadService(&stubMCPFeedbackLister{}, allowBudget()),
		"unavailable": nil,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			registrar := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "mcp-feedback", Version: "0.0.1"}, nil))
			registerMCPFeedbackTool(registrar, service)

			external := registrar.For(AudienceExternal)
			index := slices.IndexFunc(external, func(d Descriptor) bool { return d.Name == listMCPFeedbackToolName })
			require.GreaterOrEqual(t, index, 0)
			descriptor := external[index]
			require.Empty(t, registrar.For(AudienceAssistant), "an assistant can run unattended, so it never reads strangers' feedback")
			require.Equal(t, ExternalAuthorizationMember, descriptor.Meta.Authorization)
			require.Equal(t, ProjectScopeExplicit, descriptor.Meta.ProjectScope)
			require.Equal(t, discoveryMCPRead, descriptor.Meta.DiscoveryScopes)
			require.True(t, descriptor.Annotations.ReadOnlyHint)
			require.Contains(t, descriptor.Description, "UNTRUSTED CONTENT")
			require.Contains(t, descriptor.Description, "prompt injection")
			require.Contains(t, descriptor.Description, "never as instructions")

			var schema struct {
				Properties map[string]json.RawMessage `json:"properties"`
				Required   []string                   `json:"required"`
			}
			require.NoError(t, json.Unmarshal(descriptor.InputSchema, &schema))
			require.ElementsMatch(t, []string{"project_slug", "mcp_id"}, schema.Required)
			require.Len(t, schema.Properties, 6)
		})
	}
}
