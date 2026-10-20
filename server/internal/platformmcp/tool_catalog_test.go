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
	require.Equal(t, AutomaticClientRegistrationNone, inspection.AutomaticClientRegistration)
	require.Equal(t, []RepairAction{{Kind: "continue_registration", Label: "Choose a project and add this MCP server before finishing its sign-in setup"}}, inspection.Actions)
}

// The inspector must not report manual setup for a provider that offers an
// automatic client registration path, and must say which path applies.
func TestCandidateInspectionReportsAutomaticClientRegistrationPath(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		oauthDiscovery string
		category       SetupCategory
		registration   string
	}{
		{oauthDiscovery: "available_dcr", category: SetupCategoryAuthenticationRequired, registration: AutomaticClientRegistrationDCR},
		{oauthDiscovery: "available_cimd", category: SetupCategoryAuthenticationRequired, registration: AutomaticClientRegistrationCIMD},
		{oauthDiscovery: "available", category: SetupCategoryDynamicRegistrationUnsupported, registration: AutomaticClientRegistrationNone},
	} {
		t.Run(test.oauthDiscovery, func(t *testing.T) {
			t.Parallel()
			registrar := newRegistrar(newTestMCPServer())
			registerCandidateInspectionTool(registrar, nil, &testDirectRemoteInspector{inspection: DirectRemoteInspection{
				CanonicalURL:           "https://remote.example.test/mcp",
				Transport:              "streamable-http",
				Authentication:         "authentication_required",
				OAuthDiscovery:         test.oauthDiscovery,
				Trust:                  "user_supplied_unreviewed",
				RequiresDashboardSetup: true,
			}}, &testRegistrationGate{enabled: true}, allowBudget())

			result, err := catalogInspectionDescriptor(t, registrar).Invoke(ContextWithPrincipal(t.Context(), registrationServicePrincipal()), json.RawMessage(`{"remote_url":"https://remote.example.test/mcp"}`))
			require.NoError(t, err)
			inspection, ok := result.(CandidateInspection)
			require.True(t, ok)
			require.Equal(t, test.category, inspection.SetupCategory)
			require.Equal(t, test.registration, inspection.AutomaticClientRegistration)
			encoded, err := json.Marshal(inspection)
			require.NoError(t, err)
			require.Contains(t, string(encoded), `"automatic_client_registration":"`+test.registration+`"`)
		})
	}
}

// End to end through the real inspector: a protected MCP whose authorization
// server has no registration_endpoint but supports Client ID Metadata
// Documents is automatic sign-in setup; one offering neither stays manual.
func TestCandidateInspectionDirectRemoteClientRegistrationPaths(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		mode           string
		oauthDiscovery string
		category       SetupCategory
		registration   string
	}{
		{mode: "auth401", oauthDiscovery: "available_dcr", category: SetupCategoryAuthenticationRequired, registration: AutomaticClientRegistrationDCR},
		{mode: "auth401-cimd", oauthDiscovery: "available_cimd", category: SetupCategoryAuthenticationRequired, registration: AutomaticClientRegistrationCIMD},
		{mode: "auth401-manual", oauthDiscovery: "available", category: SetupCategoryDynamicRegistrationUnsupported, registration: AutomaticClientRegistrationNone},
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
			require.Equal(t, "authentication_required", inspection.Authentication)
			require.Equal(t, tc.oauthDiscovery, inspection.OAuthDiscovery)
			require.Equal(t, tc.category, inspection.SetupCategory)
			require.Equal(t, tc.registration, inspection.AutomaticClientRegistration)
		})
	}
}

func TestCandidateInspectionOmitsAutomaticClientRegistrationForAnonymous(t *testing.T) {
	t.Parallel()

	registrar := newRegistrar(newTestMCPServer())
	registerCandidateInspectionTool(registrar, nil, &testDirectRemoteInspector{inspection: DirectRemoteInspection{
		CanonicalURL:   "https://remote.example.test/mcp",
		Transport:      "streamable-http",
		Authentication: "anonymous",
		OAuthDiscovery: "not_advertised",
		Trust:          "user_supplied_unreviewed",
	}}, &testRegistrationGate{enabled: true}, allowBudget())

	result, err := catalogInspectionDescriptor(t, registrar).Invoke(ContextWithPrincipal(t.Context(), registrationServicePrincipal()), json.RawMessage(`{"remote_url":"https://remote.example.test/mcp"}`))
	require.NoError(t, err)
	inspection, ok := result.(CandidateInspection)
	require.True(t, ok)
	require.Empty(t, inspection.AutomaticClientRegistration)
	require.Empty(t, inspection.SetupCategory)
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
				require.Equal(t, SetupCategoryAuthenticationRequired, inspection.SetupCategory)
				require.Equal(t, "available_dcr", inspection.OAuthDiscovery)
				require.Equal(t, AutomaticClientRegistrationDCR, inspection.AutomaticClientRegistration)
				require.True(t, inspection.RequiresDashboardSetup)
			} else {
				require.Equal(t, []string{"example"}, inspection.ToolNames)
			}
		})
	}
}
