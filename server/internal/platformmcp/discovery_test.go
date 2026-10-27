package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/codemode"
	"github.com/speakeasy-api/gram/server/internal/feature"
)

const testCodexClientID = "https://chatgpt.com/oauth/codex/client.json"

// Codex hides Agent Plugin tools past these budgets and cuts instructions to
// the last one (codex-rs/core/src/mcp_tool_exposure.rs).
const (
	hostToolSpecBudget    = 8 << 10
	hostTotalToolBudget   = 64_000
	hostInstructionBudget = 1 << 10
)

// harnessFlags resolves the discovery flag from the harness person property,
// the way the PostHog release conditions do.
type harnessFlags map[string]feature.Variant

func (harnessFlags) IsFlagEnabled(context.Context, feature.Flag, string, map[string]string) (bool, error) {
	return false, nil
}

func (harnessFlags) IsFlagEnabledLocal(context.Context, feature.Flag, string, map[string]string, map[string]string) (bool, error) {
	return false, nil
}

func (harnessFlags) FlagPayload(context.Context, feature.Flag, string, map[string]string) ([]byte, error) {
	return nil, nil
}

func (f harnessFlags) FlagVariantLocal(_ context.Context, flag feature.Flag, _ string, _, personProperties map[string]string) (feature.Variant, error) {
	if flag != feature.FlagPlatformMCPDiscoveryMode {
		return "", nil
	}
	return f[personProperties["harness"]], nil
}

func TestHarnessForClient(t *testing.T) {
	t.Parallel()

	for clientID, want := range map[string]string{
		testCodexClientID: "codex",
		"https://chatgpt.com/oauth/codex/server-id/client.json": "codex",
		testChatGPTConnectorClientID:                            "chatgpt",
		testOtherClientID:                                       "unknown",
		"00000000-0000-0000-0000-000000000000":                  "unknown",
	} {
		require.Equal(t, want, harnessForClient(clientID), clientID)
	}
}

func TestDiscoveryModeFollowsHarnessVariant(t *testing.T) {
	t.Parallel()

	codeAvailable := true
	runtime := newAllowlistTestRuntime(t, nil).WithDiscovery(DiscoveryConfig{
		Flags:             harnessFlags{"codex": feature.VariantPlatformMCPDiscoveryCodeMode, "chatgpt": feature.VariantPlatformMCPDiscoveryProgressive, "claude": "unrecognized"},
		CodeExecutor:      codemode.Disabled{},
		CodeModeAvailable: func(context.Context, string) bool { return codeAvailable },
	})
	logger := runtime.registrar.logger
	mode := func(clientID string) discoveryMode {
		principal := testPrincipal()
		principal.ClientID = clientID
		return runtime.discoveryModeFor(t.Context(), logger, principal)
	}

	require.Equal(t, discoveryCode, mode(testCodexClientID))
	require.Equal(t, discoveryProgressive, mode(testChatGPTConnectorClientID))
	require.Equal(t, discoveryFull, mode(testOtherClientID), "harnesses outside the treatment keep the catalogue")

	codeAvailable = false
	require.Equal(t, discoveryProgressive, mode(testCodexClientID), "code mode falls back to progressive")

	unflagged := newAllowlistTestRuntime(t, nil)
	principal := testPrincipal()
	principal.ClientID = testCodexClientID
	require.Equal(t, discoveryFull, unflagged.discoveryModeFor(t.Context(), logger, principal), "no flag provider serves the catalogue")
}

func TestDiscoverySurfaceFitsHostBudget(t *testing.T) {
	t.Parallel()

	for _, mode := range []discoveryMode{discoveryProgressive, discoveryCode} {
		raw, err := json.Marshal(&mcp.ListToolsResult{Tools: discoveryTools(mode)})
		require.NoError(t, err)
		// Leave most of the shared budget to the other plugins in the session.
		require.Less(t, len(raw), hostTotalToolBudget/4, mode)
		t.Logf("%s: tools/list %d bytes, instructions %d bytes", mode, len(raw), len(discoveryInstructions(mode)))
		for _, tool := range discoveryTools(mode) {
			spec, err := json.Marshal(tool)
			require.NoError(t, err)
			require.Less(t, len(spec), hostToolSpecBudget, tool.Name)
		}
		require.LessOrEqual(t, len(discoveryInstructions(mode)), hostInstructionBudget, mode)
	}
}

