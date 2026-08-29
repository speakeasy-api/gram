package mcptoolexecution

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/delegation"
	"github.com/speakeasy-api/gram/server/internal/killswitches"
)

// TestMCPToolExecutionRegistration proves the checked-in registration builds
// a complete, fail-closed registry: registry construction validates the
// canonicalization fixtures and the coverage inventory, so a green build here
// is the startup-validation guarantee.
func TestHookSurfaceRejectsUnknownBinding(t *testing.T) {
	t.Parallel()

	_, err := hookSurface(delegation.Binding{Provider: "unknown", Event: "unknown"})
	require.ErrorContains(t, err, "has no registered surface")
}

func TestMCPToolExecutionRegistration(t *testing.T) {
	t.Parallel()

	registry, err := NewRegistry(nil)
	require.NoError(t, err)
	require.Len(t, registry.Definitions(), 2)

	definition, ok := registry.Definition(DefinitionKeyMCPToolExecution)
	require.True(t, ok)
	require.Equal(t, killswitches.FailurePolicyFailClosed, definition.FailurePolicy)
	require.Equal(t, []killswitches.PrincipalKind{PrincipalKindUser}, definition.PrincipalKinds)
	require.Equal(t, []killswitches.ResourceKind{ResourceKindMCPServer}, definition.ResourceKinds)
	require.Equal(t, IdentityContractKeyAuthenticatedUserMCPServer, definition.IdentityContract)
	require.ElementsMatch(t, []killswitches.Surface{SurfaceHostedToolsCall, SurfacePrivateProxyToolsCall}, definition.Surfaces)
	require.ElementsMatch(t, []killswitches.TransportAdapterKey{TransportAdapterHostedJSONRPC, TransportAdapterPrivateProxyJSONRPC}, definition.TransportAdapters)
	require.Equal(t, DefaultExternalNote, definition.DefaultExternalNote)
	require.Equal(t, EnforcementOwner, definition.EnforcementOwner)

	aiDefinition, ok := registry.Definition(DefinitionKeyAIAccess)
	require.True(t, ok)
	require.Equal(t, killswitches.FailurePolicyFailClosed, aiDefinition.FailurePolicy)
	require.Equal(t, []killswitches.PrincipalKind{PrincipalKindUser}, aiDefinition.PrincipalKinds)
	require.ElementsMatch(t, []killswitches.ResourceKind{ResourceKindMCPServer, ResourceKindHookActivity}, aiDefinition.ResourceKinds)
	require.Equal(t, IdentityContractKeyAuthenticatedUserAIResource, aiDefinition.IdentityContract)
	require.ElementsMatch(t, []killswitches.Surface{SurfaceHostedToolsCall, SurfacePrivateProxyToolsCall, SurfaceClaudeUserPromptSubmit, SurfaceClaudePreToolUse, SurfaceCodexUserPromptSubmit, SurfaceCodexPreToolUse}, aiDefinition.Surfaces)
	require.ElementsMatch(t, []killswitches.TransportAdapterKey{TransportAdapterHostedJSONRPC, TransportAdapterPrivateProxyJSONRPC, TransportAdapterHookNative}, aiDefinition.TransportAdapters)
	require.Equal(t, DefaultAIAccessExternalNote, aiDefinition.DefaultExternalNote)
	require.NotEqual(t, definition.DefaultExternalNote, aiDefinition.DefaultExternalNote)
	require.Equal(t, EnforcementOwner, aiDefinition.EnforcementOwner)

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
	require.Len(t, inventory, 8)
	coverageByDefinition := map[killswitches.DefinitionKey][]killswitches.Surface{}
	for _, contract := range inventory {
		coverageByDefinition[contract.Definition] = append(coverageByDefinition[contract.Definition], contract.Surface)
		require.Equal(t, killswitches.FailurePolicyFailClosed, contract.FailurePolicy)
		if contract.Definition == DefinitionKeyAIAccess {
			require.Equal(t, IdentityContractKeyAuthenticatedUserAIResource, contract.IdentityContract)
		} else {
			require.Equal(t, IdentityContractKeyAuthenticatedUserMCPServer, contract.IdentityContract)
		}
		require.NotEmpty(t, contract.PrincipalSource)
		require.NotEmpty(t, contract.ResourceSource)
		require.NotEmpty(t, contract.Checkpoint)
		require.NotEmpty(t, contract.ProtectedWork)
	}
	require.Equal(t, []killswitches.Surface{SurfaceHostedToolsCall, SurfacePrivateProxyToolsCall}, coverageByDefinition[DefinitionKeyMCPToolExecution])
	require.ElementsMatch(t, []killswitches.Surface{SurfaceHostedToolsCall, SurfacePrivateProxyToolsCall, SurfaceClaudeUserPromptSubmit, SurfaceClaudePreToolUse, SurfaceCodexUserPromptSubmit, SurfaceCodexPreToolUse}, coverageByDefinition[DefinitionKeyAIAccess])

	hosted, ok := registry.Coverage(DefinitionKeyMCPToolExecution, SurfaceHostedToolsCall)
	require.True(t, ok)
	require.Equal(t, TransportAdapterHostedJSONRPC, hosted.TransportAdapter)
	require.Contains(t, hosted.ProtectedWork, "resolving protected credentials")

	proxy, ok := registry.Coverage(DefinitionKeyMCPToolExecution, SurfacePrivateProxyToolsCall)
	require.True(t, ok)
	require.Equal(t, TransportAdapterPrivateProxyJSONRPC, proxy.TransportAdapter)

	aiHosted, ok := registry.Coverage(DefinitionKeyAIAccess, SurfaceHostedToolsCall)
	require.True(t, ok)
	require.Equal(t, TransportAdapterHostedJSONRPC, aiHosted.TransportAdapter)
	aiProxy, ok := registry.Coverage(DefinitionKeyAIAccess, SurfacePrivateProxyToolsCall)
	require.True(t, ok)
	require.Equal(t, TransportAdapterPrivateProxyJSONRPC, aiProxy.TransportAdapter)
	for _, excludedSurface := range []killswitches.Surface{"killswitch_management", "audit_read", "platform_break_glass", "hooks", "litellm", "hosted_inference", "assistant_runtime", "hooks_permission_request", "hooks_backfill"} {
		_, covered := registry.Coverage(DefinitionKeyAIAccess, excludedSurface)
		require.False(t, covered, string(excludedSurface))
	}

	require.Equal(t, "hooks-acting-user.v1", HookCoverageVersionContract.ContractVersion)
	require.Empty(t, HookCoverageVersionContract.ReleasedRelayVersions, "no already-released relay implements the proof contract")
	require.Equal(t, []string{"Claude Code 2.1.250", "Codex CLI 0.150.1"}, HookCoverageVersionContract.TestedNativeVersions)
	require.Equal(t, []string{"macOS 26.5.2 arm64"}, HookCoverageVersionContract.TestedPlatforms)

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

	for _, key := range []killswitches.TransportAdapterKey{TransportAdapterHostedJSONRPC, TransportAdapterPrivateProxyJSONRPC, TransportAdapterHookNative} {
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
