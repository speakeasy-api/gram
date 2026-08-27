package platformmcp

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// Every tool the deployment registers declares an audience. A tool with none
// would be unreachable; a tool admitted by accident would reach a surface
// nobody reviewed it for, which is what the audience model exists to prevent.
func TestEveryRegisteredToolDeclaresAnAudience(t *testing.T) {
	t.Parallel()

	_, registrar := newServer(nil, nil, nil, "", nil, nil, nil, nil, nil, nil, nil, nil, CatalogDescriptor{})
	descriptors := registrar.Descriptors()
	require.NotEmpty(t, descriptors, "the deployment registers tools even when every dependency is absent")

	for _, descriptor := range descriptors {
		require.NotEmpty(t, descriptor.Meta.Audiences, "tool %q declares no audience", descriptor.Name)
		require.NotEmpty(t, descriptor.InputSchema, "tool %q advertises no input schema", descriptor.Name)
	}

	// The external endpoint serves everything except the tools that exist only
	// to give the assistant a Go-level equivalent of an MCP method it cannot
	// speak. read_gram_doc is one: external clients read the same guides with
	// resources/read, and serving both would be two names for one thing.
	external := registrar.For(AudienceExternal)
	externalNames := make(map[string]bool, len(external))
	for _, descriptor := range external {
		externalNames[descriptor.Name] = true
	}
	for _, descriptor := range descriptors {
		if descriptor.Name == "read_gram_doc" {
			require.False(t, externalNames[descriptor.Name], "read_gram_doc duplicates resources/read for external clients")
			continue
		}
		require.True(t, externalNames[descriptor.Name], "tool %q is not served to the external endpoint", descriptor.Name)
	}

	assistant := registrar.For(AudienceAssistant)
	require.NotEmpty(t, assistant, "the assistant audience is admitted to the catalogue")
}

// Admission is per tool, so narrowing one tool must not affect the others.
func TestAudienceFilterSelectsPerTool(t *testing.T) {
	t.Parallel()

	registrar := &Registrar{descriptors: []Descriptor{
		{Name: "both", Meta: ToolMeta{Audiences: []Audience{AudienceExternal, AudienceAssistant}}},
		{Name: "external-only", Meta: ToolMeta{Audiences: []Audience{AudienceExternal}}},
		{Name: "unadmitted", Meta: ToolMeta{}},
	}}

	require.Equal(t, []string{"both", "external-only"}, names(registrar.For(AudienceExternal)))
	require.Equal(t, []string{"both"}, names(registrar.For(AudienceAssistant)))
}

type explicitSchemaInput struct {
	Mode string `json:"mode"`
}

type explicitSchemaOutput struct {
	Mode string `json:"mode"`
}

func TestAddToolExplicitInputSchemaIsAuthoritative(t *testing.T) {
	t.Parallel()

	server := mcp.NewServer(&mcp.Implementation{Name: "schema-test", Version: "0.0.1"}, nil)
	registrar := newRegistrar(server)
	var calls atomic.Int32
	schema := &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"mode": {Type: "string", Enum: []any{"safe"}, Default: json.RawMessage(`"safe"`)},
		},
		AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}},
	}
	addTool(registrar, &mcp.Tool{Name: "explicit_schema", InputSchema: schema}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeNone}, func(_ context.Context, _ *mcp.CallToolRequest, input explicitSchemaInput) (*mcp.CallToolResult, explicitSchemaOutput, error) {
		calls.Add(1)
		return nil, explicitSchemaOutput(input), nil
	})

	descriptor := registrar.Descriptors()[0]
	expected, err := json.Marshal(schema)
	require.NoError(t, err)
	require.JSONEq(t, string(expected), string(descriptor.InputSchema))

	_, err = descriptor.Invoke(t.Context(), json.RawMessage(`{"mode":"unsafe"}`))
	require.ErrorContains(t, err, "arguments do not match the tool schema")
	require.Zero(t, calls.Load())

	out, err := descriptor.Invoke(t.Context(), nil)
	require.NoError(t, err)
	require.Equal(t, explicitSchemaOutput{Mode: "safe"}, out)
	require.EqualValues(t, 1, calls.Load())

	out, err = descriptor.Invoke(t.Context(), json.RawMessage(`{"mode":"safe"}`))
	require.NoError(t, err)
	require.Equal(t, explicitSchemaOutput{Mode: "safe"}, out)
	require.EqualValues(t, 2, calls.Load())

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	defer func() { _ = serverSession.Close() }()
	client := mcp.NewClient(&mcp.Implementation{Name: "schema-client", Version: "0.0.1"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	listed, err := session.ListTools(t.Context(), nil)
	require.NoError(t, err)
	require.Len(t, listed.Tools, 1)
	listedSchema, err := json.Marshal(listed.Tools[0].InputSchema)
	require.NoError(t, err)
	require.JSONEq(t, string(expected), string(listedSchema))

	refused, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "explicit_schema", Arguments: map[string]any{"mode": "unsafe"}})
	require.NoError(t, err)
	require.True(t, refused.IsError)
	require.EqualValues(t, 2, calls.Load())

	accepted, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "explicit_schema", Arguments: map[string]any{}})
	require.NoError(t, err)
	require.False(t, accepted.IsError)
	require.Equal(t, map[string]any{"mode": "safe"}, accepted.StructuredContent)
	require.EqualValues(t, 3, calls.Load())
}