func TestRuntimeServesDiscoverySurfaceToFlaggedHarness(t *testing.T) {
	t.Parallel()

	runtime := newDiscoveryTestRuntime(t, []authz.Grant{authz.NewGrant(authz.ScopeRoot, authz.WildcardResource)})
	handler := runtime.Handler()
	toolsList := func(clientID string) []string {
		var list struct {
			Tools []*mcp.Tool `json:"tools"`
		}
		allowlistRPC(t, handler, runtime, clientID, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, &list)
		return toolNames(list.Tools)
	}
	require.ElementsMatch(t, []string{searchPlatformToolsName, describePlatformToolName, callPlatformToolName}, toolsList(testCodexClientID))
	require.ElementsMatch(t, names(runtime.registrar.For(AudienceExternal)), toolsList(testOtherClientID), "other clients keep the full catalogue")

	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`
	var codex struct {
		Instructions string `json:"instructions"`
	}
	allowlistRPC(t, handler, runtime, testCodexClientID, initialize, &codex)
	require.Equal(t, discoveryInstructions(discoveryProgressive), codex.Instructions)
}

func TestSearchAndDescribeHideUndiscoverableTools(t *testing.T) {
	t.Parallel()

	member := newDiscoveryTestRuntime(t, []authz.Grant{authz.NewGrant(authz.ScopeProjectRead, "project-1")})
	admin := adminOnlyToolName(t, member)

	search := discoveryCall[searchPlatformToolsOutput](t, member, searchPlatformToolsName, map[string]any{"query": admin})
	require.NotContains(t, summaryNames(search.Items), admin)

	described := discoveryCall[describePlatformToolOutput](t, member, describePlatformToolName, map[string]any{"names": []string{admin, "no_such_tool"}})
	require.Empty(t, described.Tools)
	require.ElementsMatch(t, []string{admin, "no_such_tool"}, described.NotFound, "a hidden tool reads like a missing one")

	called := allowlistRPCRaw(t, member.Handler(), member, testCodexClientID, callBody(callPlatformToolName, map[string]any{"name": admin, "arguments": map[string]any{}}))
	require.Nil(t, called.Error)
	var result mcp.CallToolResult
	require.NoError(t, json.Unmarshal(called.Result, &result))
	require.True(t, result.IsError)
}

func TestSearchFindsGoldenPathAndDescribeCarriesGuidance(t *testing.T) {
	t.Parallel()

	runtime := newDiscoveryTestRuntime(t, []authz.Grant{authz.NewGrant(authz.ScopeRoot, authz.WildcardResource)})

	search := discoveryCall[searchPlatformToolsOutput](t, runtime, searchPlatformToolsName, map[string]any{"query": "readiness of an MCP server"})
	require.Contains(t, summaryNames(search.Items), "get_mcp_readiness")

	described := discoveryCall[describePlatformToolOutput](t, runtime, describePlatformToolName, map[string]any{"names": []string{"get_mcp_readiness"}})
	require.Len(t, described.Tools, 1)
	require.NotEmpty(t, described.Tools[0].InputSchema)
	require.NotEmpty(t, described.Tools[0].Guidance)
	for _, paragraph := range described.Tools[0].Guidance {
		require.Contains(t, paragraph, "get_mcp_readiness")
	}
}

func TestCallPlatformToolMatchesDirectCall(t *testing.T) {
	t.Parallel()

	runtime := newDiscoveryTestRuntime(t, []authz.Grant{authz.NewGrant(authz.ScopeRoot, authz.WildcardResource)})
	handler := runtime.Handler()

	direct := allowlistRPCRaw(t, handler, runtime, testOtherClientID, callBody("get_platform_context", map[string]any{}))
	require.Nil(t, direct.Error)
	viaMeta := allowlistRPCRaw(t, handler, runtime, testCodexClientID, callBody(callPlatformToolName, map[string]any{"name": "get_platform_context"}))
	require.Nil(t, viaMeta.Error)
	require.JSONEq(t, string(direct.Result), string(viaMeta.Result))

	invalid := allowlistRPCRaw(t, handler, runtime, testCodexClientID, callBody(callPlatformToolName, map[string]any{"name": "get_platform_context", "arguments": map[string]any{"unexpected": true}}))
	require.Nil(t, invalid.Error)
	var result mcp.CallToolResult
	require.NoError(t, json.Unmarshal(invalid.Result, &result))
	require.True(t, result.IsError, "invalid arguments come back as a fixable tool error")
}

// scriptedExecutor stands in for the sandbox: it runs a fixed Go program
// against the host the way a Python program would.
type scriptedExecutor struct {
	run func(context.Context, codemode.Host) (any, error)
}

func (scriptedExecutor) Enabled() bool                                { return true }
func (scriptedExecutor) Available() bool                              { return true }
func (scriptedExecutor) Cancel(context.Context, codemode.Scope) error { return nil }

func (e scriptedExecutor) Execute(ctx context.Context, _ codemode.Scope, _ string, factory codemode.HostFactory) (*codemode.ExecutionResult, error) {
	host, err := factory(ctx)
	if err != nil {
		return nil, err
	}
	value, err := e.run(ctx, host)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode value: %w", err)
	}
	return &codemode.ExecutionResult{Value: raw, ToolCalls: []codemode.CallOutcome{}}, nil
}

func TestExecuteReachesCatalogueThroughCodeMode(t *testing.T) {
	t.Parallel()

	executor := scriptedExecutor{run: func(ctx context.Context, host codemode.Host) (any, error) {
		page, err := host.Search(ctx, codemode.SearchArgs{Query: "platform context"})
		if err != nil {
			return nil, fmt.Errorf("search: %w", err)
		}
		description, err := host.Describe(ctx, codeToolPrefix+"get_platform_context")
		if err != nil {
			return nil, fmt.Errorf("describe: %w", err)
		}
		readiness, err := host.Describe(ctx, codeToolPrefix+"get_mcp_readiness")
		if err != nil {
			return nil, fmt.Errorf("describe readiness: %w", err)
		}
		result, err := host.Call(ctx, codeToolPrefix+"get_platform_context", json.RawMessage(`{}`))
		if err != nil {
			return nil, fmt.Errorf("call: %w", err)
		}
		return map[string]any{"page": page, "path": description.Path, "readiness": readiness.Definition, "ok": result.OK}, nil
	}}
	runtime := newAllowlistTestRuntime(t, []authz.Grant{authz.NewGrant(authz.ScopeRoot, authz.WildcardResource)}).WithDiscovery(DiscoveryConfig{
		Flags:             harnessFlags{"codex": feature.VariantPlatformMCPDiscoveryCodeMode},
		CodeExecutor:      executor,
		CodeModeAvailable: func(context.Context, string) bool { return true },
	})

	var list struct {
		Tools []*mcp.Tool `json:"tools"`
	}
	allowlistRPC(t, runtime.Handler(), runtime, testCodexClientID, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, &list)
	require.Contains(t, toolNames(list.Tools), executePlatformCodeName)

	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`
	var initialized struct {
		Instructions string `json:"instructions"`
	}
	allowlistRPC(t, runtime.Handler(), runtime, testCodexClientID, initialize, &initialized)
	require.Equal(t, discoveryInstructions(discoveryCode), initialized.Instructions)

	discover := `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{},"io.modelcontextprotocol/clientInfo":{"name":"test","version":"1"}}}}`
	var discovered struct {
		Instructions string `json:"instructions"`
		CacheScope   string `json:"cacheScope"`
	}
	allowlistRPC(t, runtime.Handler(), runtime, testCodexClientID, discover, &discovered, http.Header{"Mcp-Protocol-Version": {"2026-07-28"}, "Mcp-Method": {"server/discover"}})
	require.Equal(t, discoveryInstructions(discoveryCode), discovered.Instructions)
	require.Equal(t, "private", discovered.CacheScope)

	execution := discoveryCall[codemode.ExecutionResult](t, runtime, executePlatformCodeName, map[string]any{"code": "pass"})
	var value struct {
		Page      codemode.SearchPage `json:"page"`
		Path      string              `json:"path"`
		Readiness struct {
			Guidance []string `json:"guidance"`
		} `json:"readiness"`
		OK bool `json:"ok"`
	}
	require.NoError(t, json.Unmarshal(execution.Value, &value))
	require.True(t, slices.ContainsFunc(value.Page.Items, func(item codemode.Candidate) bool { return item.Path == codeToolPrefix+"get_platform_context" }))
	require.Equal(t, codeToolPrefix+"get_platform_context", value.Path)
	require.NotEmpty(t, value.Readiness.Guidance, "Code Mode describe carries the tool's rules")
	require.True(t, value.OK)
}

