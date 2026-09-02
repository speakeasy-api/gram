// Package mcptoolexecution registers the fail-closed mcp_tool_execution and
// ai_access contracts. It preserves the MCP identities and coverage while
// extending ai_access with the explicit Gram-hosted-inference identity and
// coverage contract consumed by the shared ChatClient checkpoint.
//
// The internal ai_access definition is not exposed through customer management.
package mcptoolexecution

import (
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/killswitches/hostedinference"
)

const (
	// DefinitionKeyMCPToolExecution is the M2 capability governing MCP
	// tools/call execution.
	DefinitionKeyMCPToolExecution killswitches.DefinitionKey = "mcp_tool_execution"

	// DefinitionKeyAIAccess is the internal broad AI-access capability. Its
	// currently verified coverage is limited to authenticated MCP tools/call.
	DefinitionKeyAIAccess killswitches.DefinitionKey = "ai_access"

	// PrincipalKindUser is the concrete Gram user principal namespace; keys
	// are user IDs of authoritative active organization members.
	PrincipalKindUser killswitches.PrincipalKind = "user"

	// ResourceKindMCPServer is the canonical MCP server resource namespace;
	// keys are fronting mcp_servers row IDs.
	ResourceKindMCPServer killswitches.ResourceKind = "mcp_server"

	// IdentityContractKeyAuthenticatedUserMCPServer pairs the authoritative
	// user principal with the canonical mcp_server resource.
	IdentityContractKeyAuthenticatedUserMCPServer killswitches.IdentityContractKey = "authenticated_user_mcp_server"

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

// NewRegistration assembles the complete code-owned MCP and hosted-inference
// registration. The database is used by adapters at evaluation and validation
// time only; fixture validation during registry construction is pure.
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
				ResourceKinds:       []killswitches.ResourceKind{ResourceKindMCPServer, hostedinference.ResourceKindGramHostedInference},
				FailurePolicy:       killswitches.FailurePolicyFailClosed,
				DefaultExternalNote: DefaultAIAccessExternalNote,
				EnforcementOwner:    EnforcementOwner,
				IdentityContract:    hostedinference.IdentityContractKeyAuthenticatedUserAIAccess,
				Surfaces:            []killswitches.Surface{SurfaceHostedToolsCall, SurfacePrivateProxyToolsCall, hostedinference.SurfaceGramHostedInference},
				TransportAdapters:   []killswitches.TransportAdapterKey{TransportAdapterHostedJSONRPC, TransportAdapterPrivateProxyJSONRPC, hostedinference.TransportAdapterGramHostedInference},
			},
		},
		IdentityContracts: []killswitches.IdentityContract{
			{
				Key:            IdentityContractKeyAuthenticatedUserMCPServer,
				PrincipalKinds: []killswitches.PrincipalKind{PrincipalKindUser},
				ResourceKinds:  []killswitches.ResourceKind{ResourceKindMCPServer},
			},
			{
				Key:            hostedinference.IdentityContractKeyAuthenticatedUserAIAccess,
				PrincipalKinds: []killswitches.PrincipalKind{PrincipalKindUser},
				ResourceKinds:  []killswitches.ResourceKind{ResourceKindMCPServer, hostedinference.ResourceKindGramHostedInference},
			},
		},
		PrincipalAdapters: []killswitches.PrincipalAdapterRegistration{{
			Adapter:  NewAuthenticatedUserPrincipalAdapter(db),
			Fixtures: principalFixtures(),
		}},
		ResourceAdapters: []killswitches.ResourceAdapterRegistration{
			{Adapter: NewMCPServerResourceAdapter(db), Fixtures: resourceFixtures()},
			{Adapter: hostedinference.ResourceAdapter{}, Fixtures: hostedinference.ResourceFixtures()},
		},
		Surfaces: []killswitches.Surface{SurfaceHostedToolsCall, SurfacePrivateProxyToolsCall, hostedinference.SurfaceGramHostedInference},
		TransportAdapters: []killswitches.TransportAdapterRegistration{
			{Key: TransportAdapterHostedJSONRPC, Adapter: killswitches.ResolveTransportDisposition},
			{Key: TransportAdapterPrivateProxyJSONRPC, Adapter: killswitches.ResolveTransportDisposition},
			{Key: hostedinference.TransportAdapterGramHostedInference, Adapter: killswitches.ResolveTransportDisposition},
		},
		Coverage: []killswitches.CoverageContract{
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
				IdentityContract: hostedinference.IdentityContractKeyAuthenticatedUserAIAccess,
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
				IdentityContract: hostedinference.IdentityContractKeyAuthenticatedUserAIAccess,
			},
			{
				Definition:       DefinitionKeyAIAccess,
				Surface:          hostedinference.SurfaceGramHostedInference,
				PrincipalSource:  "Opaque tenant-bound acting-user provenance derived from a validated ordinary Gram session or a qualifying signed chat JWT carrying matching ordinary-session provenance, then revalidated as an active organization member for every provider attempt.",
				ResourceSource:   "Static canonical Gram-hosted-inference identity for an enumerated current governed user call category.",
				Checkpoint:       "ChatClient before capture and key resolution, then immediately before every completion, stream, object-completion, or embedding provider attempt.",
				ProtectedWork:    "Governed user chat completion, chat summaries, tool-call summaries, risk authoring, and organization-admin business-memory search embeddings; internal, background, and assistant-owned classes are explicit bypasses.",
				FailurePolicy:    killswitches.FailurePolicyFailClosed,
				TransportAdapter: hostedinference.TransportAdapterGramHostedInference,
				EnforcementOwner: EnforcementOwner,
				IdentityContract: hostedinference.IdentityContractKeyAuthenticatedUserAIAccess,
			},
		},
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
