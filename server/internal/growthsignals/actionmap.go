package growthsignals

import (
	"strings"

	"github.com/speakeasy-api/gram/server/internal/audit"
)

// McpKind names the flavour of MCP server an ActivityMcpServerCreated event
// describes. Five audit actions create MCP servers and Growth reads them as one
// moment, so the flavour survives as a property instead of as five activities.
type McpKind string

const (
	// McpKindHosted is a server Gram builds and hosts from a deployment.
	McpKindHosted McpKind = "hosted"

	// McpKindRemote is a third-party server Gram proxies.
	McpKindRemote McpKind = "remote"

	// McpKindTunneled is a server reachable through a customer-run tunnel.
	McpKindTunneled McpKind = "tunneled"

	// McpKindUnproxied is a server registered for visibility but not proxied.
	McpKindUnproxied McpKind = "unproxied"

	// McpKindMeta is a server that aggregates other servers.
	McpKindMeta McpKind = "meta"
)

// ActionMapping is everything one audit action contributes to an event.
type ActionMapping struct {
	// Activity is the taxonomy name the action is reported as. ActivitySkip
	// means the action is excluded and nothing should be emitted for it.
	Activity Activity

	// Extra holds the static properties the mapping itself contributes, such
	// as the mcp_kind of an MCP server creation, and is nil when it
	// contributes none. Each call returns a freshly allocated map, so callers
	// may add their own per-event extras to it.
	Extra map[string]string
}

// curatedActivities gives the moments Growth named a friendly activity, so the
// significant-events channel can filter on a stable name rather than on the
// shape of whichever audit action happened to produce it.
//
// It is deliberately short. Every other audited action still reaches PostHog
// through the pass-through name, so this map is a renaming layer rather than an
// allowlist that has to be extended for new coverage.
//
//nolint:exhaustive // a renaming layer over a subset of actions, not a case analysis
var curatedActivities = map[audit.Action]Activity{
	audit.ActionProjectCreate: ActivityProjectCreated,

	audit.ActionMcpServerCreate:          ActivityMcpServerCreated,
	audit.ActionRemoteMcpServerCreate:    ActivityMcpServerCreated,
	audit.ActionTunneledMcpServerCreate:  ActivityMcpServerCreated,
	audit.ActionUnproxiedMcpServerCreate: ActivityMcpServerCreated,
	audit.ActionMetaMcpServerCreate:      ActivityMcpServerCreated,

	audit.ActionRiskPolicyCreate: ActivitySecurityPolicyCreated,
	audit.ActionRiskPolicyUpdate: ActivitySecurityPolicyUpdated,

	audit.ActionMcpServerUpdate:             ActivityMcpServerUpdated,
	audit.ActionMCPMetadataUpdate:           ActivityMcpServerUpdated,
	audit.ActionMcpServerToolMetadataUpdate: ActivityMcpServerUpdated,

	audit.ActionOrganizationInviteCreate: ActivityMemberInvited,
}

// mcpCreateKinds records which MCP server flavour each creation action makes.
//
//nolint:exhaustive // only MCP creation actions have a server flavour
var mcpCreateKinds = map[audit.Action]McpKind{
	audit.ActionMcpServerCreate:          McpKindHosted,
	audit.ActionRemoteMcpServerCreate:    McpKindRemote,
	audit.ActionTunneledMcpServerCreate:  McpKindTunneled,
	audit.ActionUnproxiedMcpServerCreate: McpKindUnproxied,
	audit.ActionMetaMcpServerCreate:      McpKindMeta,
}

// excludedActions are audited actions with no ops value that would otherwise
// dominate the firehose: an assistant's tool calls and wake timers, chat
// session reads, and the diagnostics call Platform MCP clients poll.
//
// They are dropped here rather than at the PostHog destination because the
// volume is the problem, and a destination filter still pays for every event
// that reaches it.
//
//nolint:exhaustive // an exclusion list is partial by definition
var excludedActions = map[audit.Action]struct{}{
	audit.ActionAssistantToolCall:                    {},
	audit.ActionWakeScheduled:                        {},
	audit.ActionWakeFired:                            {},
	audit.ActionWakeCancelled:                        {},
	audit.ActionChatSessionAccess:                    {},
	audit.ActionPlatformMcpDiagnosticsUserStatusRead: {},
}

// ActivityForAction resolves an audit action to what it contributes to an
// event.
//
// An action nobody curated still maps to an activity, derived from the action
// itself, so the firehose covers every audited mutation without an allowlist to
// maintain as services add audit coverage.
func ActivityForAction(action audit.Action) ActionMapping {
	if _, excluded := excludedActions[action]; excluded {
		return ActionMapping{Activity: ActivitySkip, Extra: nil}
	}

	activity, curated := curatedActivities[action]
	if !curated {
		return ActionMapping{Activity: passThroughActivity(action), Extra: nil}
	}

	if kind, isMcpCreate := mcpCreateKinds[action]; isMcpCreate {
		return ActionMapping{
			Activity: activity,
			Extra:    map[string]string{PropertyMcpKind: string(kind)},
		}
	}

	return ActionMapping{Activity: activity, Extra: nil}
}

// passThroughActivity derives an activity name from a raw audit action:
// "toolset:create" becomes "toolset_create".
//
// Audit actions use several separators (":", "-", "_") and the activity
// property is read by humans in Slack and grouped on in PostHog, so every
// non-alphanumeric run collapses to a single underscore. An action with no
// alphanumeric content at all names nothing and is skipped rather than emitted
// as a blank activity.
func passThroughActivity(action audit.Action) Activity {
	var name strings.Builder
	name.Grow(len(action))

	separatorPending := false
	for _, r := range strings.ToLower(string(action)) {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			separatorPending = true
			continue
		}

		if separatorPending && name.Len() > 0 {
			name.WriteByte('_')
		}
		separatorPending = false
		name.WriteRune(r)
	}

	if name.Len() == 0 {
		return ActivitySkip
	}

	return Activity(name.String())
}
