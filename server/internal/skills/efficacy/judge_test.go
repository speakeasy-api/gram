package efficacy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// mockCompletionClient implements openrouter.CompletionClient for tests.
type mockCompletionClient struct {
	mock.Mock
}

func (m *mockCompletionClient) GetCompletion(ctx context.Context, request openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	args := m.Called(ctx, request)
	resp, _ := args.Get(0).(*openrouter.CompletionResponse)
	return resp, args.Error(1)
}

func (m *mockCompletionClient) GetCompletionStream(ctx context.Context, request openrouter.CompletionRequest) (openrouter.StreamReader, error) {
	args := m.Called(ctx, request)
	r, _ := args.Get(0).(openrouter.StreamReader)
	return r, args.Error(1)
}

func (m *mockCompletionClient) GetObjectCompletion(ctx context.Context, request openrouter.ObjectCompletionRequest) (*openrouter.CompletionResponse, error) {
	args := m.Called(ctx, request)
	resp, _ := args.Get(0).(*openrouter.CompletionResponse)
	return resp, args.Error(1)
}

func (m *mockCompletionClient) CreateEmbeddings(ctx context.Context, orgID string, model string, inputs []string, opts ...openrouter.EmbeddingOption) ([][]float32, error) {
	var resolved openrouter.EmbeddingOptions
	for _, opt := range opts {
		opt(&resolved)
	}
	args := m.Called(ctx, orgID, model, inputs, resolved)
	v, _ := args.Get(0).([][]float32)
	return v, args.Error(1)
}

func judgeResponse(text string) *openrouter.CompletionResponse {
	content := or.CreateChatAssistantMessageContentStr(text)
	msg := or.CreateChatMessagesAssistant(or.ChatAssistantMessage{
		Role:             or.ChatAssistantMessageRoleAssistant,
		Content:          optionalnullable.From(&content),
		Name:             nil,
		ToolCalls:        nil,
		Refusal:          nil,
		Reasoning:        nil,
		ReasoningDetails: nil,
		Images:           nil,
		Audio:            nil,
	})
	cost := 0.0004
	return &openrouter.CompletionResponse{
		StartTime:    time.Time{},
		Message:      &msg,
		MessageID:    "msg_test",
		Model:        JudgeModel,
		Usage:        openrouter.Usage{PromptTokens: 120, CompletionTokens: 30, TotalTokens: 150, Cost: &cost},
		FinishReason: nil,
		ToolCalls:    nil,
		Content:      text,
	}
}

// newTestJudge builds a Judge for the call path. The limiter is exercised only
// by Judge, which needs a Redis-backed store; call() never touches it.
func newTestJudge(t *testing.T, client openrouter.CompletionClient) *Judge {
	t.Helper()
	return &Judge{
		logger:  testenv.NewLogger(t),
		tracer:  testenv.NewTracerProvider(t).Tracer("test"),
		client:  client,
		limiter: nil,
	}
}

func testJudgeInput() JudgeInput {
	return JudgeInput{
		OrgID:        "org_1",
		ProjectID:    "00000000-0000-0000-0000-000000000001",
		SkillName:    "commit-hygiene",
		SkillURN:     "skills:project:commit-hygiene",
		SkillContent: "Always run the linter before committing.",
		Surface:      "dev",
		ActivatedAt:  time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC),
		Transcript: Transcript{
			Omitted: "",
			Messages: []TranscriptMessage{{
				Index:                     1,
				CreatedAt:                 "2026-07-21T12:00:00Z",
				SecondsSincePrevious:      nil,
				Role:                      "user",
				Content:                   "commit this",
				ContentTruncated:          false,
				ToolCalls:                 nil,
				ToolCallsTruncated:        false,
				ToolCallID:                "",
				ToolURN:                   "",
				ToolOutcome:               "",
				ToolOutcomeNotes:          "",
				ToolOutcomeNotesTruncated: false,
			}},
		},
	}
}

func TestJudgeCallReturnsNormalizedVerdict(t *testing.T) {
	t.Parallel()

	client := &mockCompletionClient{}
	client.On("GetObjectCompletion", mock.Anything, mock.Anything).
		Return(judgeResponse(`{"score":1.4,"rationale":"linter ran first","est_turns_saved":1,"est_minutes_saved":null,"roi_confidence":"low","flags":[]}`), nil).Once()

	got, err := newTestJudge(t, client).call(t.Context(), testJudgeInput())

	require.NoError(t, err)
	require.InDelta(t, 1.0, got.Verdict.Score, 0)
	require.Equal(t, "linter ran first", got.Verdict.Rationale)
	require.Equal(t, JudgeModel, got.Model)
	require.Equal(t, JudgePromptVersion, got.PromptVersion)
	client.AssertExpectations(t)
}

