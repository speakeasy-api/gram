package platformmcp

// These goldens were first generated from the Platform MCP code that read the
// compiled registry directly, before it read an injected store, which
// established equivalence. A registry change needs `-args -update`.

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	agentrepo "github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/codemode"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
)

// goldenCatalogClientIDs reuses the client IDs of the admission golden: every
// catalog document URL, variants of each, wildcard probes and refused inputs.
func goldenCatalogClientIDs(t *testing.T) []string {
	t.Helper()

	data, err := os.ReadFile("../usersessions/cimd/admission/testdata/admission_golden.json")
	require.NoError(t, err)
	var cases []struct {
		ClientID string `json:"client_id"`
	}
	require.NoError(t, json.Unmarshal(data, &cases))
	ids := make([]string, 0, len(cases)+4)
	for _, tc := range cases {
		ids = append(ids, tc.ClientID)
	}
	for _, id := range []string{testChatGPTConnectorClientID, testCodexClientID, testOtherClientID, "00000000-0000-0000-0000-000000000000"} {
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
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

	ids := goldenCatalogClientIDs(t)
	decisions := make([]goldenClientDecision, 0, len(ids))
	for _, id := range ids {
		principal := testPrincipal()
		principal.ClientID = id
		decisions = append(decisions, goldenClientDecision{
			ClientID:     id,
			ChatGPT:      isChatGPTClient(id),
			Allowlist:    clientToolAllowlist(id),
			Harness:      harnessForClient(id),
			Progressive:  progressive.discoveryModeFor(t.Context(), progressive.registrar.logger, principal),
			CodeMode:     codeMode.discoveryModeFor(t.Context(), codeMode.registrar.logger, principal),
			ReadinessApp: mcpconnectionattempts.AppOf(aivendors.CompiledSnapshot(), id, mcpconnectionattempts.ClientIDAdmitted).String(),
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
	ids := goldenCatalogClientIDs(t)
	admissions := make([]goldenOAuthAdmission, 0, len(ids))
	for _, id := range ids {
		result := goldenOAuthAdmission{ClientID: id, Denial: "", Shadow: service.shadowCIMDAdmitReason(t.Context(), id)}
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

	data, err := json.Marshal(inboundEMAPresetClients())
	require.NoError(t, err)
	testenv.RequireJSONGolden(t, "testdata/ai_catalog_inbound_ema_presets_golden.json", data)
}

// An organization with no overlay is listed the catalog's scan targets.
func TestAIScanLibraryWithoutOverlayMatchesGolden(t *testing.T) {
	t.Parallel()

	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_ai_catalog_golden")
	require.NoError(t, err)
	service := NewShadowAIService(&stubDetectionReader{}, NewPostgresShadowAILibrary(agentrepo.New(conn)), stubAuthorizer{}, allowingBudget())
	output, err := service.ListLibrary(t.Context(), shadowAIPrincipal(), ListAIScanLibraryInput{Category: ""})
	require.NoError(t, err)
	data, err := json.Marshal(output)
	require.NoError(t, err)
	testenv.RequireJSONGolden(t, "testdata/ai_catalog_scan_library_golden.json", data)
}
