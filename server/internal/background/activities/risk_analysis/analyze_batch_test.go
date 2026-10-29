package risk_analysis_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	deploymentsrepo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	tsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// recordingPromptJudge captures judge inputs under a mutex: judgeFanout calls
// Evaluate from concurrent goroutines.
type recordingPromptJudge struct {
	mu     sync.Mutex
	inputs []promptpolicy.Input
}

func (j *recordingPromptJudge) recorded() []promptpolicy.Input {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]promptpolicy.Input, len(j.inputs))
	copy(out, j.inputs)
	return out
}

func (j *recordingPromptJudge) Evaluate(_ context.Context, in promptpolicy.Input) (*promptpolicy.Verdict, error) {
	j.mu.Lock()
	j.inputs = append(j.inputs, in)
	j.mu.Unlock()
	return &promptpolicy.Verdict{
		Matched:          true,
		Confidence:       0.9,
		Rationale:        "matched tool call",
		CostUSD:          0,
		PromptTokens:     0,
		CompletionTokens: 0,
		TotalTokens:      0,
		STokens:          3,
		Completed:        true,
		Model:            "test-model",
		Provider:         "test-provider",
	}, nil
}

// newPresidioPub returns a mock presidio publisher that accepts any publish
// call and reports success. Tests that don't enable the "presidio" source
// never invoke it; tests that do get a fire-and-forget success. Wiring a real
// mock (never nil) keeps the publish path off the nil-deref cliff.
func newPresidioPub() *gcp.MockPublisher[*riskv1.PresidioAnalysis] {
	pub := gcp.NewMockPublisher[*riskv1.PresidioAnalysis]()
	pub.On("Publish", mock.Anything, mock.Anything).Return(gcp.NewSuccessPublishResult())
	return pub
}

// mustCELEngine builds a real CEL engine for tests; construction is
// deterministic so a failure is a fatal setup error.
func mustCELEngine(t *testing.T) *celenv.Engine {
	t.Helper()
	eng, err := celenv.New()
	require.NoError(t, err)
	return eng
}

func newGitleaksPub() *gcp.MockPublisher[*riskv1.GitleaksAnalysis] {
	pub := gcp.NewMockPublisher[*riskv1.GitleaksAnalysis]()
	pub.On("Publish", mock.Anything, mock.Anything).Return(gcp.NewSuccessPublishResult())
	return pub
}

func newPromptInjectionPub() *gcp.MockPublisher[*riskv1.PromptInjectionAnalysis] {
	pub := gcp.NewMockPublisher[*riskv1.PromptInjectionAnalysis]()
	pub.On("Publish", mock.Anything, mock.Anything).Return(gcp.NewSuccessPublishResult())
	return pub
}

func newPromptPolicyPub() *gcp.MockPublisher[*riskv1.PromptPolicyAnalysis] {
	pub := gcp.NewMockPublisher[*riskv1.PromptPolicyAnalysis]()
	pub.On("Publish", mock.Anything, mock.Anything).Return(gcp.NewSuccessPublishResult())
	return pub
}

func capturingPromptInjectionPub(t *testing.T) (*gcp.MockPublisher[*riskv1.PromptInjectionAnalysis], *[]*riskv1.PromptInjectionAnalysis) {
	t.Helper()
	pub := gcp.NewMockPublisher[*riskv1.PromptInjectionAnalysis]()
	var published []*riskv1.PromptInjectionAnalysis
	pub.On("Publish", mock.Anything, mock.Anything).
		Return(gcp.NewSuccessPublishResult()).
		Run(func(args mock.Arguments) {
			msg, ok := args.Get(1).(*riskv1.PromptInjectionAnalysis)
			require.True(t, ok)
			published = append(published, msg)
		})
	return pub, &published
}

func capturingPromptPolicyPub(t *testing.T) (*gcp.MockPublisher[*riskv1.PromptPolicyAnalysis], *[]*riskv1.PromptPolicyAnalysis) {
	t.Helper()
	pub := gcp.NewMockPublisher[*riskv1.PromptPolicyAnalysis]()
	var published []*riskv1.PromptPolicyAnalysis
	pub.On("Publish", mock.Anything, mock.Anything).
		Return(gcp.NewSuccessPublishResult()).
		Run(func(args mock.Arguments) {
			msg, ok := args.Get(1).(*riskv1.PromptPolicyAnalysis)
			require.True(t, ok)
			published = append(published, msg)
		})
	return pub, &published
}
func newCustomRulesPub() *gcp.MockPublisher[*riskv1.CustomRulesAnalysis] {
	pub := gcp.NewMockPublisher[*riskv1.CustomRulesAnalysis]()
	pub.On("Publish", mock.Anything, mock.Anything).Return(gcp.NewSuccessPublishResult())
	return pub
}

func newToolCallPub() *gcp.MockPublisher[*riskv1.ToolCallAnalysis] {
	pub := gcp.NewMockPublisher[*riskv1.ToolCallAnalysis]()
	pub.On("Publish", mock.Anything, mock.Anything).Return(gcp.NewSuccessPublishResult())
	return pub
}

func newAccountIdentityPub() *gcp.MockPublisher[*riskv1.AccountIdentityAnalysis] {
	pub := gcp.NewMockPublisher[*riskv1.AccountIdentityAnalysis]()
	pub.On("Publish", mock.Anything, mock.Anything).Return(gcp.NewSuccessPublishResult())
	return pub
}

func capturingAccountIdentityPub(t *testing.T) (*gcp.MockPublisher[*riskv1.AccountIdentityAnalysis], *[]*riskv1.AccountIdentityAnalysis) {
	t.Helper()
	pub := gcp.NewMockPublisher[*riskv1.AccountIdentityAnalysis]()
	var published []*riskv1.AccountIdentityAnalysis
	pub.On("Publish", mock.Anything, mock.Anything).
		Return(gcp.NewSuccessPublishResult()).
		Run(func(args mock.Arguments) {
			msg, ok := args.Get(1).(*riskv1.AccountIdentityAnalysis)
			require.True(t, ok)
			published = append(published, msg)
		})
	return pub, &published
}

func capturingToolCallPub(t *testing.T) (*gcp.MockPublisher[*riskv1.ToolCallAnalysis], *[]*riskv1.ToolCallAnalysis) {
	t.Helper()
	pub := gcp.NewMockPublisher[*riskv1.ToolCallAnalysis]()
	var published []*riskv1.ToolCallAnalysis
	pub.On("Publish", mock.Anything, mock.Anything).
		Return(gcp.NewSuccessPublishResult()).
		Run(func(args mock.Arguments) {
			msg, ok := args.Get(1).(*riskv1.ToolCallAnalysis)
			require.True(t, ok)
			published = append(published, msg)
		})
	return pub, &published
}

