package platformmcp

// These goldens were first generated from the Platform MCP code that read the
// compiled registry directly, before it read an injected store, which
// established equivalence. A registry change needs `-args -update`.

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	agentrepo "github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/aivendors/aivendorstest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/codemode"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
)

// goldenCatalogClientIDs are every catalog document URL, variants of each,
// wildcard probes, refused inputs and four test clients.
func goldenCatalogClientIDs(t *testing.T) []string {
	t.Helper()

	// Frozen from cimd/admission/testdata/admission_golden.json plus the test clients.
	data, err := os.ReadFile("testdata/ai_catalog_client_ids.json")
	require.NoError(t, err)
	var ids []string
	require.NoError(t, json.Unmarshal(data, &ids))
	return ids
}

// everyHarness serves one discovery variant to every harness.
func everyHarness(variant feature.Variant) harnessFlags {
	flags := harnessFlags{"unknown": variant}
	for _, product := range aivendors.CompiledSnapshot().Products() {
		flags[product.ID] = variant
	}
	return flags
}

// goldenClientDecision is what Platform MCP decides from one client ID.
type goldenClientDecision struct {
	ClientID     string        `json:"client_id"`
	ChatGPT      bool          `json:"chatgpt"`
	Allowlist    []string      `json:"allowlist"`
	Harness      string        `json:"harness"`
	Progressive  discoveryMode `json:"progressive"`
	CodeMode     discoveryMode `json:"code_mode"`
	ReadinessApp string        `json:"readiness_app"`
}

func TestClientCatalogDecisionsMatchGolden(t *testing.T) {
	t.Parallel()

	runtimeFor := func(variant feature.Variant) *Runtime {
		return newAllowlistTestRuntime(t, nil).WithDiscovery(DiscoveryConfig{
			Flags:             everyHarness(variant),
			CodeExecutor:      codemode.Disabled{},
			CodeModeAvailable: func(_ context.Context, _ string) bool { return true },
		})
	}
	progressive := runtimeFor(feature.VariantPlatformMCPDiscoveryProgressive)
	codeMode := runtimeFor(feature.VariantPlatformMCPDiscoveryCodeMode)

	catalog := aivendors.CompiledSnapshot()
	ids := goldenCatalogClientIDs(t)
	decisions := make([]goldenClientDecision, 0, len(ids))
	for _, id := range ids {
		principal := testPrincipal()
		principal.ClientID = id
		decisions = append(decisions, goldenClientDecision{
			ClientID:     id,
			ChatGPT:      isChatGPTClient(catalog, id),
			Allowlist:    clientToolAllowlist(catalog, id),
			Harness:      harnessForClient(catalog, id),
			Progressive:  progressive.discoveryModeFor(t.Context(), progressive.registrar.logger, principal),
			CodeMode:     codeMode.discoveryModeFor(t.Context(), codeMode.registrar.logger, principal),
			ReadinessApp: mcpconnectionattempts.AppOf(catalog, id, mcpconnectionattempts.ClientIDAdmitted).String(),
		})
	}
	data, err := json.Marshal(decisions)
	require.NoError(t, err)
	testenv.RequireJSONGolden(t, "testdata/ai_catalog_client_decisions_golden.json", data)
}

// goldenClientSurface is the full-catalogue surface one client is served.
type goldenClientSurface struct {
	ClientID     string   `json:"client_id"`
	Tools        []string `json:"tools"`
	Instructions string   `json:"instructions"`
	HiddenCall   string   `json:"hidden_call_error"`
}

func TestClientToolSurfacesMatchGolden(t *testing.T) {
	t.Parallel()

	runtime := newAllowlistTestRuntime(t, []authz.Grant{authz.NewGrant(authz.ScopeRoot, authz.WildcardResource)})
	handler := runtime.Handler()
	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`
	call := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_platform_context","arguments":{}}}`

	var surfaces []goldenClientSurface
	for _, id := range []string{testChatGPTConnectorClientID, testCodexClientID, testOtherClientID} {
		var list struct {
			Tools []*mcp.Tool `json:"tools"`
		}
		allowlistRPC(t, handler, runtime, id, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, &list)
		var initialized struct {
			Instructions string `json:"instructions"`
		}
		allowlistRPC(t, handler, runtime, id, initialize, &initialized)
		surface := goldenClientSurface{ClientID: id, Tools: toolNames(list.Tools), Instructions: initialized.Instructions, HiddenCall: ""}
		if response := allowlistRPCRaw(t, handler, runtime, id, call); response.Error != nil {
			surface.HiddenCall = response.Error.Message
		}
		surfaces = append(surfaces, surface)
	}
	data, err := json.Marshal(surfaces)
	require.NoError(t, err)
	testenv.RequireJSONGolden(t, "testdata/ai_catalog_client_surfaces_golden.json", data)
}

// The runtime reads the store it was given: an empty catalog recognizes no
// ChatGPT client, so ChatGPT keeps the full catalogue.
func TestRuntimeReadsTheInjectedCatalog(t *testing.T) {
	t.Parallel()

	runtime := newAllowlistTestRuntime(t, []authz.Grant{authz.NewGrant(authz.ScopeRoot, authz.WildcardResource)}).WithAICatalog(aivendors.NewStore(aivendors.NewSnapshot(nil)))
	var list struct {
		Tools []*mcp.Tool `json:"tools"`
	}
	allowlistRPC(t, runtime.Handler(), runtime, testChatGPTConnectorClientID, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, &list)
	require.ElementsMatch(t, names(runtime.registrar.For(AudienceExternal)), toolNames(list.Tools))
	require.Panics(t, func() { newAllowlistTestRuntime(t, nil).WithAICatalog(nil) })
}

