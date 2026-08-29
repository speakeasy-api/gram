// Package mcptoolexecution registers the MCP kill-switch contracts: the
// fail-closed `mcp_tool_execution` and internal `ai_access` definitions, the
// authoritative concrete-user principal adapter, the canonical
// organization-owned resource adapters, and the coverage inventory for hosted
// and private-proxy MCP tools/call plus approved live Claude/Codex hook activity.
//
// Registration declares the contracts consumed by the MCP and hook checkpoints.
// The internal ai_access definition is not exposed through customer management.
package mcptoolexecution

import (
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/hooks/delegation"
	"github.com/speakeasy-api/gram/server/internal/killswitches"
)

const (
	// DefinitionKeyMCPToolExecution is the M2 capability governing MCP
	// tools/call execution.
	DefinitionKeyMCPToolExecution killswitches.DefinitionKey = "mcp_tool_execution"

	// DefinitionKeyAIAccess is the internal broad AI-access capability. Its
	// verified coverage is limited to authenticated MCP tools/call and the
	// explicitly registered live Claude/Codex hook surfaces below.
	DefinitionKeyAIAccess killswitches.DefinitionKey = "ai_access"

	// PrincipalKindUser is the concrete Gram user principal namespace; keys
	// are user IDs of authoritative active organization members.
	PrincipalKindUser killswitches.PrincipalKind = "user"

	// ResourceKindMCPServer is the canonical MCP server resource namespace;
	// keys are fronting mcp_servers row IDs.
	ResourceKindMCPServer killswitches.ResourceKind = "mcp_server"

	// IdentityContractKeyAuthenticatedUserMCPServer is the unchanged DNO-988
	// contract pairing an authoritative user with an MCP server.
	IdentityContractKeyAuthenticatedUserMCPServer killswitches.IdentityContractKey = "authenticated_user_mcp_server"

	// IdentityContractKeyAuthenticatedUserAIResource is the additive ai_access
	// contract spanning existing MCP servers and governed native hook activity.
	IdentityContractKeyAuthenticatedUserAIResource killswitches.IdentityContractKey = "authenticated_user_ai_resource"

	ResourceKindHookActivity killswitches.ResourceKind = "hook_activity"

	// SurfaceHostedToolsCall is hosted MCP tools/call dispatch.
	SurfaceHostedToolsCall killswitches.Surface = "mcp_hosted_tools_call"

	// SurfacePrivateProxyToolsCall is private proxied or remote MCP
	// tools/call forwarding.
	SurfacePrivateProxyToolsCall killswitches.Surface = "mcp_private_proxy_tools_call"

	// TransportAdapterHostedJSONRPC keys the hosted JSON-RPC transport
	// mapping owned by the hosted dispatch checkpoint.
	TransportAdapterHostedJSONRPC killswitches.TransportAdapterKey = "mcp_hosted_jsonrpc"

	// TransportAdapterPrivateProxyJSONRPC keys the private-proxy JSON-RPC
	// transport mapping owned by the forwarding checkpoint.
	TransportAdapterPrivateProxyJSONRPC killswitches.TransportAdapterKey = "mcp_private_proxy_jsonrpc"
	TransportAdapterHookNative          killswitches.TransportAdapterKey = "hooks_native_deny"

	SurfaceClaudeUserPromptSubmit killswitches.Surface = "hooks_claude_live_user_prompt_submit"
	SurfaceClaudePreToolUse       killswitches.Surface = "hooks_claude_live_pre_tool_use"
	SurfaceCodexUserPromptSubmit  killswitches.Surface = "hooks_codex_live_user_prompt_submit"
	SurfaceCodexPreToolUse        killswitches.Surface = "hooks_codex_live_pre_tool_use"

	// DefaultExternalNote is the editable customer-safe starting value for
	// new MCP tool-execution prescriptions. It never leaks framework vocabulary.
	DefaultExternalNote = "MCP tool calls are currently paused by your organization's administrator."

	// DefaultAIAccessExternalNote is the separate customer-safe starting value
	// for internal AI-access prescriptions.
	DefaultAIAccessExternalNote = "AI access is currently paused by your organization's administrator."

	// EnforcementOwner is the team accountable for the enforcement
	// checkpoints of this definition.
	EnforcementOwner = "devices-observability"
)