func TestAnalyzeBatch_EmptyMessageIDs(t *testing.T) {
	t.Parallel()
	ab, err := risk_analysis.NewAnalyzeBatch(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		nil,
		nil,
		nil,
		newPresidioPub(),
		newGitleaksPub(),
		newPromptInjectionPub(),
		newPromptPolicyPub(),
		newCustomRulesPub(),
		newLLMPub(),
		newToolCallPub(),
		newAccountIdentityPub(),
		mustCELEngine(t),
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, ab)

	result, err := ab.Do(t.Context(), risk_analysis.AnalyzeBatchArgs{
		ProjectID:        uuid.MustParse("00000000-0000-0000-0000-000000000701"),
		OrganizationID:   "org",
		RiskPolicyID:     uuid.MustParse("00000000-0000-0000-0000-000000000702"),
		PolicyVersion:    1,
		MessageIDs:       nil,
		Sources:          []string{"gitleaks"},
		PresidioEntities: nil,
		CustomRuleIds:    nil,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.Processed)
}

func TestAnalyzeBatch_ContentSourcesDoNotPublishToolCallRequests(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)

	// Same synthetic AWS-style pair the presidio-degradation test seeds,
	// assembled at runtime so no contiguous fake credential sits in source.
	secret := "AccessKeyId ASIAZ2XY3WNBQR5TUVWX SecretAccessKey " + "wJalrXUtnFEMIbKp7MDoRZ" + "fiCYqTvHgNsQ8xLcWd"
	msgID, err := testrepo.New(conn).InsertChatMessage(t.Context(), testrepo.InsertChatMessageParams{
		ChatID:    td.chatID,
		ProjectID: uuid.NullUUID{UUID: td.projectID, Valid: true},
		Role:      "user",
		Content:   secret,
	})
	require.NoError(t, err)

	toolCallPub, published := capturingToolCallPub(t)
	ab, err := risk_analysis.NewAnalyzeBatch(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		conn,
		nil,
		nil,
		newPresidioPub(),
		newGitleaksPub(),
		newPromptInjectionPub(),
		newPromptPolicyPub(),
		newCustomRulesPub(),
		newLLMPub(),
		toolCallPub,
		newAccountIdentityPub(),
		mustCELEngine(t),
		false,
	)
	require.NoError(t, err)

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivity(ab.Do)

	val, err := env.ExecuteActivity(ab.Do, risk_analysis.AnalyzeBatchArgs{
		ProjectID:      td.projectID,
		OrganizationID: td.orgID,
		RiskPolicyID:   td.policyID,
		PolicyVersion:  td.policyVersion,
		MessageIDs:     []uuid.UUID{msgID},
		Sources:        []string{"gitleaks"},
	})
	require.NoError(t, err)

	var result risk_analysis.AnalyzeBatchResult
	require.NoError(t, val.Get(&result))
	require.Equal(t, 1, result.Processed)

	require.Empty(t, *published)
}

func TestContentPartBatchPreservesDeletedChatContentWithoutOwnerAttribution(t *testing.T) {
	t.Parallel()

	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	chats := chatrepo.New(conn)
	chatID, err := uuid.NewV7()
	require.NoError(t, err)
	_, err = chats.UpsertChat(t.Context(), chatrepo.UpsertChatParams{
		ID:             chatID,
		ProjectID:      td.projectID,
		OrganizationID: td.orgID,
		UserID:         pgtype.Text{String: "test-chat-owner", Valid: true},
		ExternalUserID: pgtype.Text{},
		Title:          pgtype.Text{String: "test chat", Valid: true},
	})
	require.NoError(t, err)
	queries := riskrepo.New(conn)
	partID, err := queries.CreateChatContentPartForTest(t.Context(), riskrepo.CreateChatContentPartForTestParams{
		ChatID:              chatID,
		ProjectID:           uuid.NullUUID{UUID: td.projectID, Valid: true},
		Kind:                message.PromptAttachment,
		ContentAssetUrl:     "test-content-asset",
		ParentChatMessageID: uuid.NullUUID{},
	})
	require.NoError(t, err)
	params := riskrepo.GetContentPartBatchParams{
		Ids:       []uuid.UUID{partID},
		ProjectID: uuid.NullUUID{UUID: td.projectID, Valid: true},
	}
	before, err := queries.GetContentPartBatch(t.Context(), params)
	require.NoError(t, err)
	require.Len(t, before, 1)
	require.Equal(t, "test-chat-owner", before[0].ChatUserID)

	deleted, err := chats.SoftDeleteChat(t.Context(), chatrepo.SoftDeleteChatParams{
		ProjectID: td.projectID,
		ID:        chatID,
	})
	require.NoError(t, err)
	require.True(t, deleted.Deleted)
	after, err := queries.GetContentPartBatch(t.Context(), params)
	require.NoError(t, err)
	require.Len(t, after, 1)
	require.Equal(t, partID, after[0].ID)
	require.Equal(t, chatID, after[0].ChatID)
	require.Equal(t, "test-content-asset", after[0].ContentAssetUrl)
	require.Empty(t, after[0].ChatUserID)
}