func newDiscoveryTestRuntime(t *testing.T, grants []authz.Grant) *Runtime {
	t.Helper()

	return newAllowlistTestRuntime(t, grants).WithDiscovery(DiscoveryConfig{
		Flags:             harnessFlags{"codex": feature.VariantPlatformMCPDiscoveryProgressive},
		CodeExecutor:      nil,
		CodeModeAvailable: nil,
	})
}

// discoveryCall calls one meta-tool as Codex and decodes its structured result.
func discoveryCall[Out any](t *testing.T, runtime *Runtime, name string, arguments map[string]any) Out {
	t.Helper()

	response := allowlistRPCRaw(t, runtime.Handler(), runtime, testCodexClientID, callBody(name, arguments))
	require.Nil(t, response.Error)
	var result struct {
		IsError           bool            `json:"isError"`
		StructuredContent json.RawMessage `json:"structuredContent"`
	}
	require.NoError(t, json.Unmarshal(response.Result, &result))
	require.False(t, result.IsError, string(response.Result))
	var out Out
	require.NoError(t, json.Unmarshal(result.StructuredContent, &out))
	return out
}

func callBody(name string, arguments map[string]any) string {
	params, err := json.Marshal(map[string]any{"name": name, "arguments": arguments})
	if err != nil {
		panic(err)
	}
	return `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":` + string(params) + `}`
}