func TestJudgeCallBillsInternalKeyAndEfficacySource(t *testing.T) {
	t.Parallel()

	client := &mockCompletionClient{}
	var captured openrouter.ObjectCompletionRequest
	client.On("GetObjectCompletion", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			captured, _ = args.Get(1).(openrouter.ObjectCompletionRequest)
		}).
		Return(judgeResponse(`{"score":0.5,"rationale":"ok","est_turns_saved":null,"est_minutes_saved":null,"roi_confidence":null,"flags":[]}`), nil).Once()

	_, err := newTestJudge(t, client).call(t.Context(), testJudgeInput())

	require.NoError(t, err)
	require.Equal(t, openrouter.KeyTypeInternal, captured.KeyType)
	require.Equal(t, "skill-efficacy", string(captured.UsageSource))
	require.Equal(t, "skill-efficacy", string(captured.KeySlot))
	require.Equal(t, SystemPrompt, captured.SystemPrompt)
	require.NotNil(t, captured.JSONSchema)
}

func TestJudgeCallTreatsTransportErrorAsRetryable(t *testing.T) {
	t.Parallel()

	client := &mockCompletionClient{}
	client.On("GetObjectCompletion", mock.Anything, mock.Anything).
		Return((*openrouter.CompletionResponse)(nil), errors.New("connection reset")).Once()

	_, err := newTestJudge(t, client).call(t.Context(), testJudgeInput())

	require.ErrorIs(t, err, ErrRetryable)
	require.NotErrorIs(t, err, ErrModelFailure)
}

// TestJudgeCallTreatsRejectedRequestAsModelFailure covers the payload the judge
// controls being refused: retrying re-sends the identical request, so the
// attempt has to be charged instead of looping forever.
func TestJudgeCallTreatsRejectedRequestAsModelFailure(t *testing.T) {
	t.Parallel()

	client := &mockCompletionClient{}
	client.On("GetObjectCompletion", mock.Anything, mock.Anything).
		Return((*openrouter.CompletionResponse)(nil), fmt.Errorf("OpenRouter API error (status 400): prompt is too long: %w", openrouter.ErrHistoryCorruptionCandidate)).Once()

	_, err := newTestJudge(t, client).call(t.Context(), testJudgeInput())

	require.ErrorIs(t, err, ErrModelFailure)
	require.NotErrorIs(t, err, ErrRetryable)
}

// TestJudgeCallTreatsGenericBadRequestAsModelFailure covers a 400/422 that is
// not transcript-shaped - an unusable model or parameter. It is just as
// deterministic as a corrupt transcript, so it burns an attempt rather than
// looping the same request until the pipeline gives up.
func TestJudgeCallTreatsGenericBadRequestAsModelFailure(t *testing.T) {
	t.Parallel()

	client := &mockCompletionClient{}
	client.On("GetObjectCompletion", mock.Anything, mock.Anything).
		Return((*openrouter.CompletionResponse)(nil), fmt.Errorf("OpenRouter API error (status 400): model does not support structured outputs: %w", openrouter.ErrBadRequest)).Once()

	_, err := newTestJudge(t, client).call(t.Context(), testJudgeInput())

	require.ErrorIs(t, err, ErrModelFailure)
	require.NotErrorIs(t, err, ErrRetryable)
}

// TestJudgeCallTreatsContentPolicyRejectionAsModelFailure covers a provider
// refusing the transcript on content grounds. The verdict is a property of the
// payload, so retrying it forever would pin the evaluation open.
func TestJudgeCallTreatsContentPolicyRejectionAsModelFailure(t *testing.T) {
	t.Parallel()

	client := &mockCompletionClient{}
	client.On("GetObjectCompletion", mock.Anything, mock.Anything).
		Return((*openrouter.CompletionResponse)(nil), fmt.Errorf("OpenRouter API error (status 403), response body omitted: %w", openrouter.ErrContentPolicy)).Once()

	_, err := newTestJudge(t, client).call(t.Context(), testJudgeInput())

	require.ErrorIs(t, err, ErrModelFailure)
	require.NotErrorIs(t, err, ErrRetryable)
}

// TestJudgeCallTreatsUnclassified403AsRetryable pins the other side of the 403
// split: a key or entitlement problem clears without changing the payload, so
// it must not burn the evaluation's attempt budget.
func TestJudgeCallTreatsUnclassified403AsRetryable(t *testing.T) {
	t.Parallel()

	client := &mockCompletionClient{}
	client.On("GetObjectCompletion", mock.Anything, mock.Anything).
		Return((*openrouter.CompletionResponse)(nil), errors.New("OpenRouter API error (status 403), response body omitted")).Once()

	_, err := newTestJudge(t, client).call(t.Context(), testJudgeInput())

	require.ErrorIs(t, err, ErrRetryable)
	require.NotErrorIs(t, err, ErrModelFailure)
}