// NewRegistration assembles the complete code-owned MCP registration. The database is used by the adapters at evaluation and
// validation time only; fixture validation during registry construction is
// pure.
func NewRegistration(db *pgxpool.Pool) killswitches.Registration {
	return killswitches.Registration{
		Definitions: []killswitches.Definition{
			{
				Key:                 DefinitionKeyMCPToolExecution,
				PrincipalKinds:      []killswitches.PrincipalKind{PrincipalKindUser},
				ResourceKinds:       []killswitches.ResourceKind{ResourceKindMCPServer},
				FailurePolicy:       killswitches.FailurePolicyFailClosed,
				DefaultExternalNote: DefaultExternalNote,
				EnforcementOwner:    EnforcementOwner,
				IdentityContract:    IdentityContractKeyAuthenticatedUserMCPServer,
				Surfaces:            []killswitches.Surface{SurfaceHostedToolsCall, SurfacePrivateProxyToolsCall},
				TransportAdapters:   []killswitches.TransportAdapterKey{TransportAdapterHostedJSONRPC, TransportAdapterPrivateProxyJSONRPC},
			},
			{
				Key:                 DefinitionKeyAIAccess,
				PrincipalKinds:      []killswitches.PrincipalKind{PrincipalKindUser},
				ResourceKinds:       []killswitches.ResourceKind{ResourceKindMCPServer, ResourceKindHookActivity},
				FailurePolicy:       killswitches.FailurePolicyFailClosed,
				DefaultExternalNote: DefaultAIAccessExternalNote,
				EnforcementOwner:    EnforcementOwner,
				IdentityContract:    IdentityContractKeyAuthenticatedUserAIResource,
				Surfaces:            []killswitches.Surface{SurfaceHostedToolsCall, SurfacePrivateProxyToolsCall, SurfaceClaudeUserPromptSubmit, SurfaceClaudePreToolUse, SurfaceCodexUserPromptSubmit, SurfaceCodexPreToolUse},
				TransportAdapters:   []killswitches.TransportAdapterKey{TransportAdapterHostedJSONRPC, TransportAdapterPrivateProxyJSONRPC, TransportAdapterHookNative},
			},
		},
		IdentityContracts: []killswitches.IdentityContract{
			{
				Key:            IdentityContractKeyAuthenticatedUserMCPServer,
				PrincipalKinds: []killswitches.PrincipalKind{PrincipalKindUser},
				ResourceKinds:  []killswitches.ResourceKind{ResourceKindMCPServer},
			},
			{
				Key:            IdentityContractKeyAuthenticatedUserAIResource,
				PrincipalKinds: []killswitches.PrincipalKind{PrincipalKindUser},
				ResourceKinds:  []killswitches.ResourceKind{ResourceKindMCPServer, ResourceKindHookActivity},
			},
		},
		PrincipalAdapters: []killswitches.PrincipalAdapterRegistration{{
			Adapter:  NewAuthenticatedUserPrincipalAdapter(db),
			Fixtures: principalFixtures(),
		}},
		ResourceAdapters: []killswitches.ResourceAdapterRegistration{
			{Adapter: NewMCPServerResourceAdapter(db), Fixtures: resourceFixtures()},
			{Adapter: HookActivityResourceAdapter{}, Fixtures: hookResourceFixtures()},
		},
		Surfaces: []killswitches.Surface{SurfaceHostedToolsCall, SurfacePrivateProxyToolsCall, SurfaceClaudeUserPromptSubmit, SurfaceClaudePreToolUse, SurfaceCodexUserPromptSubmit, SurfaceCodexPreToolUse},
		TransportAdapters: []killswitches.TransportAdapterRegistration{
			{Key: TransportAdapterHostedJSONRPC, Adapter: killswitches.ResolveTransportDisposition},
			{Key: TransportAdapterPrivateProxyJSONRPC, Adapter: killswitches.ResolveTransportDisposition},
			{Key: TransportAdapterHookNative, Adapter: killswitches.ResolveTransportDisposition},
		},
		Coverage: append([]killswitches.CoverageContract{
			{
				Definition:       DefinitionKeyMCPToolExecution,
				Surface:          SurfaceHostedToolsCall,
				PrincipalSource:  "Validated user-session provenance (mcpidentity), revalidated as an active organization member on every covered call.",
				ResourceSource:   "Fronting mcp_servers.id resolved from the mcp_endpoint route, validated as a live server in a live project of the organization. Never a slug, URL, toolset, backend ID, or caller-provided value.",
				Checkpoint:       "After trusted authentication, tenant resolution, and acting-user validation on hosted MCP tools/call dispatch.",
				ProtectedWork:    "Loading or applying tool configuration, resolving protected credentials, and dispatching local, dynamic, function, platform, or external-MCP proxy execution.",
				FailurePolicy:    killswitches.FailurePolicyFailClosed,
				TransportAdapter: TransportAdapterHostedJSONRPC,
				EnforcementOwner: EnforcementOwner,
				IdentityContract: IdentityContractKeyAuthenticatedUserMCPServer,
			},
			{
				Definition:       DefinitionKeyMCPToolExecution,
				Surface:          SurfacePrivateProxyToolsCall,
				PrincipalSource:  "Validated user-session provenance (mcpidentity), revalidated as an active organization member on every covered call.",
				ResourceSource:   "Fronting mcp_servers.id resolved from the mcp_endpoint route, validated as a live server in a live project of the organization. The remote or tunneled backend ID is never the key, so hosted and private routes to one server share one canonical resource.",
				Checkpoint:       "After trusted authentication, tenant resolution, and acting-user validation on private proxied or remote MCP tools/call forwarding.",
				ProtectedWork:    "Tool-level authorization-dependent forwarding work and any upstream request; per-server connection configuration may already be loaded.",
				FailurePolicy:    killswitches.FailurePolicyFailClosed,
				TransportAdapter: TransportAdapterPrivateProxyJSONRPC,
				EnforcementOwner: EnforcementOwner,
				IdentityContract: IdentityContractKeyAuthenticatedUserMCPServer,
			},
			{
				Definition:       DefinitionKeyAIAccess,
				Surface:          SurfaceHostedToolsCall,
				PrincipalSource:  "Validated user-session provenance (mcpidentity), revalidated as an active organization member on every covered call.",
				ResourceSource:   "Fronting mcp_servers.id resolved from the mcp_endpoint route and validated as a live server in a live project of the organization.",
				Checkpoint:       "The shared MCP checkpoint after trusted authentication, tenant resolution, and acting-user validation on hosted MCP tools/call dispatch.",
				ProtectedWork:    "The same hosted MCP tools/call work protected by mcp_tool_execution; no non-MCP AI surface is claimed.",
				FailurePolicy:    killswitches.FailurePolicyFailClosed,
				TransportAdapter: TransportAdapterHostedJSONRPC,
				EnforcementOwner: EnforcementOwner,
				IdentityContract: IdentityContractKeyAuthenticatedUserAIResource,
			},
			{
				Definition:       DefinitionKeyAIAccess,
				Surface:          SurfacePrivateProxyToolsCall,
				PrincipalSource:  "Validated user-session provenance (mcpidentity), revalidated as an active organization member on every covered call.",
				ResourceSource:   "Fronting mcp_servers.id resolved from the mcp_endpoint route and validated as a live server in a live project of the organization.",
				Checkpoint:       "The shared MCP checkpoint after trusted authentication, tenant resolution, and acting-user validation on private proxied or remote MCP tools/call forwarding.",
				ProtectedWork:    "The same private MCP tools/call work protected by mcp_tool_execution; no non-MCP AI surface is claimed.",
				FailurePolicy:    killswitches.FailurePolicyFailClosed,
				TransportAdapter: TransportAdapterPrivateProxyJSONRPC,
				EnforcementOwner: EnforcementOwner,
				IdentityContract: IdentityContractKeyAuthenticatedUserAIResource,
			},
		}, hookCoverageContracts()...),
	}
}

