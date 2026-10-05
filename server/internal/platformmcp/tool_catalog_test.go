package platformmcp

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCandidateInspectionIncludesCatalogSetupIntent(t *testing.T) {
	t.Parallel()

	server := newTestMCPServer()
	registrar := newRegistrar(server)
	catalog := testCatalog{details: CatalogDetails{
		CatalogCandidate: CatalogCandidate{
			ProviderKey: "provider",
			CatalogRef:  "reviewed/mcp",
			SetupIntent: "dashboard_source_settings",
		},
		Transport: "streamable-http",
	}}
	registerCandidateInspectionTool(registrar, catalog, nil, &testRegistrationGate{enabled: true}, allowBudget())

	result, err := catalogInspectionDescriptor(t, registrar).Invoke(
		ContextWithPrincipal(t.Context(), registrationServicePrincipal()),
		json.RawMessage(`{"provider_key":"provider","catalog_ref":"reviewed/mcp"}`),
	)

	require.NoError(t, err)
	inspection, ok := result.(CandidateInspection)
	require.True(t, ok)
	require.Equal(t, "dashboard_source_settings", inspection.SetupIntent)
}

func TestCandidateInspectionReturnsSafeDirectRemoteErrors(t *testing.T) {
	t.Parallel()

	server := newTestMCPServer()
	registrar := newRegistrar(server)
	registerCandidateInspectionTool(
		registrar,
		nil,
		&testDirectRemoteInspector{err: fmtDirectRemoteInspectionError()},
		&testRegistrationGate{enabled: true},
		allowBudget(),
	)

	_, err := catalogInspectionDescriptor(t, registrar).Invoke(
		ContextWithPrincipal(t.Context(), registrationServicePrincipal()),
		json.RawMessage(`{"remote_url":"https://remote.example.test/mcp"}`),
	)

	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	require.JSONEq(t, `{"code":"feature_unavailable","reason":"remote_inspection_unavailable","setup_category":"temporarily_unavailable","actions":[{"kind":"retry_inspection","label":"Check this MCP server again shortly"}],"message":"That MCP server could not be checked safely right now. Try again shortly."}`, refusal.Payload)
	require.NotContains(t, refusal.Payload, "unsafe network detail")
}

func TestCandidateInspectionReturnsOAuthSetupCategoryAndActions(t *testing.T) {
	t.Parallel()

	server := newTestMCPServer()
	registrar := newRegistrar(server)
	registerCandidateInspectionTool(
		registrar,
		nil,
		&testDirectRemoteInspector{inspection: DirectRemoteInspection{
			CanonicalURL:           "https://remote.example.test/mcp",
			Transport:              "streamable-http",
			Authentication:         "authentication_required",
			OAuthDiscovery:         "available",
			Trust:                  "user_supplied_unreviewed",
			RequiresDashboardSetup: true,
		}},
		&testRegistrationGate{enabled: true},
		allowBudget(),
	)

	result, err := catalogInspectionDescriptor(t, registrar).Invoke(
		ContextWithPrincipal(t.Context(), registrationServicePrincipal()),
		json.RawMessage(`{"remote_url":"https://remote.example.test/mcp"}`),
	)

	require.NoError(t, err)
	inspection, ok := result.(CandidateInspection)
	require.True(t, ok)
	require.Equal(t, SetupCategoryDynamicRegistrationUnsupported, inspection.SetupCategory)
	require.Equal(t, []RepairAction{{Kind: "continue_registration", Label: "Choose a project and add this MCP server before finishing its sign-in setup"}}, inspection.Actions)
}

func fmtDirectRemoteInspectionError() error {
	return errors.New("unsafe network detail that must not reach the caller")
}

func catalogInspectionDescriptor(t *testing.T, registrar *Registrar) Descriptor {
	t.Helper()
	for _, descriptor := range registrar.Descriptors() {
		if descriptor.Name == "inspect_mcp_candidate" {
			return descriptor
		}
	}
	require.FailNow(t, "inspect_mcp_candidate descriptor was not registered")
	return Descriptor{}
}

func TestCandidateInspectionDirectRemoteProtocols(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		mode           string
		authentication string
	}{
		{"modern", "anonymous"},
		{"legacy", "anonymous"},
		{"stateful", "anonymous"},
		{"auth401", "authentication_required"},
		{"auth403", "authentication_required"},
		{"tools-auth", "authentication_required"},
		{"discover-auth", "authentication_required"},
		{"stateful-auth-cleanup", "authentication_required"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			t.Parallel()
			inspector, _ := directRemoteProtocolFixture(t, tc.mode)
			registrar := newRegistrar(newTestMCPServer())
			registerCandidateInspectionTool(registrar, nil, inspector, &testRegistrationGate{enabled: true}, allowBudget())
			result, err := catalogInspectionDescriptor(t, registrar).Invoke(ContextWithPrincipal(t.Context(), registrationServicePrincipal()), json.RawMessage(`{"remote_url":"https://remote.example.test/mcp"}`))
			require.NoError(t, err)
			inspection, ok := result.(CandidateInspection)
			require.True(t, ok)
			require.Equal(t, "https://remote.example.test/mcp", inspection.CanonicalURL)
			require.Equal(t, tc.authentication, inspection.Authentication)
			if tc.authentication == "authentication_required" {
				require.Equal(t, "available_dcr", inspection.OAuthDiscovery)
				require.True(t, inspection.RequiresDashboardSetup)
			} else {
				require.Equal(t, []string{"example"}, inspection.ToolNames)
			}
		})
	}
}
