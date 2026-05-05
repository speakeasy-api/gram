package risk_analysis_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	deploymentsrepo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	tsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestAnalyzeBatch_EmptyMessageIDs(t *testing.T) {
	t.Parallel()
	ab := risk_analysis.NewAnalyzeBatch(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), nil, &risk_analysis.StubPIIScanner{}, nil)
	require.NotNil(t, ab)

	result, err := ab.Do(t.Context(), risk_analysis.AnalyzeBatchArgs{
		MessageIDs: nil,
		Sources:    []string{"gitleaks"},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.Processed)
	assert.Equal(t, 0, result.Findings)
}

func TestAnalyzeBatch_GracefulDegradationWhenPresidioDown(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)

	// Insert a message with a gitleaks-detectable secret
	msgID, err := testrepo.New(conn).InsertChatMessage(t.Context(), testrepo.InsertChatMessageParams{
		ChatID:    td.chatID,
		ProjectID: uuid.NullUUID{UUID: td.projectID, Valid: true},
		Role:      "user",
		Content:   "AWS key AKIAIOSFODNN7REALKEY and email alice@example.com",
	})
	require.NoError(t, err)

	// PresidioClient pointed at a dead URL simulates Presidio being down
	deadClient := risk_analysis.NewPresidioClient(
		"http://127.0.0.1:1",
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		testenv.NewLogger(t),
	)

	ab := risk_analysis.NewAnalyzeBatch(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		conn,
		deadClient,
		nil,
	)

	// Execute via Temporal test activity environment to satisfy activity.RecordHeartbeat
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivity(ab.Do)

	val, err := env.ExecuteActivity(ab.Do, risk_analysis.AnalyzeBatchArgs{
		ProjectID:      td.projectID,
		OrganizationID: td.orgID,
		RiskPolicyID:   td.policyID,
		PolicyVersion:  td.policyVersion,
		MessageIDs:     []uuid.UUID{msgID},
		Sources:        []string{"gitleaks", "presidio"},
	})
	require.NoError(t, err, "should not fail when presidio is down")

	var result risk_analysis.AnalyzeBatchResult
	require.NoError(t, val.Get(&result))
	assert.Equal(t, 1, result.Processed)
	assert.Positive(t, result.Findings, "gitleaks findings should be preserved when presidio is down")
}

func TestAnalyzeBatch_DestructiveToolAnnotationFinding(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)

	destructive := true
	toolsetID := seedHTTPToolset(t, conn, td, "delete_records", &destructive)
	msgID := insertAssistantToolCall(t, conn, td, "mcp__gram__delete_records", toolsetID)

	result := executeAnalyzeBatch(t, conn, td, []uuid.UUID{msgID}, []string{shadowmcp.SourceDestructiveTool})
	require.Equal(t, 1, result.Processed)
	require.Equal(t, 1, result.Findings)

	rows, err := riskrepo.New(conn).ListRiskResultsByProjectAndPolicy(t.Context(), riskrepo.ListRiskResultsByProjectAndPolicyParams{
		ProjectID:    td.projectID,
		RiskPolicyID: td.policyID,
		Cursor:       uuid.NullUUID{},
		PageLimit:    10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, msgID, rows[0].ChatMessageID)
	require.True(t, rows[0].Found)
	require.Equal(t, shadowmcp.SourceDestructiveTool, rows[0].Source)
	require.Equal(t, "destructive_tool.annotation", rows[0].RuleID.String)
	require.Equal(t, "delete_records", rows[0].Match.String)
}

func TestAnalyzeBatch_DestructiveToolAnnotationSkipsFalseHint(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)

	destructive := false
	toolsetID := seedHTTPToolset(t, conn, td, "read_records", &destructive)
	msgID := insertAssistantToolCall(t, conn, td, "MCP:read_records", toolsetID)

	result := executeAnalyzeBatch(t, conn, td, []uuid.UUID{msgID}, []string{shadowmcp.SourceDestructiveTool})
	require.Equal(t, 1, result.Processed)
	require.Equal(t, 0, result.Findings)
}

func executeAnalyzeBatch(t *testing.T, conn *pgxpool.Pool, td testData, messageIDs []uuid.UUID, sources []string) risk_analysis.AnalyzeBatchResult {
	t.Helper()

	shadowMCPClient := shadowmcp.NewClient(testenv.NewLogger(t), conn, cache.NoopCache)
	ab := risk_analysis.NewAnalyzeBatch(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		conn,
		&risk_analysis.StubPIIScanner{},
		shadowMCPClient,
	)

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivity(ab.Do)

	val, err := env.ExecuteActivity(ab.Do, risk_analysis.AnalyzeBatchArgs{
		ProjectID:      td.projectID,
		OrganizationID: td.orgID,
		RiskPolicyID:   td.policyID,
		PolicyVersion:  td.policyVersion,
		MessageIDs:     messageIDs,
		Sources:        sources,
	})
	require.NoError(t, err)

	var result risk_analysis.AnalyzeBatchResult
	require.NoError(t, val.Get(&result))
	return result
}