// HookCoverageVersionContract is the checked-in version boundary for DNO-989.
// No already-released relay is claimed: v1 first exists in the next release.
// Native versions list only clients substantiated by the real-client E2E suite.
var HookCoverageVersionContract = struct {
	ContractVersion       string
	ReleasedRelayVersions []string
	TestedNativeVersions  []string
	TestedPlatforms       []string
}{
	ContractVersion:       delegation.ContractVersion,
	ReleasedRelayVersions: []string{},
	TestedNativeVersions:  []string{"Claude Code 2.1.250", "Codex CLI 0.150.1"},
	TestedPlatforms:       []string{"macOS 26.5.2 arm64"},
}

func hookCoverageContracts() []killswitches.CoverageContract {
	bindings := delegation.ApprovedBindings()
	result := make([]killswitches.CoverageContract, 0, len(bindings))
	for _, binding := range bindings {
		result = append(result, killswitches.CoverageContract{
			Definition: DefinitionKeyAIAccess, Surface: hookSurface(binding),
			PrincipalSource: "Gram-signed hooks-acting-user.v1 assertion minted only after session-authenticated PKCE enrollment and per-invocation Ed25519 proof; active organization membership is revalidated at mint and ingest.",
			ResourceSource:  "Code-registered hook_activity resource " + binding.ResourceKey + " derived from the assertion-bound provider and exact native event.",
			Checkpoint:      "Unified /rpc/hooks.ingest before quarantine, spend, risk, persistence, or the native client resumes the prompt/tool action.",
			ProtectedWork:   "Only live " + binding.Event + " from " + binding.Provider + "; PermissionRequest, backfill, replay, legacy endpoints, and every other provider/event are excluded.",
			FailurePolicy:   killswitches.FailurePolicyFailClosed, TransportAdapter: TransportAdapterHookNative,
			EnforcementOwner: EnforcementOwner, IdentityContract: IdentityContractKeyAuthenticatedUserAIResource,
		})
	}
	return result
}