func TestBatchContentQueriesRejectForeignChatsAndMismatchedParent(t *testing.T) {
	t.Parallel()

	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	foreign := seedTestData(t, conn, true)
	queries := riskrepo.New(conn)

	foreignParentID, err := testrepo.New(conn).InsertChatMessage(t.Context(), testrepo.InsertChatMessageParams{
		ChatID:    foreign.chatID,
		ProjectID: uuid.NullUUID{UUID: foreign.projectID, Valid: true},
		Role:      "user",
		Content:   "foreign parent",
	})
	require.NoError(t, err)
	foreignChatMessageID, err := testrepo.New(conn).InsertChatMessage(t.Context(), testrepo.InsertChatMessageParams{
		ChatID:    foreign.chatID,
		ProjectID: uuid.NullUUID{UUID: td.projectID, Valid: true},
		Role:      "user",
		Content:   "foreign chat on requested project",
	})
	require.NoError(t, err)
	foreignChatPartID, err := queries.CreateChatContentPartForTest(t.Context(), riskrepo.CreateChatContentPartForTestParams{
		ChatID:              foreign.chatID,
		ProjectID:           uuid.NullUUID{UUID: td.projectID, Valid: true},
		Kind:                message.PromptAttachment,
		ContentAssetUrl:     "unused",
		ParentChatMessageID: uuid.NullUUID{UUID: foreignParentID, Valid: true},
	})
	require.NoError(t, err)

	otherChatID, err := uuid.NewV7()
	require.NoError(t, err)
	_, err = chatrepo.New(conn).UpsertChat(t.Context(), chatrepo.UpsertChatParams{
		ID:             otherChatID,
		ProjectID:      td.projectID,
		OrganizationID: td.orgID,
		UserID:         pgtype.Text{},
		ExternalUserID: pgtype.Text{},
		Title:          pgtype.Text{String: "other chat", Valid: true},
	})
	require.NoError(t, err)
	otherChatParentID, err := testrepo.New(conn).InsertChatMessage(t.Context(), testrepo.InsertChatMessageParams{
		ChatID:    otherChatID,
		ProjectID: uuid.NullUUID{UUID: td.projectID, Valid: true},
		Role:      "user",
		Content:   "same-project wrong-chat parent",
	})
	require.NoError(t, err)
	mismatchedParentPartID, err := queries.CreateChatContentPartForTest(t.Context(), riskrepo.CreateChatContentPartForTestParams{
		ChatID:              td.chatID,
		ProjectID:           uuid.NullUUID{UUID: td.projectID, Valid: true},
		Kind:                message.PromptAttachment,
		ContentAssetUrl:     "unused",
		ParentChatMessageID: uuid.NullUUID{UUID: otherChatParentID, Valid: true},
	})
	require.NoError(t, err)

	messageRows, err := queries.GetMessageContentBatch(t.Context(), riskrepo.GetMessageContentBatchParams{
		Ids:       []uuid.UUID{foreignChatMessageID},
		ProjectID: uuid.NullUUID{UUID: td.projectID, Valid: true},
	})
	require.NoError(t, err)
	require.Empty(t, messageRows)

	rows, err := queries.GetContentPartBatch(t.Context(), riskrepo.GetContentPartBatchParams{
		Ids:       []uuid.UUID{foreignChatPartID, mismatchedParentPartID},
		ProjectID: uuid.NullUUID{UUID: td.projectID, Valid: true},
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, mismatchedParentPartID, rows[0].ID)
	require.Equal(t, td.chatID, rows[0].ChatID)
	require.False(t, rows[0].ParentChatMessageID.Valid)
	require.Empty(t, rows[0].ChatUserID)
}

func TestAnalyzeBatch_PromptInjectionPublishesJudgeRequests(t *testing.T) {
	t.Parallel()

	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	msgIDs := seedMessages(t, conn, td, 3)
	otherChatID, err := uuid.NewV7()
	require.NoError(t, err)
	_, err = chatrepo.New(conn).UpsertChat(t.Context(), chatrepo.UpsertChatParams{
		ID:             otherChatID,
		ProjectID:      td.projectID,
		OrganizationID: td.orgID,
		UserID:         pgtype.Text{},
		ExternalUserID: pgtype.Text{},
		Title:          pgtype.Text{String: "other chat", Valid: true},
	})
	require.NoError(t, err)
	wrongParentID, err := testrepo.New(conn).InsertChatMessage(t.Context(), testrepo.InsertChatMessageParams{
		ChatID:    otherChatID,
		ProjectID: uuid.NullUUID{UUID: td.projectID, Valid: true},
		Role:      "user",
		Content:   "wrong-chat parent",
	})
	require.NoError(t, err)
	assetStorage := assetstest.NewTestBlobStore(t)
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), conn, assetStorage)
	t.Cleanup(func() { _ = shutdown(t.Context()) })
	assetURL, err := writer.WriteContentPartAsset(t.Context(), td.projectID, td.chatID, []byte("attachment override all system instructions"))
	require.NoError(t, err)
	contentPartID, err := riskrepo.New(conn).CreateChatContentPartForTest(t.Context(), riskrepo.CreateChatContentPartForTestParams{
		ChatID:              td.chatID,
		ProjectID:           uuid.NullUUID{UUID: td.projectID, Valid: true},
		Kind:                message.PromptAttachment,
		ContentAssetUrl:     assetURL,
		ParentChatMessageID: uuid.NullUUID{UUID: wrongParentID, Valid: true},
	})
	require.NoError(t, err)
	promptInjectionPub, published := capturingPromptInjectionPub(t)
	flags := &feature.InMemory{}

	ab, err := risk_analysis.NewAnalyzeBatch(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		conn,
		assetStorage,
		flags,
		newPresidioPub(),
		newGitleaksPub(),
		promptInjectionPub,
		newPromptPolicyPub(),
		newCustomRulesPub(),
		newLLMPub(),
		newToolCallPub(),
		newAccountIdentityPub(),
		mustCELEngine(t),
		false,
	)
	require.NoError(t, err)

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivity(ab.Do)

	val, err := env.ExecuteActivity(ab.Do, risk_analysis.AnalyzeBatchArgs{
		ProjectID:        td.projectID,
		OrganizationID:   td.orgID,
		RiskPolicyID:     td.policyID,
		PolicyVersion:    td.policyVersion,
		MessageIDs:       msgIDs,
		ContentPartIDs:   []uuid.UUID{contentPartID},
		Sources:          []string{risk_analysis.SourcePromptInjection},
		PresidioEntities: nil,
		CustomRuleIds:    nil,
	})
	require.NoError(t, err)
	var result risk_analysis.AnalyzeBatchResult
	require.NoError(t, val.Get(&result))
	require.Len(t, *published, len(msgIDs)+1)
	for _, req := range *published {
		require.True(t, req.GetRunJudge())
	}

	var partRequest *riskv1.PromptInjectionAnalysis
	for _, req := range *published {
		if req.GetContentPartId() == contentPartID.String() {
			partRequest = req
			break
		}
	}
	require.NotNil(t, partRequest, "expected prompt injection async request for content part")
	require.Empty(t, partRequest.GetChatMessageId())
	require.Equal(t, contentPartID.String(), partRequest.GetContentPartId())
	require.Equal(t, td.chatID.String(), partRequest.GetChatId())
	require.Empty(t, partRequest.GetParentChatMessageId())
	require.Equal(t, "content_part_unlinked", partRequest.GetMessageLinkReason())
	require.Equal(t, "judge_handler_stream", partRequest.GetExecutionPath())
	require.Equal(t, td.policyID.String(), partRequest.GetOriginRiskPolicyId())
	require.Equal(t, td.policyVersion, partRequest.GetOriginRiskPolicyVersion())
	require.NotEmpty(t, partRequest.GetRequestId())
}

