package assistants

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestMCPDeliveryMode_ManagedInstructions(t *testing.T) {
	t.Parallel()

	perServer := RenderManagedAssistantInstructions(managedAssistantInstructions, MCPDeliveryPerServer)
	require.Equal(t, managedAssistantInstructions, perServer, "per-server mode must be byte-for-byte unchanged")

	gateway := RenderManagedAssistantInstructions(managedAssistantInstructions, MCPDeliveryProjectGateway)
	require.NotEqual(t, managedAssistantInstructions, gateway)
	require.NotContains(t, gateway, "tool_search")
	require.NotContains(t, gateway, "`select:`")
	require.Contains(t, gateway, "server slug `platform`")
	require.Contains(t, gateway, "execute_read_only_tool")
	require.Contains(t, gateway, "execute_tool")
	require.Contains(t, gateway, "platform--create_skill")
	require.Contains(t, gateway, "platform--distribute_skill")
	require.Contains(t, gateway, "platform--undistribute_skill")
	require.Contains(t, gateway, "get_project_overview")
}

func TestMCPDeliveryMode_ComposeInstructionsAndSkills(t *testing.T) {
	t.Parallel()

	skills := []assistantSkillSnapshot{
		{SkillID: uuid.New(), Name: "incident-triage", Description: "Guide for triaging incidents"},
	}
	slackThread := assistantThreadRecord{
		ID:            uuid.New(),
		AssistantID:   uuid.New(),
		ProjectID:     uuid.New(),
		CorrelationID: "slack:T1:C1:1.0",
		ChatID:        uuid.New(),
		SourceKind:    sourceKindSpeakeasySlack,
		SourceRefJSON: []byte(`{"team_id":"T1","channel_id":"C1","thread_id":"1.0","user_id":"U1"}`),
		LastEventAt:   time.Now(),
	}
	prefix := "https://app.example.com/mcp/connection_recovery/"

	perServer, err := ComposeInstructionsForMode("Base", slackThread, skills, prefix, MCPDeliveryPerServer)
	require.NoError(t, err)
	legacy, err := composeInstructions("Base", slackThread, skills, prefix)
	require.NoError(t, err)
	require.Equal(t, legacy, perServer)
	require.Contains(t, perServer, "mcp__p-assistants_skills_load")
	require.Contains(t, perServer, "assistant_mcp_auth_required")
	require.Contains(t, perServer, "tool_search")

	gateway, err := ComposeInstructionsForMode("Base", slackThread, skills, prefix, MCPDeliveryProjectGateway)
	require.NoError(t, err)
	require.NotContains(t, gateway, "mcp__p-assistants_skills_load")
	require.NotContains(t, gateway, "assistant_mcp_auth_required")
	require.NotContains(t, gateway, "tool_search")
	require.Contains(t, gateway, "execute_read_only_tool with assistants--skills_load")
	require.Contains(t, gateway, "execute_tool with speakeasy-slack--platform_slack_send_message")
	require.Contains(t, gateway, "execute_tool with speakeasy-slack--platform_slack_post_ephemeral")
	require.NotContains(t, gateway, "speakeasy-slack--platform_slack_post_message")
	require.Contains(t, gateway, "execute_tool with assistants--platform_thread_unsubscribe")
	require.Contains(t, gateway, prefix)
	require.Contains(t, gateway, "connection_status")

	// Custom Slack app (not Speakeasy Slack) prompts discovery via list_servers
	// rather than hardcoding speakeasy-slack.
	customSlackThread := slackThread
	customSlackThread.SourceKind = sourceKindSlack
	customGateway, err := ComposeInstructionsForMode("Base", customSlackThread, nil, prefix, MCPDeliveryProjectGateway)
	require.NoError(t, err)
	require.Contains(t, customGateway, "list_servers")
	require.Contains(t, customGateway, "execute_tool with <server_slug>--platform_slack_send_message")
	require.Contains(t, customGateway, "execute_tool with assistants--platform_thread_unsubscribe")

	// Dashboard adapter omits legacy assistant_mcp_auth_required in gateway mode
	// while keeping personal connection recovery guidance.
	dashThread := assistantThreadRecord{
		ID:            uuid.New(),
		AssistantID:   uuid.New(),
		ProjectID:     uuid.New(),
		CorrelationID: uuid.NewString(),
		ChatID:        uuid.New(),
		SourceKind:    sourceKindDashboard,
		SourceRefJSON: []byte(`{"user_id":"user-1"}`),
		LastEventAt:   time.Now(),
	}
	dashGateway, err := ComposeInstructionsForMode("Base", dashThread, nil, prefix, MCPDeliveryProjectGateway)
	require.NoError(t, err)
	require.NotContains(t, dashGateway, "assistant_mcp_auth_required")
	require.Contains(t, dashGateway, `When relaying a genuine "Connection required" or "Reconnect required" link`)
}

func TestMCPDeliveryMode_SlackTurnDecodeAndSkillSnapshot(t *testing.T) {
	t.Parallel()

	event := assistantThreadEventRecord{
		EventID:               "evt-1",
		NormalizedPayloadJSON: []byte(`{"event_type":"message","addressed":true,"team_id":"T1","channel_id":"C1","thread_id":"1.0","text":"hi","thread_context":{"unavailable":true}}`),
	}
	adapter, err := getSourceAdapter(sourceKindSpeakeasySlack)
	require.NoError(t, err)

	perServerTurn, err := adapterDecodeTurn(adapter, event, MCPDeliveryPerServer)
	require.NoError(t, err)
	require.Contains(t, perServerTurn, "read them with platform_slack_read_thread_messages")

	gatewayTurn, err := adapterDecodeTurn(adapter, event, MCPDeliveryProjectGateway)
	require.NoError(t, err)
	require.Contains(t, gatewayTurn, "execute_read_only_tool with <server_slug>--platform_slack_read_thread_messages")
	require.NotContains(t, gatewayTurn, "speakeasy-slack--platform_slack_read_thread_messages")

	prev := assistantSkillSetSnapshot{Version: assistantSkillSnapshotVersion, Skills: nil}
	curr := assistantSkillSetSnapshot{
		Version: assistantSkillSnapshotVersion,
		Skills: []assistantSkillSnapshot{
			{SkillID: uuid.New(), Name: "runbook", Description: "Ops runbook"},
		},
	}
	perServerChange := renderAssistantSkillSetChangeForMode(prev, curr, MCPDeliveryPerServer)
	require.Contains(t, perServerChange, `Call skills_load with name "runbook"`)

	gatewayChange := renderAssistantSkillSetChangeForMode(prev, curr, MCPDeliveryProjectGateway)
	require.Contains(t, gatewayChange, `Call execute_read_only_tool with assistants--skills_load for name "runbook"`)
}
