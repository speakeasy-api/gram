package platformmcp

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const testWorkflowRunOrg = "org_test"

type capturedWorkflowRunEvent struct {
	Name       string
	DistinctID string
	Properties map[string]any
}

type captureWorkflowRunEmitter struct {
	mu     sync.Mutex
	events []capturedWorkflowRunEvent
}

func (c *captureWorkflowRunEmitter) CaptureEvent(_ context.Context, eventName string, distinctID string, eventProperties map[string]any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, capturedWorkflowRunEvent{Name: eventName, DistinctID: distinctID, Properties: eventProperties})
	return nil
}

func (c *captureWorkflowRunEmitter) captured() []capturedWorkflowRunEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]capturedWorkflowRunEvent, len(c.events))
	copy(out, c.events)
	return out
}

func newTestWorkflowRunService(t *testing.T) (*WorkflowRunService, *captureWorkflowRunEmitter) {
	t.Helper()
	emitter := &captureWorkflowRunEmitter{}
	return NewWorkflowRunService(testenv.NewLogger(t), emitter), emitter
}

func testWorkflowRunInput(items ...WorkflowRunItem) WorkflowRunInput {
	return WorkflowRunInput{
		Skill:       "add-existing-mcp-servers",
		RunID:       "run-1",
		ProjectSlug: "speakeasy",
		Client:      "claude_code",
		Items:       items,
	}
}

func testWorkflowRunItem() WorkflowRunItem {
	return WorkflowRunItem{Name: "linear", Kind: "streamable_http", Endpoint: "https://mcp.linear.app/mcp", Outcome: "added", Reason: ""}
}

func TestWorkflowRunRecordEmitsPerItemAndRunEvents(t *testing.T) {
	t.Parallel()

	service, emitter := newTestWorkflowRunService(t)
	err := service.Record(t.Context(), Principal{OrganizationID: testWorkflowRunOrg}, testWorkflowRunInput(
		testWorkflowRunItem(),
		WorkflowRunItem{Name: "local-tool", Kind: "stdio", Endpoint: "", Outcome: "blocked", Reason: "stdio transport is not importable"},
	))
	require.NoError(t, err)

	events := emitter.captured()
	require.Len(t, events, 3)

	require.Equal(t, workflowRunItemEvent, events[0].Name)
	require.Equal(t, testWorkflowRunOrg, events[0].DistinctID)
	require.Equal(t, "add-existing-mcp-servers", events[0].Properties["skill"])
	require.Equal(t, "linear", events[0].Properties["name"])
	require.Equal(t, "https://mcp.linear.app/mcp", events[0].Properties["endpoint"])
	require.Equal(t, "added", events[0].Properties["outcome"])

	require.Equal(t, "blocked", events[1].Properties["outcome"])
	require.Equal(t, "stdio transport is not importable", events[1].Properties["reason"])

	require.Equal(t, workflowRunEvent, events[2].Name)
	require.Equal(t, 2, events[2].Properties["items_total"])
	require.Equal(t, 1, events[2].Properties["items_added"])
	require.Equal(t, 1, events[2].Properties["items_blocked"])
	require.Equal(t, 0, events[2].Properties["items_failed"])
	require.Equal(t, 0, events[2].Properties["items_added_unverified"])
}

// Only scheme, host and path reach analytics. Anything that could hide a
// credential in the tail of a URL is dropped rather than inspected, and an
// endpoint that is not plain https is dropped whole — the item still reports.
func TestWorkflowRunRecordKeepsOnlySchemeHostAndPath(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"https://mcp.example.test/mcp":                         "https://mcp.example.test/mcp",
		"https://mcp.example.test/mcp?region=eu":               "https://mcp.example.test/mcp",
		"https://mcp.example.test/mcp?access_token=SECRET":     "https://mcp.example.test/mcp",
		"https://mcp.example.test/mcp?api-key=SECRET":          "https://mcp.example.test/mcp",
		"https://mcp.example.test/mcp?a=b;access_token=SECRET": "https://mcp.example.test/mcp",
		"https://mcp.example.test/mcp#access_token=SECRET":     "https://mcp.example.test/mcp",
		"https://user:pass@mcp.example.test/mcp":               "",
		"http://mcp.example.test/mcp":                          "",
		"npx -y some-server --token SECRET":                    "",
	}

	for endpoint, want := range cases {
		service, emitter := newTestWorkflowRunService(t)
		err := service.Record(t.Context(), Principal{OrganizationID: testWorkflowRunOrg}, testWorkflowRunInput(
			WorkflowRunItem{Name: "candidate", Kind: "streamable_http", Endpoint: endpoint, Outcome: "blocked", Reason: ""},
		))
		require.NoErrorf(t, err, "endpoint %q must not fail the report", endpoint)

		events := emitter.captured()
		require.Equalf(t, want, events[0].Properties["endpoint"], "endpoint %q", endpoint)
		require.Equalf(t, "candidate", events[0].Properties["name"], "endpoint %q must keep its item", endpoint)
		require.NotContainsf(t, events[0].Properties["endpoint"], "SECRET", "endpoint %q must not leak", endpoint)
	}
}

