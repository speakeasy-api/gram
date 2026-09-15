package mcptoolexecution

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/killswitches"
)

// TestMCPToolExecutionRegistration proves the checked-in registration builds
// a complete, fail-closed registry: registry construction validates the
// canonicalization fixtures and the coverage inventory, so a green build here
// is the startup-validation guarantee.
func TestMCPToolExecutionRegistration(t *testing.T) {
	t.Parallel()

	registry, err := NewRegistry(nil)
	require.NoError(t, err)

	definition, ok := registry.Definition(DefinitionKeyMCPToolExecution)
	require.True(t, ok)
	require.Equal(t, killswitches.FailurePolicyFailClosed, definition.FailurePolicy)
	require.Equal(t, []killswitches.PrincipalKind{PrincipalKindUser}, definition.PrincipalKinds)
	require.Equal(t, []killswitches.ResourceKind{ResourceKindMCPServer}, definition.ResourceKinds)
	require.Equal(t, IdentityContractKeyAuthenticatedUserMCPServer, definition.IdentityContract)
	require.ElementsMatch(t, []killswitches.Surface{SurfaceHostedToolsCall, SurfacePrivateProxyToolsCall}, definition.Surfaces)
	require.ElementsMatch(t, []killswitches.TransportAdapterKey{TransportAdapterHostedJSONRPC, TransportAdapterPrivateProxyJSONRPC}, definition.TransportAdapters)
	require.NotEmpty(t, definition.DefaultExternalNote)
	require.NotEmpty(t, definition.EnforcementOwner)

	adapter, ok := registry.PrincipalAdapter(PrincipalKindUser)
	require.True(t, ok)
	require.Equal(t, PrincipalKindUser, adapter.Kind())
	resourceAdapter, ok := registry.ResourceAdapter(ResourceKindMCPServer)
	require.True(t, ok)
	require.Equal(t, ResourceKindMCPServer, resourceAdapter.Kind())
}

// TestMCPCoverageInventory proves each declared surface has its own coverage
// entry with the fail-closed policy, and that the exclusion inventory names
// every serving mode M2 deliberately leaves uncovered.
func TestMCPCoverageInventory(t *testing.T) {
	t.Parallel()

	registry, err := NewRegistry(nil)
	require.NoError(t, err)

	inventory := registry.CoverageInventory()
	require.Len(t, inventory, 2)
	for _, contract := range inventory {
		require.Equal(t, DefinitionKeyMCPToolExecution, contract.Definition)
		require.Equal(t, killswitches.FailurePolicyFailClosed, contract.FailurePolicy)
		require.Equal(t, IdentityContractKeyAuthenticatedUserMCPServer, contract.IdentityContract)
		require.NotEmpty(t, contract.PrincipalSource)
		require.NotEmpty(t, contract.ResourceSource)
		require.NotEmpty(t, contract.Checkpoint)
		require.NotEmpty(t, contract.ProtectedWork)
	}

	hosted, ok := registry.Coverage(DefinitionKeyMCPToolExecution, SurfaceHostedToolsCall)
	require.True(t, ok)
	require.Equal(t, TransportAdapterHostedJSONRPC, hosted.TransportAdapter)
	require.Contains(t, hosted.ProtectedWork, "resolving protected credentials")

	proxy, ok := registry.Coverage(DefinitionKeyMCPToolExecution, SurfacePrivateProxyToolsCall)
	require.True(t, ok)
	require.Equal(t, TransportAdapterPrivateProxyJSONRPC, proxy.TransportAdapter)

	excluded := ExcludedMCPSurfaces()
	names := make([]string, 0, len(excluded))
	for _, surface := range excluded {
		require.NotEmpty(t, surface.Reason)
		names = append(names, surface.Name)
	}
	for _, expected := range []string{
		"legacy toolset-only /mcp/{slug} fallback",
		"meta MCP endpoints",
		"public anonymous MCP",
		"platform MCP (/platform/mcp)",
		"internal direct tool callers (HandleToolsCall / HandleToolsList)",
		"API-key and chat-session authenticated calls",
	} {
		require.Contains(t, names, expected)
	}
}

// TestMCPToolExecutionTransportAdaptersFailClosed proves the registered
// neutral transport behavior under the fail-closed policy: a match denies
// with only the external note, and an infrastructure failure denies without
// any match language.
func TestMCPToolExecutionTransportAdaptersFailClosed(t *testing.T) {
	t.Parallel()

	registry, err := NewRegistry(nil)
	require.NoError(t, err)

	match, err := killswitches.NewMatchResult(killswitches.PrescriptionID("0198a1b2-c3d4-7000-8000-0123456789ab"), "Paused by your administrator.")
	require.NoError(t, err)
	noMatch, err := killswitches.NewNoMatchResult(killswitches.NoMatchReasonUnsupportedIdentity)
	require.NoError(t, err)
	failure, err := killswitches.NewInfrastructureFailureResult(errors.New("database unavailable"))
	require.NoError(t, err)

	for _, key := range []killswitches.TransportAdapterKey{TransportAdapterHostedJSONRPC, TransportAdapterPrivateProxyJSONRPC} {
		adapter, ok := registry.TransportAdapter(key)
		require.True(t, ok, string(key))

		disposition, err := adapter(match, killswitches.FailurePolicyFailClosed)
		require.NoError(t, err)
		require.Equal(t, killswitches.TransportDispositionMatchedDenial, disposition.Kind())
		note, ok := disposition.ExternalNote()
		require.True(t, ok)
		require.Equal(t, "Paused by your administrator.", note)

		disposition, err = adapter(noMatch, killswitches.FailurePolicyFailClosed)
		require.NoError(t, err)
		require.Equal(t, killswitches.TransportDispositionContinue, disposition.Kind())

		disposition, err = adapter(failure, killswitches.FailurePolicyFailClosed)
		require.NoError(t, err)
		require.Equal(t, killswitches.TransportDispositionInfrastructureRejection, disposition.Kind())
		_, hasNote := disposition.ExternalNote()
		require.False(t, hasNote)
	}
}