func hookSurface(binding delegation.Binding) killswitches.Surface {
	switch {
	case binding.Provider == delegation.ProviderClaude && binding.Event == delegation.EventUserPromptSubmit:
		return SurfaceClaudeUserPromptSubmit
	case binding.Provider == delegation.ProviderClaude && binding.Event == delegation.EventPreToolUse:
		return SurfaceClaudePreToolUse
	case binding.Provider == delegation.ProviderCodex && binding.Event == delegation.EventUserPromptSubmit:
		return SurfaceCodexUserPromptSubmit
	case binding.Provider == delegation.ProviderCodex && binding.Event == delegation.EventPreToolUse:
		return SurfaceCodexPreToolUse
	default:
		panic("approved hook binding has no registered surface")
	}
}

// NewRegistry builds and validates the finalized MCP registry.
func NewRegistry(db *pgxpool.Pool) (*killswitches.Registry, error) {
	registry, err := killswitches.BuildRegistry(NewRegistration(db))
	if err != nil {
		return nil, fmt.Errorf("build MCP kill-switch registry: %w", err)
	}
	return registry, nil
}

// ExcludedMCPSurface documents an MCP serving mode that deliberately produces
// no supported principal or resource for either registered MCP definition.
type ExcludedMCPSurface struct {
	// Name identifies the serving mode.
	Name string

	// Reason states why the mode is outside M2 coverage and how covered code
	// must classify traffic from it.
	Reason string
}

