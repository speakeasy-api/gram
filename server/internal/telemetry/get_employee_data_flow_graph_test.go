package telemetry_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/stretchr/testify/require"
)

func TestGetEmployeeDataFlowGraph_MissingUserID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	now := time.Now().UTC()
	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	_, err := ti.service.GetEmployeeDataFlowGraph(ctx, &gen.GetEmployeeDataFlowGraphPayload{
		From: from,
		To:   to,
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "either user_id or external_user_id is required")
}

func TestGetEmployeeDataFlowGraph_Empty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	now := time.Now().UTC()
	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)
	userID := "nonexistent-user-" + uuid.New().String()

	result, err := ti.service.GetEmployeeDataFlowGraph(ctx, &gen.GetEmployeeDataFlowGraphPayload{
		From:   from,
		To:     to,
		UserID: &userID,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.Nodes)
	require.Empty(t, result.Edges)
}

func TestGetEmployeeDataFlowGraph_WithHookToolCalls(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()
	userID := "flow-user-" + uuid.New().String()
	otherUserID := "other-flow-user-" + uuid.New().String()

	now := time.Now().UTC()
	cursor := "cursor"
	claudeCode := "claude-code"
	githubServer := "github"
	listIssues := "list_issues"
	createIssue := "create_issue"
	shellTool := "shell"
	searchRepos := "search_repos"
	hookEvent := "hook"
	toolCallEvent := "tool_call"
	endpointHostname := "subomi-mbp"
	remoteServerID := uuid.NewString()

	insertTelemetryLogWithParams(t, ctx, testLogParams{
		projectID:    projectID,
		deploymentID: deploymentID,
		timestamp:    now.Add(-10 * time.Minute),
		gramURN:      "",
		severity:     "INFO",
		serviceName:  "gram-hooks",
		toolName:     &listIssues,
		toolSource:   &githubServer,
		eventSource:  &hookEvent,
		customAttrs: map[string]any{
			"user.id":                 userID,
			"gram.hook.source":        cursor,
			"gram.hook.event":         "PostToolUse",
			"gram.hook.hostname":      endpointHostname,
			"gram.external_mcp.name":  "GitHub",
			"gram.mcp.server_url":     "https://api.github.com/mcp",
			"gen_ai.response.model":   "claude-sonnet-4",
			"gen_ai.tool.call.result": "ok",
		},
	})
	insertTelemetryLogWithParams(t, ctx, testLogParams{
		projectID:    projectID,
		deploymentID: deploymentID,
		timestamp:    now.Add(-9 * time.Minute),
		gramURN:      "",
		severity:     "INFO",
		serviceName:  "gram-hooks",
		toolName:     &createIssue,
		toolSource:   &githubServer,
		eventSource:  &hookEvent,
		customAttrs: map[string]any{
			"user.id":                userID,
			"gram.hook.source":       cursor,
			"gram.hook.event":        "PostToolUseFailure",
			"gram.hook.hostname":     endpointHostname,
			"gram.external_mcp.name": "GitHub",
			"gram.mcp.server_url":    "https://api.github.com/mcp",
			"gen_ai.response.model":  "claude-sonnet-4",
			"gram.hook.error":        "blocked",
		},
	})
	insertTelemetryLogWithParams(t, ctx, testLogParams{
		projectID:    projectID,
		deploymentID: deploymentID,
		timestamp:    now.Add(-8 * time.Minute),
		gramURN:      "",
		severity:     "INFO",
		serviceName:  "gram-hooks",
		toolName:     &shellTool,
		eventSource:  &hookEvent,
		customAttrs: map[string]any{
			"user.id":                 userID,
			"gram.hook.source":        claudeCode,
			"gram.hook.event":         "PostToolUse",
			"gram.hook.hostname":      endpointHostname,
			"gen_ai.tool.call.result": "ok",
		},
	})
	insertTelemetryLogWithParams(t, ctx, testLogParams{
		projectID:    projectID,
		deploymentID: deploymentID,
		timestamp:    now.Add(-7 * time.Minute),
		gramURN:      "",
		severity:     "INFO",
		serviceName:  "gram-hooks",
		toolName:     &listIssues,
		toolSource:   &githubServer,
		eventSource:  &hookEvent,
		customAttrs: map[string]any{
			"user.id":                otherUserID,
			"gram.hook.source":       cursor,
			"gram.hook.event":        "PostToolUse",
			"gram.external_mcp.name": "GitHub",
		},
	})
	statusOK := int32(200)
	insertTelemetryLogWithParams(t, ctx, testLogParams{
		projectID:    projectID,
		deploymentID: deploymentID,
		timestamp:    now.Add(-6 * time.Minute),
		gramURN:      "tools:externalmcp:" + remoteServerID + ":" + searchRepos,
		severity:     "INFO",
		serviceName:  "gram-server",
		toolName:     &searchRepos,
		eventSource:  &toolCallEvent,
		httpStatus:   &statusOK,
		customAttrs: map[string]any{
			"user.id":                   userID,
			"gram.remote_mcp_server.id": remoteServerID,
		},
	})

	time.Sleep(200 * time.Millisecond)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)
	result, err := ti.service.GetEmployeeDataFlowGraph(ctx, &gen.GetEmployeeDataFlowGraphPayload{
		From:   from,
		To:     to,
		UserID: &userID,
	})

	require.NoError(t, err)
	require.NotNil(t, result)

	endpointNode := findEmployeeDataFlowNode(result.Nodes, "endpoint", endpointHostname)
	require.NotNil(t, endpointNode)
	require.Equal(t, int64(3), endpointNode.TotalCalls)
	require.Nil(t, findEmployeeDataFlowNode(result.Nodes, "endpoint", "https://api.github.com/mcp"))

	githubNode := findEmployeeDataFlowNode(result.Nodes, "server", "GitHub")
	require.NotNil(t, githubNode)
	require.NotNil(t, githubNode.ServerClass)
	require.Equal(t, "external", *githubNode.ServerClass)
	require.Equal(t, int64(2), githubNode.TotalCalls)

	localNode := findEmployeeDataFlowNode(result.Nodes, "server", "local")
	require.NotNil(t, localNode)
	require.NotNil(t, localNode.ServerClass)
	require.Equal(t, "local", *localNode.ServerClass)
	require.Equal(t, int64(1), localNode.TotalCalls)

	cursorNode := findEmployeeDataFlowNode(result.Nodes, "client", "cursor")
	require.NotNil(t, cursorNode)
	require.Equal(t, int64(2), cursorNode.TotalCalls)

	edge := findEmployeeDataFlowEdge(result.Edges, endpointNode.ID, cursorNode.ID)
	require.NotNil(t, edge)
	require.Equal(t, int64(2), edge.CallCount)

	listIssuesNode := findEmployeeDataFlowNode(result.Nodes, "tool", "list_issues")
	require.NotNil(t, listIssuesNode)
	edge = findEmployeeDataFlowEdge(result.Edges, githubNode.ID, listIssuesNode.ID)
	require.NotNil(t, edge)
	require.Equal(t, int64(1), edge.CallCount)
	require.Equal(t, int64(1), edge.SuccessCount)
	require.Equal(t, int64(0), edge.FailureCount)

	createIssueNode := findEmployeeDataFlowNode(result.Nodes, "tool", "create_issue")
	require.NotNil(t, createIssueNode)
	edge = findEmployeeDataFlowEdge(result.Edges, githubNode.ID, createIssueNode.ID)
	require.NotNil(t, edge)
	require.Equal(t, int64(1), edge.CallCount)
	require.Equal(t, int64(0), edge.SuccessCount)
	require.Equal(t, int64(1), edge.FailureCount)
}

func findEmployeeDataFlowNode(nodes []*gen.EmployeeDataFlowNode, tier, label string) *gen.EmployeeDataFlowNode {
	for _, node := range nodes {
		if node.Tier == tier && node.Label == label {
			return node
		}
	}
	return nil
}

func findEmployeeDataFlowEdge(edges []*gen.EmployeeDataFlowEdge, source, target string) *gen.EmployeeDataFlowEdge {
	for _, edge := range edges {
		if edge.Source == source && edge.Target == target {
			return edge
		}
	}
	return nil
}