func TestAnalyzeBatch_PromptInjectionPublishesStrictlyBoundedTrajectory(t *testing.T) {
	t.Parallel()

	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	queries := testrepo.New(conn)
	insert := func(role, content string) uuid.UUID {
		id, err := queries.InsertChatMessage(t.Context(), testrepo.InsertChatMessageParams{
			ChatID:    td.chatID,
			ProjectID: uuid.NullUUID{UUID: td.projectID, Valid: true},
			Role:      role,
			Content:   content,
		})
		require.NoError(t, err)
		return id
	}

	staleID := insert("tool", "stale tool result before the latest request")
	priorUserRequest := "latest request:" + strings.Repeat("u", 4100)
	recentUntrustedContent := "latest tool result:" + strings.Repeat("t", 4100)
	userID := insert("user", priorUserRequest)
	toolID := insert("tool", recentUntrustedContent)
	// The current event must be a write tool call: the recommended PI scope
	// exempts plain assistant text and all-read-only tool-call batches.
	currentID := insertAssistantToolCallWithArgs(t, conn, td, "Bash", map[string]any{"command": "echo current event"})

	// SQL defaults use the database clock; the writer uses the Go clock.
	// Pin conversation order rather than relying on clock agreement.
	base := time.Now().UTC().Add(-time.Hour)
	for i, id := range []uuid.UUID{staleID, userID, toolID, currentID} {
		err := queries.UpdateChatMessageCreatedAt(t.Context(), testrepo.UpdateChatMessageCreatedAtParams{
			CreatedAt: pgtype.Timestamptz{Time: base.Add(time.Duration(i) * time.Second), Valid: true},
			ID:        id,
		})
		require.NoError(t, err)
	}

	promptInjectionPub, published := capturingPromptInjectionPub(t)
	ab, err := risk_analysis.NewAnalyzeBatch(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		conn,
		nil,
		&feature.InMemory{},
		newPresidioPub(),
		newGitleaksPub(),
		promptInjectionPub,
		newPromptPolicyPub(),
		newCustomRulesPub(),
		newLLMPub(),
		newToolCallPub(),
		newAccountIdentityPub(),
		mustCELEngine(t),
		false,
	)
	require.NoError(t, err)

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivity(ab.Do)
	val, err := env.ExecuteActivity(ab.Do, risk_analysis.AnalyzeBatchArgs{
		ProjectID:        td.projectID,
		OrganizationID:   td.orgID,
		RiskPolicyID:     td.policyID,
		PolicyVersion:    td.policyVersion,
		MessageIDs:       []uuid.UUID{currentID},
		Sources:          []string{risk_analysis.SourcePromptInjection},
		PresidioEntities: nil,
		CustomRuleIds:    nil,
	})
	require.NoError(t, err)
	var result risk_analysis.AnalyzeBatchResult
	require.NoError(t, val.Get(&result))

	require.Len(t, *published, 1)
	require.Equal(t, string([]rune(priorUserRequest)[:4000]), (*published)[0].GetPriorUserRequest())
	require.Equal(t, string([]rune(recentUntrustedContent)[:4000]), (*published)[0].GetRecentUntrustedContent())
}

func TestAnalyzeBatch_PromptInjectionPublishesBoundedOversizedInputs(t *testing.T) {
	t.Parallel()

	const bound = 50 * 1024
	// Oversized inputs are spaced words, not one repeated character: the token
	// counter's BPE merge is quadratic on a single unbroken run, which turned
	// this test into ten minutes of tokenizing under -race.
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	userID, err := testrepo.New(conn).InsertChatMessage(t.Context(), testrepo.InsertChatMessageParams{
		ChatID:    td.chatID,
		ProjectID: uuid.NullUUID{UUID: td.projectID, Valid: true},
		Role:      "user",
		Content:   strings.Repeat("u ", 2*bound),
	})
	require.NoError(t, err)
	toolID := insertAssistantToolCallWithArgs(t, conn, td, "Bash", map[string]any{"command": strings.Repeat("c ", 2*bound)})
	// Several calls publish as structured tool calls; the last runs past the budget.
	multiID := insertAssistantToolCallsWithArgs(t, conn, td, []struct {
		name string
		args map[string]any
	}{
		{name: "Bash", args: map[string]any{"command": "ls"}},
		{name: "Write", args: map[string]any{"content": strings.Repeat("w ", 2*bound)}},
		{name: "Bash", args: map[string]any{"command": strings.Repeat("c ", 2*bound)}},
	})

	assetStorage := assetstest.NewTestBlobStore(t)
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), conn, assetStorage)
	t.Cleanup(func() { _ = shutdown(t.Context()) })
	assetURL, err := writer.WriteContentPartAsset(t.Context(), td.projectID, td.chatID, []byte(strings.Repeat("p ", 2*bound)))
	require.NoError(t, err)
	partID, err := riskrepo.New(conn).CreateChatContentPartForTest(t.Context(), riskrepo.CreateChatContentPartForTestParams{
		ChatID:              td.chatID,
		ProjectID:           uuid.NullUUID{UUID: td.projectID, Valid: true},
		Kind:                message.PromptAttachment,
		ContentAssetUrl:     assetURL,
		ParentChatMessageID: uuid.NullUUID{},
	})
	require.NoError(t, err)

	promptInjectionPub, published := capturingPromptInjectionPub(t)
	ab, err := risk_analysis.NewAnalyzeBatch(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		conn,
		assetStorage,
		&feature.InMemory{},
		newPresidioPub(),
		newGitleaksPub(),
		promptInjectionPub,
		newPromptPolicyPub(),
		newCustomRulesPub(),
		newLLMPub(),
		newToolCallPub(),
		newAccountIdentityPub(),
		mustCELEngine(t),
		false,
	)
	require.NoError(t, err)

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivity(ab.Do)
	val, err := env.ExecuteActivity(ab.Do, risk_analysis.AnalyzeBatchArgs{
		ProjectID:        td.projectID,
		OrganizationID:   td.orgID,
		RiskPolicyID:     td.policyID,
		PolicyVersion:    td.policyVersion,
		MessageIDs:       []uuid.UUID{userID, toolID, multiID},
		ContentPartIDs:   []uuid.UUID{partID},
		Sources:          []string{risk_analysis.SourcePromptInjection},
		PresidioEntities: nil,
		CustomRuleIds:    nil,
	})
	require.NoError(t, err)
	var result risk_analysis.AnalyzeBatchResult
	require.NoError(t, val.Get(&result))

	require.Len(t, *published, 4)
	sawToolCalls := false
	for _, req := range *published {
		require.True(t, req.GetRunJudge())
		require.LessOrEqual(t, len(req.GetContent()), bound)
		require.LessOrEqual(t, len(req.GetBody()), bound)
		argBytes := 0
		for _, call := range req.GetToolCalls() {
			argBytes += len(call.GetArguments())
		}
		require.LessOrEqual(t, argBytes, bound)
		if len(req.GetToolCalls()) > 0 {
			sawToolCalls = true
			require.Len(t, req.GetToolCalls(), 3)
			continue
		}
		require.NotEmpty(t, req.GetContent()+req.GetBody())
	}
	require.True(t, sawToolCalls, "expected a multi-call request with structured tool calls")
}