func adminOnlyToolName(t *testing.T, runtime *Runtime) string {
	t.Helper()

	for _, descriptor := range runtime.registrar.For(AudienceExternal) {
		if descriptor.Meta.Authorization == ExternalAuthorizationOrgAdmin {
			return descriptor.Name
		}
	}
	t.Fatal("no admin-only external tool registered")
	return ""
}

func summaryNames(items []platformToolSummary) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Name)
	}
	return out
}

type failingFlags struct{ harnessFlags }

func (failingFlags) FlagVariantLocal(context.Context, feature.Flag, string, map[string]string, map[string]string) (feature.Variant, error) {
	return "", errors.New("posthog unavailable")
}

func TestDiscoveryModeFailsClosedToCatalogue(t *testing.T) {
	t.Parallel()

	failing := newAllowlistTestRuntime(t, nil).WithDiscovery(DiscoveryConfig{Flags: failingFlags{}, CodeExecutor: nil, CodeModeAvailable: nil})
	principal := testPrincipal()
	principal.ClientID = testCodexClientID
	require.Equal(t, discoveryFull, failing.discoveryModeFor(t.Context(), failing.registrar.logger, principal), "a flag error serves the catalogue")

	flagged := newDiscoveryTestRuntime(t, nil)
	principal.OrganizationID = ""
	require.Equal(t, discoveryFull, flagged.discoveryModeFor(t.Context(), flagged.registrar.logger, principal), "no organization serves the catalogue")
}

func TestFlaggedChatGPTReachesToolsOutsideAllowlist(t *testing.T) {
	t.Parallel()

	runtime := newAllowlistTestRuntime(t, []authz.Grant{authz.NewGrant(authz.ScopeRoot, authz.WildcardResource)}).WithDiscovery(DiscoveryConfig{
		Flags:             harnessFlags{"chatgpt": feature.VariantPlatformMCPDiscoveryProgressive},
		CodeExecutor:      nil,
		CodeModeAvailable: nil,
	})
	require.NotContains(t, chatGPTToolAllowlist, "get_platform_context")

	response := allowlistRPCRaw(t, runtime.Handler(), runtime, testChatGPTConnectorClientID, callBody(callPlatformToolName, map[string]any{"name": "get_platform_context"}))
	require.Nil(t, response.Error)
	var result mcp.CallToolResult
	require.NoError(t, json.Unmarshal(response.Result, &result))
	require.False(t, result.IsError, "the ChatGPT allowlist applies to the full catalogue only")
}

