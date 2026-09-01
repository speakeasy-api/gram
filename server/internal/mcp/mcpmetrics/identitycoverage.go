package mcpmetrics

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// InstrumentMCPToolCallKillswitchIdentity is the OTel instrument name for the
// kill-switch identity-coverage census: every real tools/call reaching a
// covered kill-switch checkpoint, dimensioned by bounded server-controlled
// classes only. It quantifies what share of traffic carries supported
// authoritative user identity and a canonical server resource, so low
// coverage is observable instead of being silently folded into "unmatched".
const InstrumentMCPToolCallKillswitchIdentity = "mcp.tool.call.killswitch_identity"

// KillswitchCoverageSurface is the enforcement checkpoint family that
// observed a covered tools/call.
type KillswitchCoverageSurface string

const (
	// KillswitchSurfaceHosted covers hosted MCP tools/call dispatch.
	KillswitchSurfaceHosted KillswitchCoverageSurface = "hosted"

	// KillswitchSurfacePrivateProxy covers private proxied or remote MCP
	// tools/call forwarding.
	KillswitchSurfacePrivateProxy KillswitchCoverageSurface = "private_proxy"
)

// KillswitchIdentityClass is the bounded identity-coverage outcome of
// principal-candidate derivation for one covered tools/call.
type KillswitchIdentityClass string

const (
	// KillswitchIdentityActiveUser means an authoritative concrete user was
	// revalidated as an active organization member and became a candidate.
	KillswitchIdentityActiveUser KillswitchIdentityClass = "active_user"

	// KillswitchIdentityInactiveUser means an authoritative concrete user was
	// presented but is no longer an active member of the organization.
	KillswitchIdentityInactiveUser KillswitchIdentityClass = "inactive_user"

	// KillswitchIdentityAnonymous means the validated session is anonymous.
	KillswitchIdentityAnonymous KillswitchIdentityClass = "anonymous"

	// KillswitchIdentityAPIKey means the validated credential is an API key,
	// which has no acting user.
	KillswitchIdentityAPIKey KillswitchIdentityClass = "api_key"

	// KillswitchIdentityAssistant means the validated credential is an
	// assistant-runtime token, which is not an authoritative acting user.
	KillswitchIdentityAssistant KillswitchIdentityClass = "assistant"

	// KillswitchIdentityChatSession means the validated credential is a
	// chat-session token, which attributes an embedded chat session or
	// external end-user and is not an authoritative acting user.
	KillswitchIdentityChatSession KillswitchIdentityClass = "chat_session"

	// KillswitchIdentityUnattributed means the surface established no
	// authentication provenance at all.
	KillswitchIdentityUnattributed KillswitchIdentityClass = "unattributed"

	// KillswitchIdentityUnavailable means candidate derivation failed as an
	// infrastructure error; the call's identity coverage is unknown.
	KillswitchIdentityUnavailable KillswitchIdentityClass = "unavailable"
)

// KillswitchResourceClass is the bounded resource-coverage outcome of
// canonical mcp_server derivation for one covered tools/call.
type KillswitchResourceClass string

const (
	// KillswitchResourceCanonicalServer means the call resolved one canonical
	// organization-owned fronting mcp_servers identity.
	KillswitchResourceCanonicalServer KillswitchResourceClass = "canonical_server"

	// KillswitchResourceLegacyNoServer means the call arrived on the legacy
	// toolset-only route, which has no fronting mcp_servers row.
	KillswitchResourceLegacyNoServer KillswitchResourceClass = "legacy_no_server"

	// KillswitchResourceUnsupportedSurface means the serving mode never
	// carries a supported mcp_server resource (meta, platform, internal).
	KillswitchResourceUnsupportedSurface KillswitchResourceClass = "unsupported_surface"

	// KillswitchResourceInvalidOwner means a fronting server was resolved but
	// is no longer a live server in a live project of the organization.
	KillswitchResourceInvalidOwner KillswitchResourceClass = "invalid_owner"

	// KillswitchResourceUnavailable means resource derivation failed as an
	// infrastructure error; the call's resource coverage is unknown.
	KillswitchResourceUnavailable KillswitchResourceClass = "unavailable"
)

func validKillswitchCoverageSurface(surface KillswitchCoverageSurface) bool {
	switch surface {
	case KillswitchSurfaceHosted, KillswitchSurfacePrivateProxy:
		return true
	default:
		return false
	}
}

func validKillswitchIdentityClass(class KillswitchIdentityClass) bool {
	switch class {
	case KillswitchIdentityActiveUser, KillswitchIdentityInactiveUser, KillswitchIdentityAnonymous,
		KillswitchIdentityAPIKey, KillswitchIdentityAssistant, KillswitchIdentityChatSession,
		KillswitchIdentityUnattributed, KillswitchIdentityUnavailable:
		return true
	default:
		return false
	}
}

func validKillswitchResourceClass(class KillswitchResourceClass) bool {
	switch class {
	case KillswitchResourceCanonicalServer, KillswitchResourceLegacyNoServer,
		KillswitchResourceUnsupportedSurface, KillswitchResourceInvalidOwner,
		KillswitchResourceUnavailable:
		return true
	default:
		return false
	}
}

// IdentityCoverageCounter owns the kill-switch identity-coverage census. A
// nil *IdentityCoverageCounter is valid — Record becomes a no-op — so callers
// that do not care about metrics can pass nil.
type IdentityCoverageCounter struct {
	calls metric.Int64Counter
}

// NewIdentityCoverageCounter constructs the coverage census counter. An
// instrument creation failure is logged and leaves the instrument nil; Record
// handles nil instruments so partial construction still produces a usable
// value.
func NewIdentityCoverageCounter(meter metric.Meter, logger *slog.Logger) *IdentityCoverageCounter {
	calls, err := meter.Int64Counter(
		InstrumentMCPToolCallKillswitchIdentity,
		metric.WithDescription("MCP tools/call requests observed at kill-switch checkpoints, by surface and bounded identity and resource coverage class"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(InstrumentMCPToolCallKillswitchIdentity), attr.SlogError(err))
	}

	return &IdentityCoverageCounter{calls: calls}
}

// Record counts one covered tools/call. Every dimension is clamped to its
// bounded set — an unknown value records as the corresponding "unavailable"
// class (surface falls back to hosted) — so no identifier, URL, note, or
// free-form error text can ever become a metric dimension.
func (c *IdentityCoverageCounter) Record(ctx context.Context, surface KillswitchCoverageSurface, identity KillswitchIdentityClass, resource KillswitchResourceClass) {
	if c == nil || c.calls == nil {
		return
	}

	if !validKillswitchCoverageSurface(surface) {
		surface = KillswitchSurfaceHosted
	}
	if !validKillswitchIdentityClass(identity) {
		identity = KillswitchIdentityUnavailable
	}
	if !validKillswitchResourceClass(resource) {
		resource = KillswitchResourceUnavailable
	}

	c.calls.Add(ctx, 1, metric.WithAttributes(
		attr.McpKillswitchSurface(surface),
		attr.McpKillswitchIdentityClass(identity),
		attr.McpKillswitchResourceClass(resource),
	))
}