func TestAnalyzeBatch_PromptPolicyPublishesJudgeRequests(t *testing.T) {
	t.Parallel()

	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	policyID, err := uuid.NewV7()
	require.NoError(t, err)
	policy, err := riskrepo.New(conn).CreateRiskPolicy(t.Context(), riskrepo.CreateRiskPolicyParams{
		ID:             policyID,
		ProjectID:      td.projectID,
		OrganizationID: td.orgID,
		Name:           "prompt policy",
		PolicyType:     "prompt_based",
		Sources:        []string{},
		Enabled:        true,
		Action:         "flag",
		AudienceType:   "everyone",
		AutoName:       false,
		Prompt:         pgtype.Text{String: "Block unsafe requests", Valid: true},
	})
	require.NoError(t, err)
	msgIDs := seedMessages(t, conn, td, 2)
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagPromptPolicies, td.orgID, true)
	promptPolicyPub, published := capturingPromptPolicyPub(t)

	ab, err := risk_analysis.NewAnalyzeBatch(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		conn,
		nil,
		flags,
		newPresidioPub(),
		newGitleaksPub(),
		newPromptInjectionPub(),
		promptPolicyPub,
		newCustomRulesPub(),
		newLLMPub(),
		newToolCallPub(),
		newAccountIdentityPub(),
		mustCELEngine(t),
		false,
	)
	require.NoError(t, err)

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivity(ab.Do)

	val, err := env.ExecuteActivity(ab.Do, risk_analysis.AnalyzeBatchArgs{
		ProjectID:        td.projectID,
		OrganizationID:   td.orgID,
		RiskPolicyID:     policy.ID,
		PolicyVersion:    policy.Version,
		MessageIDs:       msgIDs,
		Sources:          nil,
		PresidioEntities: nil,
		CustomRuleIds:    nil,
	})
	require.NoError(t, err)
	var result risk_analysis.AnalyzeBatchResult
	require.NoError(t, val.Get(&result))
	require.Len(t, *published, len(msgIDs))
	for _, req := range *published {
		require.True(t, req.GetRunJudge())
		require.Equal(t, "judge_handler_stream", req.GetExecutionPath())
	}
}

func TestAnalyzeBatch_PromptPolicyPublishesPolicyWithoutPrompt(t *testing.T) {
	t.Parallel()

	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	policyID, err := uuid.NewV7()
	require.NoError(t, err)
	policy, err := riskrepo.New(conn).CreateRiskPolicy(t.Context(), riskrepo.CreateRiskPolicyParams{
		ID:             policyID,
		ProjectID:      td.projectID,
		OrganizationID: td.orgID,
		Name:           "prompt policy without prompt",
		PolicyType:     "prompt_based",
		Sources:        []string{},
		Enabled:        true,
		Action:         "flag",
		AudienceType:   "everyone",
		AutoName:       false,
		Prompt:         pgtype.Text{},
	})
	require.NoError(t, err)
	msgIDs := seedMessages(t, conn, td, 2)
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagPromptPolicies, td.orgID, true)
	promptPolicyPub, published := capturingPromptPolicyPub(t)

	ab, err := risk_analysis.NewAnalyzeBatch(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		conn,
		nil,
		flags,
		newPresidioPub(),
		newGitleaksPub(),
		newPromptInjectionPub(),
		promptPolicyPub,
		newCustomRulesPub(),
		newLLMPub(),
		newToolCallPub(),
		newAccountIdentityPub(),
		mustCELEngine(t),
		false,
	)
	require.NoError(t, err)

	runAnalyzeBatch(t, ab, risk_analysis.AnalyzeBatchArgs{
		ProjectID:        td.projectID,
		OrganizationID:   td.orgID,
		RiskPolicyID:     policy.ID,
		PolicyVersion:    policy.Version,
		MessageIDs:       msgIDs,
		Sources:          nil,
		PresidioEntities: nil,
		CustomRuleIds:    nil,
	})
	require.Len(t, *published, len(msgIDs), "the handler applies the fail mode to a policy without a prompt")
	for _, req := range *published {
		require.True(t, req.GetRunJudge())
		require.Empty(t, req.GetPrompt())
	}
}

func TestAnalyzeBatch_PromptPolicyPublishesToolCallPayload(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)

	policyID, err := uuid.NewV7()
	require.NoError(t, err)
	policy, err := riskrepo.New(conn).CreateRiskPolicy(t.Context(), riskrepo.CreateRiskPolicyParams{
		ID:             policyID,
		ProjectID:      td.projectID,
		OrganizationID: td.orgID,
		Name:           "prompt policy",
		PolicyType:     "prompt_based",
		Sources:        []string{},
		Enabled:        true,
		Action:         "flag",
		AudienceType:   "everyone",
		AutoName:       false,
		Prompt:         pgtype.Text{String: "Block destructive shell commands", Valid: true},
	})
	require.NoError(t, err)
	td.policyID = policy.ID
	td.policyVersion = policy.Version

	msgID := insertAssistantToolCallWithArgs(t, conn, td, "Bash", map[string]any{"command": "rm -rf /tmp/data"})
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagPromptPolicies, td.orgID, true)
	promptPolicyPub, published := capturingPromptPolicyPub(t)
	ab, err := risk_analysis.NewAnalyzeBatch(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		conn,
		nil,
		flags,
		newPresidioPub(),
		newGitleaksPub(),
		newPromptInjectionPub(),
		promptPolicyPub,
		newCustomRulesPub(),
		newLLMPub(),
		newToolCallPub(),
		newAccountIdentityPub(),
		mustCELEngine(t),
		false,
	)
	require.NoError(t, err)

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivity(ab.Do)

	val, err := env.ExecuteActivity(ab.Do, risk_analysis.AnalyzeBatchArgs{
		ProjectID:        td.projectID,
		OrganizationID:   td.orgID,
		RiskPolicyID:     td.policyID,
		PolicyVersion:    td.policyVersion,
		MessageIDs:       []uuid.UUID{msgID},
		Sources:          nil,
		PresidioEntities: nil,
		CustomRuleIds:    nil,
	})
	require.NoError(t, err)

	var result risk_analysis.AnalyzeBatchResult
	require.NoError(t, val.Get(&result))
	require.Equal(t, 1, result.Processed)
	require.Len(t, *published, 1)
	request := (*published)[0]
	require.True(t, request.GetRunJudge())
	require.Equal(t, message.ToolRequest, request.GetMessageType())
	require.Equal(t, "Bash", request.GetToolName())
	require.Contains(t, request.GetBody(), "rm -rf /tmp/data")
}
func TestAnalyzeBatch_CLIDestructivePublishesAnalysisRequest(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)

	msgID := insertAssistantToolCallWithArgs(t, conn, td, "Bash", map[string]any{"command": "rm -rf *"})

	pub, published := capturingToolCallPub(t)
	result := executeAnalyzeBatch(t, conn, td, []uuid.UUID{msgID}, []string{risk_analysis.SourceCLIDestructive}, pub)
	require.Equal(t, 1, result.Processed)

	require.Len(t, *published, 1)
	request := (*published)[0]
	require.Equal(t, []string{risk_analysis.SourceCLIDestructive}, request.GetSources())
	require.Equal(t, msgID.String(), request.GetChatMessageId())
	require.Len(t, request.GetToolCalls(), 1)
	require.Equal(t, "Bash", request.GetToolCalls()[0].GetName())
}

