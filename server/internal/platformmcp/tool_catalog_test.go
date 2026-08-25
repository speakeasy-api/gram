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
	require.JSONEq(t, `{"code":"feature_unavailable","reason":"remote_inspection_unavailable","message":"The remote MCP could not be inspected safely right now. Retry after a short delay."}`, refusal.Payload)
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