// excludedMCPSurfaces is the checked-in exclusion inventory. Every entry is a
// product limitation to disclose: none of these modes may be reported as
// M2-covered, and none may have identity inferred from API-key creators,
// credential owners, emails, external IDs, or organization-only context.
var excludedMCPSurfaces = []ExcludedMCPSurface{
	{
		Name:   "legacy toolset-only /mcp/{slug} fallback",
		Reason: "Has no fronting mcp_servers row. The resource adapter classifies it as an unsupported resource; a server key must never be synthesized from a toolset.",
	},
	{
		Name:   "meta MCP endpoints",
		Reason: "Resolve a meta_mcp_servers identity, not an mcp_servers identity, and are excluded until they get their own resource design.",
	},
	{
		Name:   "public anonymous MCP",
		Reason: "Anonymous subjects have no acting user; the principal adapter classifies them as unsupported identity.",
	},
	{
		Name:   "platform MCP (/platform/mcp)",
		Reason: "Accepts only assistant-runtime tokens, which are not authoritative acting users.",
	},
	{
		Name:   "internal direct tool callers (HandleToolsCall / HandleToolsList)",
		Reason: "Agent-workflow callers bypass MCP authentication and have no fronting server; they carry neither supported principal nor resource.",
	},
	{
		Name:   "API-key and chat-session authenticated calls",
		Reason: "These credentials prove an organization or a machine, never an acting user. Creator or owner attribution must not be promoted to a principal.",
	},
}

// ExcludedMCPSurfaces returns the checked-in exclusion inventory.
func ExcludedMCPSurfaces() []ExcludedMCPSurface {
	return slices.Clone(excludedMCPSurfaces)
}

// supportedKey builds a supported fixture expectation from a static key. An
// invalid static key yields the zero result, which registry construction
// rejects at startup.
func supportedKey[T ~string](key T) killswitches.CanonicalizationResult[T] {
	result, _ := killswitches.NewCanonicalizationResult(key)
	return result
}

func principalFixtures() []killswitches.PrincipalCanonicalizationFixture {
	return []killswitches.PrincipalCanonicalizationFixture{
		{OrganizationID: "org_fixture", Input: "user_01J8FIXTURE", Expected: supportedKey(killswitches.PrincipalKey("user_01J8FIXTURE"))},
		{OrganizationID: "org_fixture", Input: "  user_01J8FIXTURE  ", Expected: supportedKey(killswitches.PrincipalKey("user_01J8FIXTURE"))},
		{OrganizationID: "org_fixture", Input: "   ", Expected: killswitches.UnsupportedCanonicalizationResult[killswitches.PrincipalKey]()},
		{OrganizationID: "org_fixture", Input: "user\x00id", Expected: killswitches.UnsupportedCanonicalizationResult[killswitches.PrincipalKey]()},
	}
}

func resourceFixtures() []killswitches.ResourceCanonicalizationFixture {
	return []killswitches.ResourceCanonicalizationFixture{
		{OrganizationID: "org_fixture", Input: "0198A1B2-C3D4-7000-8000-0123456789AB", Expected: supportedKey(killswitches.ResourceKey("0198a1b2-c3d4-7000-8000-0123456789ab"))},
		{OrganizationID: "org_fixture", Input: " 0198a1b2-c3d4-7000-8000-0123456789ab ", Expected: supportedKey(killswitches.ResourceKey("0198a1b2-c3d4-7000-8000-0123456789ab"))},
		{OrganizationID: "org_fixture", Input: "not-a-server-id", Expected: killswitches.UnsupportedCanonicalizationResult[killswitches.ResourceKey]()},
		{OrganizationID: "org_fixture", Input: "", Expected: killswitches.UnsupportedCanonicalizationResult[killswitches.ResourceKey]()},
	}
}