func TestAnalyzeBatch_DestructiveToolPublishesAnalysisRequest(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)

	destructive := true
	toolsetID := seedHTTPToolset(t, conn, td, "delete_records", &destructive)
	msgID := insertAssistantToolCall(t, conn, td, "mcp__gram__delete_records", toolsetID)

	pub, published := capturingToolCallPub(t)
	result := executeAnalyzeBatch(t, conn, td, []uuid.UUID{msgID}, []string{shadowmcp.SourceDestructiveTool}, pub)
	require.Equal(t, 1, result.Processed)

	require.Len(t, *published, 1)
	request := (*published)[0]
	require.Equal(t, []string{shadowmcp.SourceDestructiveTool}, request.GetSources())
	require.Equal(t, msgID.String(), request.GetChatMessageId())
}

func TestAnalyzeBatch_ShadowMCPPublishesAnalysisRequest(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)

	// An MCP-shaped tool call with no Speakeasy toolset marker and no recorded
	// provenance is shadow MCP by signature validation.
	msgID := insertAssistantToolCallWithArgs(t, conn, td, "mcp__rogue__exfiltrate", map[string]any{"target": "data"})

	pub, published := capturingToolCallPub(t)
	result := executeAnalyzeBatch(t, conn, td, []uuid.UUID{msgID}, []string{shadowmcp.SourceShadowMCP}, pub)
	require.Equal(t, 1, result.Processed)

	require.Len(t, *published, 1)
	request := (*published)[0]
	require.Equal(t, []string{shadowmcp.SourceShadowMCP}, request.GetSources())
	require.Equal(t, msgID.String(), request.GetChatMessageId())
}

// TestAnalyzeBatch_Gitleaks_SecretInToolCallArgsOnly plants a secret solely in
// tool-call arguments (content empty) and asserts the batch gitleaks scan
// composes the argument surface and reports it - the POC-314 scenario where
// args-only credentials never triggered risk policies.
// TestAnalyzeBatch_Presidio_PIIInToolCallArgsOnly mirrors the gitleaks case for
// Presidio: PII only in tool-call arguments must survive the composed scan
// surface end to end through the batch activity.
// TestAnalyzeBatch_CLIDestructive_MCPArgsDropTable proves the cli_destructive
// scanner is genuinely tool-name-agnostic: an MCP-routed tool whose
// `arguments` carry a destructive SQL fragment also flags. This is the
// "scan all tool calls" semantics the planner asked for.
// TestAnalyzeBatch_CLIDestructive_StableRuleIDAcrossKeys exercises the
// deterministic-iteration guarantee of flattenCLIStrings: a tool call whose
// arguments carry destructive content under multiple keys must report the
// same rule_id every run. Without sorted map iteration the rule_id flaps
// between matches because the first-match-wins inner loop sees keys in a
// random order.
// TestAnalyzeBatch_BothSources_OnSameMCPCall asserts that destructive_tool
// (annotation) and cli_destructive (content) emit two distinct findings on
// a single MCP tool call when both sources are enabled. Proves the dedup
// pass at the merge boundary lets non-overlapping findings through and
// neither rule_id collides.
// A configured exclusion must suppress a message-level content finding through
// the full Do() path. TestAnalyzeBatch_CustomDetectionRuleFinding is the control
// (identical setup, no exclusion -> 1 finding). The ExclusionSet predicate is
// unit-tested in isolation; the wiring the session-level work reshaped -
// policyExclusionSet's DB fetch and threading into scanStandardPolicy - is only
// exercised end-to-end here.
// A CEL detection rule targeting the tool server flags a tool-request message
// whose MCP server matches, exercising the DB → customRuleMessageView → engine
// path.
// insertAssistantToolCallWithArgs is a sibling of insertAssistantToolCall for
// CLI scenarios where the recorded arguments don't carry a Speakeasy toolset id -
// the cli_destructive scanner is content-driven, so the args field is the
// thing under test.
func insertAssistantToolCallWithArgs(t *testing.T, conn *pgxpool.Pool, td testData, callName string, argsMap map[string]any) uuid.UUID {
	t.Helper()

	args, err := json.Marshal(argsMap)
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
	_, err = writer.Write(t.Context(), td.projectID, []chat.MessageWrite{{Params: chatrepo.CreateChatMessageParams{
		CreatedAt:        pgtype.Timestamptz{},
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
	}, UserEmail: ""}})
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

// insertAssistantToolCallsWithArgs inserts an assistant message that issued
// multiple tool calls, mirroring insertAssistantToolCallWithArgs for the
// multi-call judge path. Each entry is (tool name, arguments map).
func insertAssistantToolCallsWithArgs(t *testing.T, conn *pgxpool.Pool, td testData, calls []struct {
	name string
	args map[string]any
}) uuid.UUID {
	t.Helper()

	recorded := make([]map[string]any, 0, len(calls))
	for i, c := range calls {
		args, err := json.Marshal(c.args)
		require.NoError(t, err)
		recorded = append(recorded, map[string]any{
			"id":   fmt.Sprintf("call_%d", i+1),
			"type": "function",
			"function": map[string]any{
				"name":      c.name,
				"arguments": string(args),
			},
		})
	}
	toolCalls, err := json.Marshal(recorded)
	require.NoError(t, err)

	messageID := "msg-" + uuid.NewString()
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), conn, nil)
	t.Cleanup(func() { _ = shutdown(t.Context()) })
	_, err = writer.Write(t.Context(), td.projectID, []chat.MessageWrite{{Params: chatrepo.CreateChatMessageParams{
		CreatedAt:        pgtype.Timestamptz{},
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
	}, UserEmail: ""}})
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
	require.FailNow(t, "inserted multi-tool-call message not found")
	return uuid.Nil
}