func seedHTTPToolset(t *testing.T, conn *pgxpool.Pool, td testData, toolName string, destructiveHint *bool) uuid.UUID {
	t.Helper()
	ctx := t.Context()

	toolset, err := tsrepo.New(conn).CreateToolset(ctx, tsrepo.CreateToolsetParams{
		OrganizationID:         td.orgID,
		ProjectID:              td.projectID,
		Name:                   "ts-" + uuid.NewString()[:8],
		Slug:                   "ts-" + uuid.NewString()[:8],
		Description:            pgtype.Text{},
		DefaultEnvironmentSlug: pgtype.Text{},
		McpSlug:                pgtype.Text{},
		McpEnabled:             false,
	})
	require.NoError(t, err)

	deploymentID := seedCompletedDeployment(t, conn, td.projectID, td.orgID)
	toolURN := urn.NewTool(urn.ToolKindHTTP, "test-api", uuid.NewString()[:8])
	var destructive pgtype.Bool
	if destructiveHint != nil {
		destructive = pgtype.Bool{Bool: *destructiveHint, Valid: true}
	}
	_, err = deploymentsrepo.New(conn).CreateOpenAPIv3ToolDefinition(ctx, deploymentsrepo.CreateOpenAPIv3ToolDefinitionParams{
		ProjectID:           td.projectID,
		DeploymentID:        deploymentID,
		Openapiv3DocumentID: uuid.NullUUID{},
		ToolUrn:             toolURN,
		Name:                toolName,
		UntruncatedName:     pgtype.Text{String: "", Valid: true},
		Openapiv3Operation:  pgtype.Text{},
		Summary:             "Test tool",
		Description:         "A test tool",
		Tags:                []string{},
		Confirm:             pgtype.Text{},
		ConfirmPrompt:       pgtype.Text{},
		XGram:               pgtype.Bool{},
		OriginalName:        pgtype.Text{},
		OriginalSummary:     pgtype.Text{},
		OriginalDescription: pgtype.Text{},
		Security:            []byte("[]"),
		HttpMethod:          "POST",
		Path:                "/test",
		SchemaVersion:       "3.0.0",
		Schema:              []byte("{}"),
		HeaderSettings:      []byte("{}"),
		QuerySettings:       []byte("{}"),
		PathSettings:        []byte("{}"),
		ServerEnvVar:        "TEST_SERVER_URL",
		DefaultServerUrl:    pgtype.Text{},
		RequestContentType:  pgtype.Text{},
		ResponseFilter:      nil,
		ReadOnlyHint:        pgtype.Bool{},
		DestructiveHint:     destructive,
		IdempotentHint:      pgtype.Bool{},
		OpenWorldHint:       pgtype.Bool{},
	})
	require.NoError(t, err)

	_, err = tsrepo.New(conn).CreateToolsetVersion(ctx, tsrepo.CreateToolsetVersionParams{
		ToolsetID:     toolset.ID,
		Version:       1,
		ToolUrns:      []urn.Tool{toolURN},
		ResourceUrns:  []urn.Resource{},
		PredecessorID: uuid.NullUUID{},
	})
	require.NoError(t, err)

	return toolset.ID
}

func seedCompletedDeployment(t *testing.T, conn *pgxpool.Pool, projectID uuid.UUID, orgID string) uuid.UUID {
	t.Helper()
	ctx := t.Context()
	deployments := deploymentsrepo.New(conn)
	idempotencyKey := "test-" + uuid.NewString()

	_, err := deployments.CreateDeployment(ctx, deploymentsrepo.CreateDeploymentParams{
		IdempotencyKey: idempotencyKey,
		UserID:         "test-user",
		OrganizationID: orgID,
		ProjectID:      projectID,
		GithubRepo:     pgtype.Text{},
		GithubPr:       pgtype.Text{},
		GithubSha:      pgtype.Text{},
		ExternalID:     pgtype.Text{},
		ExternalUrl:    pgtype.Text{},
	})
	require.NoError(t, err)

	deployment, err := deployments.GetDeploymentByIdempotencyKey(ctx, deploymentsrepo.GetDeploymentByIdempotencyKeyParams{
		IdempotencyKey: idempotencyKey,
		ProjectID:      projectID,
	})
	require.NoError(t, err)

	for _, status := range []string{"created", "pending", "completed"} {
		_, err = deployments.TransitionDeployment(ctx, deploymentsrepo.TransitionDeploymentParams{
			DeploymentID: deployment.Deployment.ID,
			Status:       status,
			ProjectID:    projectID,
			Event:        "test",
			Message:      "test deployment status",
		})
		require.NoError(t, err)
	}

	return deployment.Deployment.ID
}