func TestAddToolInfersInputSchemaWhenUnset(t *testing.T) {
	t.Parallel()

	server := mcp.NewServer(&mcp.Implementation{Name: "inferred-test", Version: "0.0.1"}, nil)
	registrar := newRegistrar(server)
	addTool(registrar, &mcp.Tool{Name: "inferred_schema"}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeNone}, func(_ context.Context, _ *mcp.CallToolRequest, input explicitSchemaInput) (*mcp.CallToolResult, explicitSchemaOutput, error) {
		return nil, explicitSchemaOutput(input), nil
	})

	inferred, err := jsonschema.For[explicitSchemaInput](nil)
	require.NoError(t, err)
	expected, err := json.Marshal(inferred)
	require.NoError(t, err)
	require.JSONEq(t, string(expected), string(registrar.Descriptors()[0].InputSchema))

	_, err = registrar.Descriptors()[0].Invoke(t.Context(), json.RawMessage(`{"mode":42}`))
	require.ErrorContains(t, err, "arguments do not match the tool schema")
	_, err = registrar.Descriptors()[0].Invoke(t.Context(), json.RawMessage(`{"mode":"safe"}`))
	require.NoError(t, err)
}

func names(descriptors []Descriptor) []string {
	out := make([]string, 0, len(descriptors))
	for _, descriptor := range descriptors {
		out = append(out, descriptor.Name)
	}
	return out
}

// A tool admitted to the assistant must work without an OAuth connection.
// Admitting one that reaches connection-scoped state advertises a capability
// that always errors, which reads to a user as the platform being broken
// rather than the tool being unavailable.
func TestAssistantAudienceExcludesConnectionScopedTools(t *testing.T) {
	t.Parallel()

	_, registrar := newServer(nil, nil, nil, "", nil, nil, nil, nil, nil, nil, nil, nil, CatalogDescriptor{})

	admitted := map[string]bool{}
	for _, descriptor := range registrar.For(AudienceAssistant) {
		admitted[descriptor.Name] = true
	}

	// Named-plugin distribution is intentionally unavailable until
	// compatibility deployment. Session recall is external-only in v1: the
	// shared project-assistant surface must not serve user-personal
	// cross-project transcripts.
	for _, name := range []string{
		"distribute_mcp_to_plugin",
		"remove_mcp_from_plugin",
		"list_plugins",
		"get_plugin",
		"list_my_sessions",
		"continue_session",
	} {
		require.False(t, admitted[name], "tool %q needs a connection or is rollout-gated and must not be admitted to the assistant", name)
	}

	// The reads, registration paths, and persisted readiness projections are
	// connection-less end to end. get_setup_handoff is admitted because the
	// handoff only carries the caller to the dashboard, which completes setup
	// under its own session.
	for _, name := range []string{
		"get_platform_context",
		"list_projects",
		"find_mcp",
		"get_mcp",
		"update_mcp_metadata",
		"register_catalog_mcp",
		"register_remote_mcp",
		"search_mcp_catalog",
		"inspect_mcp_candidate",
		"send_platform_mcp_feedback",
		"get_setup_handoff",
		"get_mcp_readiness",
		"get_mcp_repair_plan",
		"attach_platform_mcp_identity_provider",
		"get_mcp_client_admission",
		"set_mcp_client_admission",
		"disable_mcp",
		"enable_mcp",
	} {
		require.True(t, admitted[name], "tool %q works without a connection and should serve the assistant", name)
	}
}