func executeAnalyzeBatch(t *testing.T, conn *pgxpool.Pool, td testData, messageIDs []uuid.UUID, sources []string, toolCallPub gcp.Publisher[*riskv1.ToolCallAnalysis]) risk_analysis.AnalyzeBatchResult {
	t.Helper()
	return executeAnalyzeBatchForIDs(t, conn, nil, td, messageIDs, nil, sources, toolCallPub, nil)
}

func TestAnalyzeBatch_ContentPartHydratesFullAssetContent(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	assetStorage := assetstest.NewTestBlobStore(t)
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), conn, assetStorage)
	t.Cleanup(func() { _ = shutdown(t.Context()) })

	fullContent := strings.Repeat("safe prefix\n", 500) + "AccessKeyId ASIAZ2XY3WNBQR5TUVWX SecretAccessKey wJalrXUtnFEMIbKp7MDoRZfiCYqTvHgNsQ8xLcWd\n"
	assetURL, err := writer.WriteContentPartAsset(t.Context(), td.projectID, td.chatID, []byte(fullContent))
	require.NoError(t, err)
	partID, err := riskrepo.New(conn).CreateChatContentPartForTest(t.Context(), riskrepo.CreateChatContentPartForTestParams{
		ChatID:              td.chatID,
		ProjectID:           uuid.NullUUID{UUID: td.projectID, Valid: true},
		Kind:                message.PromptAttachment,
		ContentAssetUrl:     assetURL,
		ParentChatMessageID: uuid.NullUUID{},
	})
	require.NoError(t, err)

	gitleaksPub, published := capturingPub[*riskv1.GitleaksAnalysis](t)
	result := runAnalyzeBatch(t, gitleaksAnalyzer(t, conn, assetStorage, gitleaksPub), risk_analysis.AnalyzeBatchArgs{
		ProjectID:        td.projectID,
		OrganizationID:   td.orgID,
		RiskPolicyID:     td.policyID,
		PolicyVersion:    td.policyVersion,
		ContentPartIDs:   []uuid.UUID{partID},
		Sources:          []string{risk_analysis.SourceGitleaks},
		PresidioEntities: nil,
		CustomRuleIds:    nil,
	})
	require.Equal(t, 1, result.Processed)
	require.Len(t, *published, 1)
	require.Empty(t, (*published)[0].GetChatMessageId())
	require.Equal(t, partID.String(), (*published)[0].GetContentPartId())
	require.Equal(t, fullContent, (*published)[0].GetContent())
}

func executeAnalyzeBatchForIDs(t *testing.T, conn *pgxpool.Pool, assetStorage assets.BlobStore, td testData, messageIDs []uuid.UUID, contentPartIDs []uuid.UUID, sources []string, toolCallPub gcp.Publisher[*riskv1.ToolCallAnalysis], flags feature.Provider) risk_analysis.AnalyzeBatchResult {
	t.Helper()
	ab, err := risk_analysis.NewAnalyzeBatch(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		conn,
		assetStorage,
		flags,
		newPresidioPub(),
		newGitleaksPub(),
		newPromptInjectionPub(),
		newPromptPolicyPub(),
		newCustomRulesPub(),
		newLLMPub(),
		toolCallPub,
		newAccountIdentityPub(),
		mustCELEngine(t),
		false,
	)
	require.NoError(t, err)

	return runAnalyzeBatch(t, ab, risk_analysis.AnalyzeBatchArgs{
		ProjectID:        td.projectID,
		OrganizationID:   td.orgID,
		RiskPolicyID:     td.policyID,
		PolicyVersion:    td.policyVersion,
		MessageIDs:       messageIDs,
		ContentPartIDs:   contentPartIDs,
		Sources:          sources,
		PresidioEntities: nil,
		CustomRuleIds:    nil,
	})
}

func seedCustomRulePolicySelection(t *testing.T, conn *pgxpool.Pool, td testData, ruleID string, detectionExpr string) testData {
	t.Helper()

	detectionValue := pgtype.Text{}
	if detectionExpr != "" {
		detectionValue = pgtype.Text{String: detectionExpr, Valid: true}
	}
	_, err := riskrepo.New(conn).CreateCustomDetectionRule(t.Context(), riskrepo.CreateCustomDetectionRuleParams{
		ProjectID:      td.projectID,
		OrganizationID: td.orgID,
		RuleID:         ruleID,
		Title:          "ACME token",
		Description:    "ACME token",
		DetectionExpr:  detectionValue,
		Severity:       "high",
	})
	require.NoError(t, err)

	policy, err := riskrepo.New(conn).GetRiskPolicy(t.Context(), riskrepo.GetRiskPolicyParams{
		ID:        td.policyID,
		ProjectID: td.projectID,
	})
	require.NoError(t, err)

	updated, err := riskrepo.New(conn).UpdateRiskPolicy(t.Context(), riskrepo.UpdateRiskPolicyParams{
		ID:                   policy.ID,
		ProjectID:            policy.ProjectID,
		Name:                 policy.Name,
		Sources:              []string{},
		PresidioEntities:     policy.PresidioEntities,
		PromptInjectionRules: policy.PromptInjectionRules,
		DisabledRules:        policy.DisabledRules,
		CustomRuleIds:        []string{ruleID},
		Enabled:              policy.Enabled,
		Action:               "flag",
		AudienceType:         policy.AudienceType,
		AutoName:             policy.AutoName,
		UserMessage:          policy.UserMessage,
	})
	require.NoError(t, err)
	td.policyVersion = updated.Version
	return td
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
	_, err = writer.Write(t.Context(), td.projectID, []chat.MessageWrite{{Params: chatrepo.CreateChatMessageParams{
		CreatedAt:        pgtype.Timestamptz{},
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
	}, UserEmail: ""}})
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
		Content:   "token ghp_R2D2C3POLuk3Skywalker1234567890ab",
	})
	require.NoError(t, err)

	result := executeAnalyzeBatch(t, conn, td, []uuid.UUID{msgID}, []string{"gitleaks"}, newToolCallPub())
	assert.Equal(t, 0, result.Processed)
}