// goldenOAuthAdmission is how the Platform MCP authorization server admits one
// client ID and what it records about it.
type goldenOAuthAdmission struct {
	ClientID string                 `json:"client_id"`
	Denial   admission.DenialReason `json:"denial"`
	Shadow   admission.AdmitReason  `json:"shadow"`
}

func TestOAuthClientAdmissionMatchesGolden(t *testing.T) {
	t.Parallel()

	service := newTestOAuthHTTP(t)
	catalog := service.aiCatalog.Snapshot()
	ids := goldenCatalogClientIDs(t)
	admissions := make([]goldenOAuthAdmission, 0, len(ids))
	for _, id := range ids {
		result := goldenOAuthAdmission{ClientID: id, Denial: "", Shadow: service.shadowCIMDAdmitReason(t.Context(), catalog, id)}
		if err := service.admitCIMDClient(t.Context(), id); err != nil {
			var denial *admission.DenialError
			require.ErrorAs(t, err, &denial)
			result.Denial = denial.Reason
		}
		admissions = append(admissions, result)
	}
	data, err := json.Marshal(admissions)
	require.NoError(t, err)
	testenv.RequireJSONGolden(t, "testdata/ai_catalog_oauth_admission_golden.json", data)
}

func TestInboundEMAPresetClientsMatchGolden(t *testing.T) {
	t.Parallel()

	data, err := json.Marshal(inboundEMAPresetClients(aivendors.CompiledSnapshot()))
	require.NoError(t, err)
	testenv.RequireJSONGolden(t, "testdata/ai_catalog_inbound_ema_presets_golden.json", data)
}

// An organization with no overlay is listed the catalog's scan targets.
func TestAIScanLibraryWithoutOverlayMatchesGolden(t *testing.T) {
	t.Parallel()

	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_ai_catalog_golden")
	require.NoError(t, err)
	service := NewShadowAIService(&stubDetectionReader{}, NewPostgresShadowAILibrary(aivendorstest.NewStore(), agentrepo.New(conn)), stubAuthorizer{}, allowingBudget())
	output, err := service.ListLibrary(t.Context(), shadowAIPrincipal(), ListAIScanLibraryInput{Category: ""})
	require.NoError(t, err)
	data, err := json.Marshal(output)
	require.NoError(t, err)
	testenv.RequireJSONGolden(t, "testdata/ai_catalog_scan_library_golden.json", data)
}

// server/discover filters the instructions the way initialize does.
func TestDiscoverInstructionsMatchGolden(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("testdata/ai_catalog_client_surfaces_golden.json")
	require.NoError(t, err)
	var surfaces []goldenClientSurface
	require.NoError(t, json.Unmarshal(data, &surfaces))
	require.Len(t, surfaces, 3)

	runtime := newAllowlistTestRuntime(t, []authz.Grant{authz.NewGrant(authz.ScopeRoot, authz.WildcardResource)})
	handler := runtime.Handler()
	discover := `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{},"io.modelcontextprotocol/clientInfo":{"name":"test","version":"1"}}}}`
	header := http.Header{"Mcp-Protocol-Version": {"2026-07-28"}, "Mcp-Method": {"server/discover"}}
	for _, surface := range surfaces {
		var discovered struct {
			Instructions string `json:"instructions"`
		}
		allowlistRPC(t, handler, runtime, surface.ClientID, discover, &discovered, header)
		require.Equal(t, surface.Instructions, discovered.Instructions, surface.ClientID)
	}
}

// get_inbound_ema_trust lists the catalog's enabled presets in order.
func TestInboundEMATrustToolPresetClientsMatchGolden(t *testing.T) {
	t.Parallel()

	f := newInboundEMAFixture(t, 1)
	reg := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "inbound-ema", Version: "1"}, nil))
	reg.withAICatalog(aivendorstest.NewStore())
	registerInboundEMATrustTools(reg, f.service)
	result, err := descriptorByName(t, reg, getInboundEMATrustToolName).Invoke(contextWithPrincipal(t.Context(), f.principal), json.RawMessage(`{}`))
	require.NoError(t, err)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	var output struct {
		StructuredContent struct {
			PresetClients json.RawMessage `json:"preset_clients"`
		} `json:"structuredContent"`
		PresetClients json.RawMessage `json:"preset_clients"`
	}
	require.NoError(t, json.Unmarshal(encoded, &output))
	presets := output.PresetClients
	if presets == nil {
		presets = output.StructuredContent.PresetClients
	}
	require.NotNil(t, presets, string(encoded))
	testenv.RequireJSONGolden(t, "testdata/ai_catalog_inbound_ema_presets_golden.json", presets)
}

// A runtime composed without a catalog fails when it is attached, not on its
// first request.
func TestRuntimeWithoutCatalogFailsAtComposition(t *testing.T) {
	t.Parallel()

	runtime := NewRuntime(testenv.NewLogger(t), &testAuthenticator{principal: testPrincipal()}, testGate{enabled: true}, &testAuthorizer{grants: nil}, "", "test-cursor-key", nil, nil, nil, nil, nil)
	require.PanicsWithValue(t, "platformmcp: runtime composed without an AI client catalog store; call WithAICatalog", func() { runtime.Handler() })
	require.Panics(t, func() { runtime.AssistantTools() })
	require.Panics(t, func() { NewRemoteMCPReadinessProber(nil, testenv.NewLogger(t), nil, nil, nil, nil) })
	require.Panics(t, func() { NewPostgresShadowAILibrary(nil, nil) })
}
