package telemetry_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/stretchr/testify/assert"
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
	originHostname := "subomi-mbp"
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
			"gram.hook.hostname":      originHostname,
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
			"gram.hook.hostname":     originHostname,
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
			"gram.hook.hostname":      originHostname,
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

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.GetEmployeeDataFlowGraph(ctx, &gen.GetEmployeeDataFlowGraphPayload{
			From:   from,
			To:     to,
			UserID: &userID,
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		assert.NotEmpty(c, res.Nodes)

		// Guard every node/edge lookup before dereferencing: assert.NotNil only
		// records a failure, so without an early return a nil hit on a not-yet-
		// converged poll iteration would panic and abort the whole test.
		originNode := findEmployeeDataFlowNode(res.Nodes, "origin", originHostname)
		if !assert.NotNil(c, originNode) {
			return
		}
		assert.Equal(c, int64(3), originNode.TotalCalls)
		assert.Nil(c, findEmployeeDataFlowNode(res.Nodes, "origin", "https://api.github.com/mcp"))

		githubNode := findEmployeeDataFlowNode(res.Nodes, "server", "GitHub")
		if !assert.NotNil(c, githubNode) {
			return
		}
		if !assert.NotNil(c, githubNode.ServerClass) {
			return
		}
		assert.Equal(c, "external", *githubNode.ServerClass)
		assert.Equal(c, int64(2), githubNode.TotalCalls)

		localNode := findEmployeeDataFlowNode(res.Nodes, "server", "local")
		if !assert.NotNil(c, localNode) {
			return
		}
		if !assert.NotNil(c, localNode.ServerClass) {
			return
		}
		assert.Equal(c, "local", *localNode.ServerClass)
		assert.Equal(c, int64(1), localNode.TotalCalls)

		cursorNode := findEmployeeDataFlowNode(res.Nodes, "client", "cursor")
		if !assert.NotNil(c, cursorNode) {
			return
		}
		assert.Equal(c, int64(2), cursorNode.TotalCalls)

		edge := findEmployeeDataFlowEdge(res.Edges, originNode.ID, cursorNode.ID)
		if !assert.NotNil(c, edge) {
			return
		}
		assert.Equal(c, int64(2), edge.CallCount)

		listIssuesNode := findEmployeeDataFlowNode(res.Nodes, "tool", "list_issues")
		if !assert.NotNil(c, listIssuesNode) {
			return
		}
		edge = findEmployeeDataFlowEdge(res.Edges, githubNode.ID, listIssuesNode.ID)
		if !assert.NotNil(c, edge) {
			return
		}
		assert.Equal(c, int64(1), edge.CallCount)
		assert.Equal(c, int64(1), edge.SuccessCount)
		assert.Equal(c, int64(0), edge.FailureCount)

		createIssueNode := findEmployeeDataFlowNode(res.Nodes, "tool", "create_issue")
		if !assert.NotNil(c, createIssueNode) {
			return
		}
		edge = findEmployeeDataFlowEdge(res.Edges, githubNode.ID, createIssueNode.ID)
		if !assert.NotNil(c, edge) {
			return
		}
		assert.Equal(c, int64(1), edge.CallCount)
		assert.Equal(c, int64(0), edge.SuccessCount)
		assert.Equal(c, int64(1), edge.FailureCount)
	}, 10*time.Second, 200*time.Millisecond)
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