func TestAnalyzeBatch_SkipsWhenPolicyDeleted(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)

	msgID, err := testrepo.New(conn).InsertChatMessage(t.Context(), testrepo.InsertChatMessageParams{
		ChatID:    td.chatID,
		ProjectID: uuid.NullUUID{UUID: td.projectID, Valid: true},
		Role:      "user",
		Content:   "token ghp_R2D2C3POLuk3Skywalker1234567890ab",
	})
	require.NoError(t, err)

	require.NoError(t, riskrepo.New(conn).DeleteRiskPolicy(t.Context(), riskrepo.DeleteRiskPolicyParams{
		ID:        td.policyID,
		ProjectID: td.projectID,
	}))

	result := executeAnalyzeBatch(t, conn, td, []uuid.UUID{msgID}, []string{"gitleaks"}, newToolCallPub())
	assert.Equal(t, 0, result.Processed)
}

// gitleaksAnalyzer builds an analyzer that captures gitleaks requests through
// gitleaksPub and accepts every other publish.
func gitleaksAnalyzer(t *testing.T, conn *pgxpool.Pool, assetStorage assets.BlobStore, gitleaksPub gcp.Publisher[*riskv1.GitleaksAnalysis]) *risk_analysis.AnalyzeBatch {
	t.Helper()
	ab, err := risk_analysis.NewAnalyzeBatch(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		conn,
		assetStorage,
		&feature.InMemory{},
		newPresidioPub(),
		gitleaksPub,
		newPromptInjectionPub(),
		newPromptPolicyPub(),
		newCustomRulesPub(),
		newLLMPub(),
		newToolCallPub(),
		newAccountIdentityPub(),
		mustCELEngine(t),
		false,
	)
	require.NoError(t, err)
	return ab
}

func runAnalyzeBatch(t *testing.T, ab *risk_analysis.AnalyzeBatch, args risk_analysis.AnalyzeBatchArgs) risk_analysis.AnalyzeBatchResult {
	t.Helper()
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivity(ab.Do)
	val, err := env.ExecuteActivity(ab.Do, args)
	require.NoError(t, err)
	var result risk_analysis.AnalyzeBatchResult
	require.NoError(t, val.Get(&result))
	return result
}

func TestAnalyzeBatch_OnlyPublishes(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	msgID, err := testrepo.New(conn).InsertChatMessage(t.Context(), testrepo.InsertChatMessageParams{
		ChatID:    td.chatID,
		ProjectID: uuid.NullUUID{UUID: td.projectID, Valid: true},
		Role:      "user",
		Content:   "AccessKeyId ASIAZ2XY3WNBQR5TUVWX SecretAccessKey wJalrXUtnFEMIbKp7MDoRZfiCYqTvHgNsQ8xLcWd",
	})
	require.NoError(t, err)

	gitleaksPub, published := capturingPub[*riskv1.GitleaksAnalysis](t)
	ab := gitleaksAnalyzer(t, conn, nil, gitleaksPub)
	result := runAnalyzeBatch(t, ab, risk_analysis.AnalyzeBatchArgs{
		ProjectID:        td.projectID,
		OrganizationID:   td.orgID,
		RiskPolicyID:     td.policyID,
		PolicyVersion:    td.policyVersion,
		MessageIDs:       []uuid.UUID{msgID},
		Sources:          []string{risk_analysis.SourceGitleaks, risk_analysis.SourcePromptInjection, risk_analysis.SourceCLIDestructive, risk_analysis.SourceAccountIdentity},
		PresidioEntities: nil,
		CustomRuleIds:    nil,
	})
	require.Equal(t, 1, result.Processed)
	require.Len(t, *published, 1)
	require.Equal(t, msgID.String(), (*published)[0].GetChatMessageId())

	rows, err := testrepo.New(conn).ListRiskResultsAll(t.Context(), testrepo.ListRiskResultsAllParams{
		ProjectID:    td.projectID,
		RiskPolicyID: td.policyID,
	})
	require.NoError(t, err)
	require.Empty(t, rows, "the activity writes no Postgres results")

	count, err := testrepo.New(conn).CountOutboxEntriesByEventType(t.Context(), string(events.RiskFindingCreatedV1.EventType()))
	require.NoError(t, err)
	require.Zero(t, count, "the ClickHouse writer owns finding webhooks")
}

func TestAnalyzeBatch_RequestIDsIgnoreBatchComposition(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	msgIDs := seedMessages(t, conn, td, 2)

	gitleaksPub, published := capturingPub[*riskv1.GitleaksAnalysis](t)
	ab := gitleaksAnalyzer(t, conn, nil, gitleaksPub)
	args := risk_analysis.AnalyzeBatchArgs{
		ProjectID:        td.projectID,
		OrganizationID:   td.orgID,
		RiskPolicyID:     td.policyID,
		PolicyVersion:    td.policyVersion,
		MessageIDs:       msgIDs[:1],
		Sources:          []string{risk_analysis.SourceGitleaks},
		PresidioEntities: nil,
		CustomRuleIds:    nil,
	}
	runAnalyzeBatch(t, ab, args)
	args.MessageIDs = msgIDs
	runAnalyzeBatch(t, ab, args)

	requestIDs := map[string][]string{}
	for _, req := range *published {
		requestIDs[req.GetChatMessageId()] = append(requestIDs[req.GetChatMessageId()], req.GetRequestId())
	}
	require.Len(t, requestIDs[msgIDs[0].String()], 2)
	require.Equal(t, requestIDs[msgIDs[0].String()][0], requestIDs[msgIDs[0].String()][1], "a rebatched anchor keeps its request id, so handler finding ids converge")
	require.NotEqual(t, requestIDs[msgIDs[0].String()][0], requestIDs[msgIDs[1].String()][0])
}

func TestAnalyzeBatch_PublishFailureFailsActivity(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	msgIDs := seedMessages(t, conn, td, 1)

	gitleaksPub := gcp.NewMockPublisher[*riskv1.GitleaksAnalysis]()
	gitleaksPub.On("Publish", mock.Anything, mock.Anything).Return(gcp.NewErrPublishResult(errors.New("topic unavailable")))
	ab := gitleaksAnalyzer(t, conn, nil, gitleaksPub)

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivity(ab.Do)
	_, err := env.ExecuteActivity(ab.Do, risk_analysis.AnalyzeBatchArgs{
		ProjectID:        td.projectID,
		OrganizationID:   td.orgID,
		RiskPolicyID:     td.policyID,
		PolicyVersion:    td.policyVersion,
		MessageIDs:       msgIDs,
		Sources:          []string{risk_analysis.SourceGitleaks},
		PresidioEntities: nil,
		CustomRuleIds:    nil,
	})
	require.ErrorContains(t, err, "topic unavailable", "an unacknowledged publish must fail the batch so it is redriven, not marked analyzed")
}