func insertAssistantToolCall(t *testing.T, conn *pgxpool.Pool, td testData, callName string, toolsetID uuid.UUID) uuid.UUID {
	t.Helper()

	args, err := json.Marshal(map[string]string{
		shadowmcp.XGramToolsetIDField: toolsetID.String(),
	})
	require.NoError(t, err)

	toolCalls, err := json.Marshal([]map[string]any{
		{
			"id":   "call_1",
			"type": "function",
			"function": map[string]any{
				"name":      callName,
				"arguments": string(args),
			},
		},
	})
	require.NoError(t, err)

	messageID := "msg-" + uuid.NewString()
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), conn, nil)
	t.Cleanup(func() { _ = shutdown(t.Context()) })
	_, err = writer.Write(t.Context(), td.projectID, []chatrepo.CreateChatMessageParams{{
		ChatID:           td.chatID,
		Role:             "assistant",
		ProjectID:        td.projectID,
		Content:          "",
		ContentRaw:       nil,
		ContentAssetUrl:  pgtype.Text{},
		StorageError:     pgtype.Text{},
		Model:            pgtype.Text{},
		MessageID:        pgtype.Text{String: messageID, Valid: true},
		ToolCallID:       pgtype.Text{},
		UserID:           pgtype.Text{},
		ExternalUserID:   pgtype.Text{},
		FinishReason:     pgtype.Text{String: "tool_calls", Valid: true},
		ToolCalls:        toolCalls,
		PromptTokens:     0,
		CompletionTokens: 0,
		TotalTokens:      0,
		Origin:           pgtype.Text{},
		UserAgent:        pgtype.Text{},
		IpAddress:        pgtype.Text{},
		Source:           pgtype.Text{},
		ContentHash:      nil,
		Generation:       0,
	}})
	require.NoError(t, err)

	messages, err := chatrepo.New(conn).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{
		ChatID:    td.chatID,
		ProjectID: td.projectID,
	})
	require.NoError(t, err)
	for _, msg := range messages {
		if msg.MessageID.String == messageID {
			return msg.ID
		}
	}
	require.FailNow(t, "inserted tool-call message not found")
	return uuid.Nil
}

func TestAnalyzeBatch_SkipsWhenPolicyDisabled(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, false)

	msgID, err := testrepo.New(conn).InsertChatMessage(t.Context(), testrepo.InsertChatMessageParams{
		ChatID:    td.chatID,
		ProjectID: uuid.NullUUID{UUID: td.projectID, Valid: true},
		Role:      "user",
		Content:   "AWS key AKIAIOSFODNN7REALKEY",
	})
	require.NoError(t, err)

	result := executeAnalyzeBatch(t, conn, td, []uuid.UUID{msgID}, []string{"gitleaks"})
	assert.Equal(t, 0, result.Processed)
	assert.Equal(t, 0, result.Findings)

	rows, err := riskrepo.New(conn).ListRiskResultsByProjectAndPolicy(t.Context(), riskrepo.ListRiskResultsByProjectAndPolicyParams{
		ProjectID:    td.projectID,
		RiskPolicyID: td.policyID,
		Cursor:       uuid.NullUUID{},
		PageLimit:    10,
	})
	require.NoError(t, err)
	assert.Empty(t, rows, "no risk_results should be written for a disabled policy")
}

func TestAnalyzeBatch_SkipsWhenPolicyDeleted(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)

	msgID, err := testrepo.New(conn).InsertChatMessage(t.Context(), testrepo.InsertChatMessageParams{
		ChatID:    td.chatID,
		ProjectID: uuid.NullUUID{UUID: td.projectID, Valid: true},
		Role:      "user",
		Content:   "AWS key AKIAIOSFODNN7REALKEY",
	})
	require.NoError(t, err)

	require.NoError(t, riskrepo.New(conn).DeleteRiskPolicy(t.Context(), riskrepo.DeleteRiskPolicyParams{
		ID:        td.policyID,
		ProjectID: td.projectID,
	}))

	result := executeAnalyzeBatch(t, conn, td, []uuid.UUID{msgID}, []string{"gitleaks"})
	assert.Equal(t, 0, result.Processed)
	assert.Equal(t, 0, result.Findings)
}