// The audience list must bind the MCP endpoint, not just the descriptor
// registry: the server IS the external surface, so a tool withheld from the
// external audience must not be listed or callable there.
func TestExternalEndpointServesOnlyExternallyAdmittedTools(t *testing.T) {
	t.Parallel()

	server, registrar := newServer(nil, nil, nil, "", nil, nil, nil, nil, nil, nil, nil, nil, CatalogDescriptor{})

	admitted := make(map[string]bool)
	for _, descriptor := range registrar.For(AudienceExternal) {
		admitted[descriptor.Name] = true
	}

	// Listed through a real MCP session rather than the registrar, so the
	// assertion covers what a client actually sees.
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	defer func() { _ = serverSession.Close() }()

	client := mcp.NewClient(&mcp.Implementation{Name: "audience-test", Version: "0.0.1"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	tools, err := session.ListTools(t.Context(), nil)
	require.NoError(t, err)
	require.NotEmpty(t, tools.Tools)
	for _, tool := range tools.Tools {
		require.Truef(t, admitted[tool.Name], "the external endpoint lists %q, which is not admitted to the external audience", tool.Name)
	}

	withheld := 0
	for _, descriptor := range registrar.Descriptors() {
		if !admitted[descriptor.Name] {
			withheld++
		}
	}
	require.Positive(t, withheld, "the catalogue withholds at least one tool from the external endpoint, so this test can fail")
}

// A tool whose result carries a SubjectCount must advertise that field as it
// serializes — a number or the suppression label — not as the Go struct it is
// reflected from. Asserted against a real session's tools/list rather than the
// inference helper, so dropping the schema in addTool fails here too.
func TestAdvertisedOutputSchemaMatchesTheSubjectCountWireForm(t *testing.T) {
	t.Parallel()

	// Registered directly rather than through newServer: with no dependencies
	// the deployment substitutes the "diagnostics are not enabled" stubs, whose
	// results carry no subject count. The handler is never called here — only
	// the schema the registration advertises is under test.
	server := newTestMCPServer()
	registerDiagnosticsTools(newRegistrar(server), nil)

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	defer func() { _ = serverSession.Close() }()

	client := mcp.NewClient(&mcp.Implementation{Name: "subject-count-test", Version: "0.0.1"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	tools, err := session.ListTools(t.Context(), nil)
	require.NoError(t, err)

	var advertised any
	for _, tool := range tools.Tools {
		if tool.Name == "get_project_overview" {
			advertised = tool.OutputSchema
		}
	}
	require.NotNil(t, advertised, "the external endpoint advertises an output schema for get_project_overview")

	encodedSchema, err := json.Marshal(advertised)
	require.NoError(t, err)
	var schema jsonschema.Schema
	require.NoError(t, json.Unmarshal(encodedSchema, &schema))
	resolved, err := schema.Resolve(nil)
	require.NoError(t, err)

	// Zero is reported exactly, three is suppressed, and twenty-five is
	// reported exactly: the three shapes active_users takes on the wire.
	for _, count := range []SubjectCount{NewSubjectCount(0), NewSubjectCount(3), NewSubjectCount(25)} {
		encoded, err := json.Marshal(GetProjectOverviewOutput{ActiveUsers: count})
		require.NoError(t, err)

		var decoded any
		require.NoError(t, json.Unmarshal(encoded, &decoded))
		require.NoError(t, resolved.Validate(decoded), "output %s", encoded)
	}
}

// Schema inference panics at process boot, so a tool input the nil-dependency
// server never registers can crash-loop production while CI stays green. The
// jsonschema tag is a description; a "word=" prefix is rejected outright.
func TestClientAdmissionToolInputsInferSchemas(t *testing.T) {
	t.Parallel()

	_, err := jsonschema.For[GetMCPClientAdmissionToolInput](nil)
	require.NoError(t, err)

	schema, err := jsonschema.For[SetMCPClientAdmissionToolInput](nil)
	require.NoError(t, err)
	require.Contains(t, schema.Properties["mode"].Description, "presets, open, or disabled")
}
