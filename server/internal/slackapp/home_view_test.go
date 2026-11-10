package slackapp

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func encodeHome(t *testing.T, data HomeData) string {
	t.Helper()
	encoded, err := json.Marshal(HomeView(data))
	require.NoError(t, err)
	return string(encoded)
}

func TestHomeViewPermissionSections(t *testing.T) {
	t.Parallel()
	data := HomeData{Linked: true, DashboardURL: "https://example.com", OrganizationName: "Example", Servers: []HomeServer{{Name: "Team MCP", ProjectName: "Default", URL: "https://example.com/mcp"}}}
	encoded := encodeHome(t, data)
	require.Contains(t, encoded, "Team MCP")
	require.NotContains(t, encoded, "All clear")
	require.NotContains(t, encoded, "Recent risk events")
	require.NotContains(t, encoded, "Waiting on your review")
	// Even if stale data is present, a denied section must not render it.
	data.Risks = []HomeRisk{{PolicyName: "hidden risk"}}
	data.Approvals = []HomeApproval{{ToolName: "hidden approval"}}
	data.MCPRequests = []HomeMCPRequest{{Name: "hidden request"}}
	encoded = encodeHome(t, data)
	require.NotContains(t, encoded, "hidden")
	data.Linked = false
	encoded = encodeHome(t, data)
	require.Contains(t, encoded, "Ask your organization admin")
	require.NotContains(t, encoded, "Team MCP")
}

func TestHomeViewAllClearRequiresSuccessfulChecks(t *testing.T) {
	t.Parallel()
	data := HomeData{Linked: true, RiskEnabled: true, ApprovalsEnabled: true, MCPRequestsEnabled: true}
	require.Contains(t, encodeHome(t, data), "All clear")
	baseline := data
	for _, section := range []string{"risk", "approvals", "requests"} {
		t.Run(section, func(t *testing.T) {
			t.Parallel()
			broken := baseline
			switch section {
			case "risk":
				broken.RiskError = true
			case "approvals":
				broken.ApprovalsError = true
			case "requests":
				broken.MCPRequestsError = true
			}
			encoded := encodeHome(t, broken)
			require.NotContains(t, encoded, "All clear")
			require.Contains(t, encoded, "temporarily unavailable")
		})
	}
	data.Risks = []HomeRisk{{PolicyName: "Credential exposure", Severity: "critical", ProjectName: "Default", CreatedAt: time.Date(2026, 1, 2, 15, 4, 0, 0, time.UTC)}}
	encoded := encodeHome(t, data)
	require.NotContains(t, encoded, "All clear")
	require.Contains(t, encoded, "Critical")
	require.Contains(t, encoded, "2 Jan 2026, 15:04 UTC")
}

func TestHomeViewApprovalsAndSelfApproval(t *testing.T) {
	t.Parallel()
	data := HomeData{Linked: true, ApprovalsEnabled: true, Approvals: []HomeApproval{{ID: "approval", ProjectID: "project", ToolName: "delete_repo", PolicyName: "Protect writes", RequesterName: "Example user", CanDecide: true, URL: "https://example.com/review"}}}
	encoded := encodeHome(t, data)
	require.Contains(t, encoded, `"action_id":"home_approval_approve"`)
	require.Contains(t, encoded, `"action_id":"home_approval_deny"`)
	require.Contains(t, encoded, `"value":"project:approval"`)
	require.Contains(t, encoded, `"confirm":`)
	data.Approvals[0].CanDecide = false
	encoded = encodeHome(t, data)
	require.NotContains(t, encoded, `"action_id":"home_approval_approve"`)
	require.Contains(t, encoded, "Waiting for another approver")
}

func TestHomeViewBoundedCards(t *testing.T) {
	t.Parallel()
	name := "<@U123> *not markup* " + strings.Repeat("界", 4000)
	data := HomeData{Linked: true, ApprovalsEnabled: true, MCPRequestsEnabled: true, RiskEnabled: true}
	for range 10 {
		data.Approvals = append(data.Approvals, HomeApproval{ToolName: name, RequesterName: name, PolicyName: name, CanDecide: true})
		data.MCPRequests = append(data.MCPRequests, HomeMCPRequest{Name: name, ProjectName: name})
		data.Risks = append(data.Risks, HomeRisk{PolicyName: name, ProjectName: name})
		data.Servers = append(data.Servers, HomeServer{Name: name, ProjectName: name})
	}
	view := HomeView(data)
	blocks, ok := view["blocks"].([]any)
	require.True(t, ok)
	require.Less(t, len(blocks), 100)
	encoded := encodeHome(t, data)
	require.Contains(t, encoded, `"type":"mrkdwn"`)
	require.Contains(t, encoded, "not markup")
	var tree any
	require.NoError(t, json.Unmarshal([]byte(encoded), &tree))
	checkHomeTextLimits(t, tree)
}

func checkHomeTextLimits(t *testing.T, value any) {
	t.Helper()
	switch value := value.(type) {
	case map[string]any:
		if value["type"] == "plain_text" || value["type"] == "mrkdwn" {
			text, ok := value["text"].(string)
			require.True(t, ok)
			require.LessOrEqual(t, len([]rune(text)), 3000)
		}
		for _, child := range value {
			checkHomeTextLimits(t, child)
		}
	case []any:
		for _, child := range value {
			checkHomeTextLimits(t, child)
		}
	}
}