// TestJudgeCallTreatsInsufficientCreditsAsRetryable pins the other side of the
// split: a funding failure is infrastructure and must not burn attempts.
func TestJudgeCallTreatsInsufficientCreditsAsRetryable(t *testing.T) {
	t.Parallel()

	client := &mockCompletionClient{}
	client.On("GetObjectCompletion", mock.Anything, mock.Anything).
		Return((*openrouter.CompletionResponse)(nil), fmt.Errorf("OpenRouter API error (status 402): %w", openrouter.ErrInsufficientCredits)).Once()

	_, err := newTestJudge(t, client).call(t.Context(), testJudgeInput())

	require.ErrorIs(t, err, ErrRetryable)
	require.NotErrorIs(t, err, ErrModelFailure)
}

func TestJudgeCallTreatsCallTimeoutAsModelFailure(t *testing.T) {
	t.Parallel()

	client := &mockCompletionClient{}
	client.On("GetObjectCompletion", mock.Anything, mock.Anything).
		Return((*openrouter.CompletionResponse)(nil), context.DeadlineExceeded).Once()

	_, err := newTestJudge(t, client).call(t.Context(), testJudgeInput())

	require.ErrorIs(t, err, ErrModelFailure)
	require.NotErrorIs(t, err, ErrRetryable)
}

func TestJudgeCallTreatsUnparseableOutputAsModelFailure(t *testing.T) {
	t.Parallel()

	client := &mockCompletionClient{}
	client.On("GetObjectCompletion", mock.Anything, mock.Anything).
		Return(judgeResponse("sorry, I cannot help with that"), nil).Once()

	_, err := newTestJudge(t, client).call(t.Context(), testJudgeInput())

	require.ErrorIs(t, err, ErrModelFailure)
}

func TestJudgeCallTreatsEmptyContentAsModelFailure(t *testing.T) {
	t.Parallel()

	client := &mockCompletionClient{}
	client.On("GetObjectCompletion", mock.Anything, mock.Anything).
		Return(judgeResponse("   "), nil).Once()

	_, err := newTestJudge(t, client).call(t.Context(), testJudgeInput())

	require.ErrorIs(t, err, ErrModelFailure)
}

func TestBuildJudgePromptCarriesSkillAndTranscript(t *testing.T) {
	t.Parallel()

	in := testJudgeInput()
	in.Transcript.Omitted = "[2 earlier messages omitted]"

	prompt, err := BuildJudgePrompt(in)
	require.NoError(t, err)

	var payload judgePromptPayload
	require.NoError(t, json.Unmarshal([]byte(prompt), &payload))
	require.Equal(t, in.SkillName, payload.SkillName)
	require.Equal(t, in.SkillContent, payload.SkillContent)
	require.Equal(t, in.Surface, payload.Surface)
	require.Equal(t, in.Transcript, payload.Transcript)
}

// TestVerdictSchemaHasNoUnsupportedBounds guards the constraint that makes
// Anthropic routes 400: minimum/maximum/maxLength anywhere in the schema.
func TestVerdictSchemaHasNoUnsupportedBounds(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(VerdictSchema())
	require.NoError(t, err)

	var decoded any
	require.NoError(t, json.Unmarshal(encoded, &decoded))

	var walk func(node any)
	walk = func(node any) {
		switch v := node.(type) {
		case map[string]any:
			for key, child := range v {
				require.NotContains(t, []string{"minimum", "maximum", "maxLength"}, key, "schema key %q is rejected by Anthropic routes", key)
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(decoded)
}

func TestVerdictSchemaIsStrictOverEveryVerdictField(t *testing.T) {
	t.Parallel()

	schema := VerdictSchema()

	require.Equal(t, false, schema["additionalProperties"])

	properties, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	required, ok := schema["required"].([]string)
	require.True(t, ok)
	require.Len(t, required, len(properties))
	for _, name := range required {
		require.Contains(t, properties, name)
	}

	// The schema is the contract Verdict unmarshals from, so its properties must
	// be exactly the verdict's JSON fields.
	encoded, err := json.Marshal(Verdict{
		Score:           0,
		Rationale:       "",
		EstTurnsSaved:   nil,
		EstMinutesSaved: nil,
		ROIConfidence:   nil,
		Flags:           nil,
	})
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(encoded, &fields))
	for name := range fields {
		require.Contains(t, properties, name)
	}
	require.Len(t, properties, len(fields))
}
