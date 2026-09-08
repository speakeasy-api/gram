package growthsignals_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/growthsignals"
)

func TestActivityForActionCuratedMoments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		action audit.Action
		want   growthsignals.Activity
	}{
		{name: "project create", action: audit.ActionProjectCreate, want: growthsignals.ActivityProjectCreated},
		{name: "risk policy create", action: audit.ActionRiskPolicyCreate, want: growthsignals.ActivitySecurityPolicyCreated},
		{name: "risk policy update", action: audit.ActionRiskPolicyUpdate, want: growthsignals.ActivitySecurityPolicyUpdated},
		{name: "mcp server update", action: audit.ActionMcpServerUpdate, want: growthsignals.ActivityMcpServerUpdated},
		{name: "mcp metadata update", action: audit.ActionMCPMetadataUpdate, want: growthsignals.ActivityMcpServerUpdated},
		{name: "mcp tool metadata update", action: audit.ActionMcpServerToolMetadataUpdate, want: growthsignals.ActivityMcpServerUpdated},
		{name: "organization invite create", action: audit.ActionOrganizationInviteCreate, want: growthsignals.ActivityMemberInvited},
	}

	for _, tt := range tests {
		mapping := growthsignals.ActivityForAction(tt.action)

		require.Equal(t, tt.want, mapping.Activity, "activity for %s (%s)", tt.name, tt.action)
		require.Empty(t, mapping.Extra, "extras for %s (%s)", tt.name, tt.action)
	}
}

// The five MCP creation actions collapse to one activity, so the flavour has to
// survive as a property or the distinction is lost entirely.
func TestActivityForActionCarriesMcpKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		action audit.Action
		want   growthsignals.McpKind
	}{
		{name: "hosted", action: audit.ActionMcpServerCreate, want: growthsignals.McpKindHosted},
		{name: "remote", action: audit.ActionRemoteMcpServerCreate, want: growthsignals.McpKindRemote},
		{name: "tunneled", action: audit.ActionTunneledMcpServerCreate, want: growthsignals.McpKindTunneled},
		{name: "unproxied", action: audit.ActionUnproxiedMcpServerCreate, want: growthsignals.McpKindUnproxied},
		{name: "meta", action: audit.ActionMetaMcpServerCreate, want: growthsignals.McpKindMeta},
	}

	for _, tt := range tests {
		mapping := growthsignals.ActivityForAction(tt.action)

		require.Equal(t, growthsignals.ActivityMcpServerCreated, mapping.Activity, "activity for %s mcp create", tt.name)
		require.Equal(t, map[string]string{
			growthsignals.PropertyMcpKind: string(tt.want),
		}, mapping.Extra, "extras for %s mcp create", tt.name)
	}
}

func TestActivityForActionPassesThroughUncuratedActions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		action audit.Action
		want   growthsignals.Activity
	}{
		{name: "colon separator", action: audit.ActionToolsetCreate, want: "toolset_create"},
		{name: "hyphenated subject", action: audit.ActionMcpEndpointCreate, want: "mcp_endpoint_create"},
		{name: "already underscored", action: audit.ActionEnvironmentCreate, want: "environment_create"},
		{name: "mixed separators", action: audit.ActionRemoteSessionClientAttachJsonWebKeySet, want: "remote_session_client_attach_json_web_key_set"},
		{name: "deleted mcp server", action: audit.ActionMcpServerDelete, want: "mcp_server_delete"},
		{name: "unknown action", action: audit.Action("widget:frobnicate"), want: "widget_frobnicate"},
		{name: "repeated separators", action: audit.Action("widget::--frobnicate"), want: "widget_frobnicate"},
		{name: "leading and trailing separators", action: audit.Action(":widget:"), want: "widget"},
		{name: "uppercase", action: audit.Action("Widget:Frobnicate"), want: "widget_frobnicate"},
	}

	for _, tt := range tests {
		mapping := growthsignals.ActivityForAction(tt.action)

		require.Equal(t, tt.want, mapping.Activity, "activity for %s (%s)", tt.name, tt.action)
		require.Empty(t, mapping.Extra, "extras for %s (%s)", tt.name, tt.action)
	}
}

// An action with nothing to name is skipped rather than emitted as a blank
// activity, which would be indistinguishable from a bug in the taxonomy.
func TestActivityForActionSkipsUnnamableActions(t *testing.T) {
	t.Parallel()

	tests := []audit.Action{"", ":", "---", "::--"}

	for _, action := range tests {
		mapping := growthsignals.ActivityForAction(action)

		require.Equal(t, growthsignals.ActivitySkip, mapping.Activity, "activity for %q", action)
	}
}

func TestActivityForActionSkipsHighVolumeNoise(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		action audit.Action
	}{
		{name: "assistant tool call", action: audit.ActionAssistantToolCall},
		{name: "assistant wake scheduled", action: audit.ActionWakeScheduled},
		{name: "assistant wake fired", action: audit.ActionWakeFired},
		{name: "assistant wake cancelled", action: audit.ActionWakeCancelled},
		{name: "chat session access", action: audit.ActionChatSessionAccess},
		{name: "platform mcp diagnostics read", action: audit.ActionPlatformMcpDiagnosticsUserStatusRead},
	}

	for _, tt := range tests {
		mapping := growthsignals.ActivityForAction(tt.action)

		require.Equal(t, growthsignals.ActivitySkip, mapping.Activity, "activity for %s (%s)", tt.name, tt.action)
		require.Empty(t, mapping.Extra, "extras for %s (%s)", tt.name, tt.action)
	}
}

// Callers merge their own per-event extras into the returned map, so it must
// not be the map the package keeps.
func TestActivityForActionReturnsFreshExtras(t *testing.T) {
	t.Parallel()

	first := growthsignals.ActivityForAction(audit.ActionMcpServerCreate)
	first.Extra[growthsignals.PropertyMcpKind] = "tampered"
	first.Extra["added"] = "by caller"

	second := growthsignals.ActivityForAction(audit.ActionMcpServerCreate)

	require.Equal(t, map[string]string{
		growthsignals.PropertyMcpKind: string(growthsignals.McpKindHosted),
	}, second.Extra)
}