func TestDiscoverySurfaceProtocolEdges(t *testing.T) {
	t.Parallel()

	runtime := newDiscoveryTestRuntime(t, []authz.Grant{authz.NewGrant(authz.ScopeRoot, authz.WildcardResource)})
	handler := runtime.Handler()

	paged := allowlistRPCRaw(t, handler, runtime, testCodexClientID, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"cursor":"next"}}`)
	require.NotNil(t, paged.Error, "the small surface has one page")

	executed := allowlistRPCRaw(t, handler, runtime, testCodexClientID, callBody(executePlatformCodeName, map[string]any{"code": "pass"}))
	require.NotNil(t, executed.Error, "execute exists only in code mode")
	require.Equal(t, `unknown tool "execute"`, executed.Error.Message)

	discover := `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{},"io.modelcontextprotocol/clientInfo":{"name":"test","version":"1"}}}}`
	var discovered struct {
		Instructions string `json:"instructions"`
		CacheScope   string `json:"cacheScope"`
	}
	allowlistRPC(t, handler, runtime, testCodexClientID, discover, &discovered, http.Header{"Mcp-Protocol-Version": {"2026-07-28"}, "Mcp-Method": {"server/discover"}})
	require.Equal(t, discoveryInstructions(discoveryProgressive), discovered.Instructions)
	require.Equal(t, "private", discovered.CacheScope)
}

func TestDiscoveryToolsBoundTheirInputs(t *testing.T) {
	t.Parallel()

	runtime := newDiscoveryTestRuntime(t, []authz.Grant{authz.NewGrant(authz.ScopeRoot, authz.WildcardResource)})

	tooMany := make([]string, maxDescribeTools+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("tool_%d", i)
	}
	for name, arguments := range map[string]map[string]any{
		describePlatformToolName: {"names": tooMany},
		searchPlatformToolsName:  {"query": "x", "unexpected": true},
		callPlatformToolName:     {"name": "get_platform_context", "extra": 1},
	} {
		require.True(t, discoveryToolError(t, runtime, name, arguments), name)
	}

	first := discoveryCall[searchPlatformToolsOutput](t, runtime, searchPlatformToolsName, map[string]any{"query": "", "limit": 1})
	require.Len(t, first.Items, 1)
	require.NotEmpty(t, first.NextCursor)
	second := discoveryCall[searchPlatformToolsOutput](t, runtime, searchPlatformToolsName, map[string]any{"query": "", "limit": 1, "cursor": first.NextCursor})
	require.Len(t, second.Items, 1)
	require.NotEqual(t, first.Items[0].Name, second.Items[0].Name)
}

func TestExecuteCannotReachUndiscoverableTools(t *testing.T) {
	t.Parallel()

	var admin string
	executor := scriptedExecutor{run: func(ctx context.Context, host codemode.Host) (any, error) {
		page, err := host.Search(ctx, codemode.SearchArgs{Query: admin, Limit: 50})
		if err != nil {
			return nil, fmt.Errorf("search: %w", err)
		}
		_, describeErr := host.Describe(ctx, codeToolPrefix+admin)
		result, err := host.Call(ctx, codeToolPrefix+admin, json.RawMessage(`{}`))
		if err != nil {
			return nil, fmt.Errorf("call: %w", err)
		}
		return map[string]any{"page": page, "hidden": errors.Is(describeErr, codemode.ErrToolUnavailable), "ok": result.OK}, nil
	}}
	runtime := newCodeModeTestRuntime(t, []authz.Grant{authz.NewGrant(authz.ScopeProjectRead, "project-1")}, executor)
	admin = adminOnlyToolName(t, runtime)

	execution := discoveryCall[codemode.ExecutionResult](t, runtime, executePlatformCodeName, map[string]any{"code": "pass"})
	var value struct {
		Page   codemode.SearchPage `json:"page"`
		Hidden bool                `json:"hidden"`
		OK     bool                `json:"ok"`
	}
	require.NoError(t, json.Unmarshal(execution.Value, &value))
	require.False(t, slices.ContainsFunc(value.Page.Items, func(item codemode.Candidate) bool { return item.Path == codeToolPrefix+admin }))
	require.True(t, value.Hidden, "describe reads a hidden tool as unavailable")
	require.False(t, value.OK, "a hidden tool is refused before it runs")
}

func TestExecuteHonoursToolsAllowlist(t *testing.T) {
	t.Parallel()

	executor := scriptedExecutor{run: func(ctx context.Context, host codemode.Host) (any, error) {
		_, err := host.Call(ctx, codeToolPrefix+"get_platform_context", json.RawMessage(`{}`))
		return map[string]any{"refused": err != nil}, nil
	}}
	runtime := newCodeModeTestRuntime(t, []authz.Grant{authz.NewGrant(authz.ScopeRoot, authz.WildcardResource)}, executor)

	execution := discoveryCall[codemode.ExecutionResult](t, runtime, executePlatformCodeName, map[string]any{"code": "pass", "tools": []string{}})
	var value struct {
		Refused bool `json:"refused"`
	}
	require.NoError(t, json.Unmarshal(execution.Value, &value))
	require.True(t, value.Refused, "an empty tools list permits no tool")
}

// admissionExecutor refuses every program before it starts.
type admissionExecutor struct {
	scriptedExecutor
	err error
}

func (e admissionExecutor) Execute(context.Context, codemode.Scope, string, codemode.HostFactory) (*codemode.ExecutionResult, error) {
	return nil, e.err
}

func TestExecuteReportsRefusalsAsToolErrors(t *testing.T) {
	t.Parallel()

	runtime := newCodeModeTestRuntime(t, nil, scriptedExecutor{run: func(context.Context, codemode.Host) (any, error) { return nil, nil }})
	for _, arguments := range []map[string]any{
		{"code": ""},
		{"code": "pass", "tools": nil},
		{"code": "pass", "tools": "platform--get_platform_context"},
	} {
		require.True(t, discoveryToolError(t, runtime, executePlatformCodeName, arguments), arguments)
	}

	for err, want := range map[error]string{
		codemode.ErrCapacity:              "code_runtime_busy",
		codemode.ErrRuntimeUnavailable:    "code_runtime_unavailable",
		codemode.ErrCancelledBeforeSubmit: "code_execution_cancelled",
		errors.New("boom"):                "code_runtime_unavailable",
	} {
		busy := newCodeModeTestRuntime(t, nil, admissionExecutor{scriptedExecutor: scriptedExecutor{run: nil}, err: err})
		response := allowlistRPCRaw(t, busy.Handler(), busy, testCodexClientID, callBody(executePlatformCodeName, map[string]any{"code": "pass"}))
		require.Nil(t, response.Error)
		var result mcp.CallToolResult
		require.NoError(t, json.Unmarshal(response.Result, &result))
		require.True(t, result.IsError)
		require.Len(t, result.Content, 1)
		text, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		require.True(t, strings.HasPrefix(text.Text, want+":"), text.Text)
	}
}

func newCodeModeTestRuntime(t *testing.T, grants []authz.Grant, executor codemode.Executor) *Runtime {
	t.Helper()

	return newAllowlistTestRuntime(t, grants).WithDiscovery(DiscoveryConfig{
		Flags:             harnessFlags{"codex": feature.VariantPlatformMCPDiscoveryCodeMode},
		CodeExecutor:      executor,
		CodeModeAvailable: func(context.Context, string) bool { return true },
	})
}

// discoveryToolError calls one meta-tool as Codex and reports whether it
// answered with a tool error rather than a result.
func discoveryToolError(t *testing.T, runtime *Runtime, name string, arguments map[string]any) bool {
	t.Helper()

	response := allowlistRPCRaw(t, runtime.Handler(), runtime, testCodexClientID, callBody(name, arguments))
	if response.Error != nil {
		return true
	}
	var result mcp.CallToolResult
	require.NoError(t, json.Unmarshal(response.Result, &result))
	return result.IsError
}