// A run that considered nothing still reports, which is how "the workflow found
// no servers" reaches the team at all.
func TestWorkflowRunRecordAcceptsEmptyRun(t *testing.T) {
	t.Parallel()

	service, emitter := newTestWorkflowRunService(t)
	require.NoError(t, service.Record(t.Context(), Principal{OrganizationID: testWorkflowRunOrg}, testWorkflowRunInput()))

	events := emitter.captured()
	require.Len(t, events, 1)
	require.Equal(t, workflowRunEvent, events[0].Name)
	require.Equal(t, 0, events[0].Properties["items_total"])
}

// A catalogue add the workflow completed but could not prove is neither a
// confirmed add nor a failure, and must not be reported as blocked.
func TestWorkflowRunRecordCountsUnverifiedAdds(t *testing.T) {
	t.Parallel()

	service, emitter := newTestWorkflowRunService(t)
	err := service.Record(t.Context(), Principal{OrganizationID: testWorkflowRunOrg}, testWorkflowRunInput(
		WorkflowRunItem{Name: "vercel", Kind: "catalog", Endpoint: "https://mcp.example.test/vercel", Outcome: "added_unverified", Reason: "configuration not read back"},
	))
	require.NoError(t, err)

	events := emitter.captured()
	require.Equal(t, "added_unverified", events[0].Properties["outcome"])
	require.Equal(t, 1, events[1].Properties["items_added_unverified"])
	require.Equal(t, 0, events[1].Properties["items_blocked"])
}

func TestWorkflowRunRecordRejectsMalformedReports(t *testing.T) {
	t.Parallel()

	noSkill := testWorkflowRunInput(testWorkflowRunItem())
	noSkill.Skill = ""
	noRunID := testWorkflowRunInput(testWorkflowRunItem())
	noRunID.RunID = ""

	cases := map[string]struct {
		principal Principal
		input     WorkflowRunInput
	}{
		"no skill":        {Principal{OrganizationID: testWorkflowRunOrg}, noSkill},
		"no run id":       {Principal{OrganizationID: testWorkflowRunOrg}, noRunID},
		"unknown outcome": {Principal{OrganizationID: testWorkflowRunOrg}, testWorkflowRunInput(WorkflowRunItem{Name: "linear", Kind: "", Endpoint: "", Outcome: "deployed", Reason: ""})},
		"no name":         {Principal{OrganizationID: testWorkflowRunOrg}, testWorkflowRunInput(WorkflowRunItem{Name: "", Kind: "", Endpoint: "", Outcome: "added", Reason: ""})},
		"no organization": {Principal{OrganizationID: ""}, testWorkflowRunInput(testWorkflowRunItem())},
	}

	for name, test := range cases {
		service, emitter := newTestWorkflowRunService(t)
		require.ErrorIsf(t, service.Record(t.Context(), test.principal, test.input), ErrWorkflowRunInvalid, "%s must be rejected", name)
		require.Emptyf(t, emitter.captured(), "%s must not be emitted", name)
	}
}

func TestWorkflowRunToolRegisteredAsStubWithoutEmitter(t *testing.T) {
	t.Parallel()

	_, registrar := newServer(nil, nil, nil, "", nil, nil, nil, nil, nil, nil, nil, nil, nil, CatalogDescriptor{})

	var found bool
	for _, descriptor := range registrar.Descriptors() {
		if descriptor.Name == recordWorkflowRunToolName {
			found = true
			require.Contains(t, descriptor.Description, "not switched on for your organization yet")
			require.Equal(t, ExternalAuthorizationOrgAdmin, descriptor.Meta.Authorization)
			require.Equal(t, ProjectScopeNone, descriptor.Meta.ProjectScope)
		}
	}
	require.True(t, found, "%s must stay in the catalogue as a stub", recordWorkflowRunToolName)
}
