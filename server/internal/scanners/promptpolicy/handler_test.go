package promptpolicy_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func capturingPub(t *testing.T) (*gcp.MockPublisher[*riskv1.Finding], *[]*riskv1.Finding) {
	t.Helper()
	pub := gcp.NewMockPublisher[*riskv1.Finding]()
	var published []*riskv1.Finding
	pub.On("Publish", mock.Anything, mock.Anything).
		Return(gcp.NewSuccessPublishResult()).
		Run(func(args mock.Arguments) {
			f, ok := args.Get(1).(*riskv1.Finding)
			require.True(t, ok)
			published = append(published, f)
		})
	return pub, &published
}

func capturingMeterPub(t *testing.T) (*gcp.MockPublisher[*meteringv1.MeterReading], *[]*meteringv1.MeterReading) {
	t.Helper()
	pub := gcp.NewMockPublisher[*meteringv1.MeterReading]()
	var published []*meteringv1.MeterReading
	pub.On("Publish", mock.Anything, mock.Anything).
		Return(gcp.NewSuccessPublishResult()).
		Run(func(args mock.Arguments) {
			reading, ok := args.Get(1).(*meteringv1.MeterReading)
			require.True(t, ok)
			published = append(published, reading)
		})
	return pub, &published
}

func newRequest(content string) *riskv1.PromptPolicyAnalysis {
	// Keeps a legacy `model` key: the judge model is fixed, so the key must be
	// ignored without disturbing fail_open.
	modelConfig := []byte(`{"model":"openai/gpt-4.1-mini","fail_open":false}`)
	return riskv1.PromptPolicyAnalysis_builder{
		RequestId:               new("req-1"),
		ChatMessageId:           new("018ffad2-1c32-7f73-8a54-85306c37a314"),
		ProjectId:               new("018ffad2-1c32-7f73-8a54-85306c37a313"),
		OrganizationId:          new("org-1"),
		RiskPolicyId:            new("018ffad2-1c32-7f73-8a54-85306c37a315"),
		RiskPolicyVersion:       new(int64(3)),
		CreatedAt:               new("2026-06-20T00:00:00Z"),
		Content:                 &content,
		UserId:                  new("user-1"),
		Prompt:                  new("block unsafe requests"),
		ModelConfig:             modelConfig,
		MessageType:             new("user_message"),
		Body:                    &content,
		ToolName:                new(""),
		OriginRiskPolicyId:      new("018ffad2-1c32-7f73-8a54-85306c37a315"),
		OriginRiskPolicyVersion: new(int64(3)),
		ExecutionPath:           new("async"),
	}.Build()
}

func TestHandle_PublishesPromptPolicyFinding(t *testing.T) {
	t.Parallel()

	pub, published := capturingPub(t)
	evaluator := func(_ context.Context, in promptpolicy.Input) (*promptpolicy.Verdict, error) {
		require.Equal(t, "org-1", in.OrgID)
		require.Equal(t, "018ffad2-1c32-7f73-8a54-85306c37a313", in.ProjectID)
		require.Equal(t, "user-1", in.UserID)
		require.Equal(t, "block unsafe requests", in.Prompt)
		require.Equal(t, "delete production", in.Message.Body)
		require.False(t, in.Config.FailOpen)
		return &promptpolicy.Verdict{
			Matched:          true,
			Confidence:       0.88,
			Rationale:        "Message matched the prompt-based policy.",
			CostUSD:          0,
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
			STokens:          1,
			Completed:        true,
			Model:            "test",
			Provider:         "test",
		}, nil
	}
	realScanner := promptpolicy.NewScanner(testenv.NewLogger(t), evaluator)
	h := promptpolicy.NewHandler(testenv.NewLogger(t), testenv.NewMeterProvider(t), realScanner, pub, metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()))

	require.NoError(t, h.Handle(t.Context(), newRequest("delete production"), gcp.MessageMetadata{}))

	require.Len(t, *published, 1)
	f := (*published)[0]
	require.Equal(t, promptpolicy.Source, f.GetSource())
	require.Equal(t, promptpolicy.Rule, f.GetRuleId())
	require.Equal(t, "req-1", f.GetRequestId())
	require.Equal(t, "018ffad2-1c32-7f73-8a54-85306c37a314", f.GetChatMessageId())
	require.Equal(t, int64(3), f.GetRiskPolicyVersion())
	require.NotEmpty(t, f.GetId())
	require.InDelta(t, 0.88, f.GetConfidence(), 0.0001)
	require.Equal(t, judgemessage.Render(judgemessage.New("user_message", "", "delete production")), f.GetMatch())
}

func TestHandle_ToolRequestPublishesRenderedEventMatch(t *testing.T) {
	t.Parallel()

	pub, published := capturingPub(t)
	evaluator := func(_ context.Context, _ promptpolicy.Input) (*promptpolicy.Verdict, error) {
		return &promptpolicy.Verdict{Matched: true, Confidence: 0.9, Rationale: "matched", CostUSD: 0, PromptTokens: 0, CompletionTokens: 0, TotalTokens: 0, STokens: 1, Completed: true, Model: "test", Provider: "test"}, nil
	}
	realScanner := promptpolicy.NewScanner(testenv.NewLogger(t), evaluator)
	h := promptpolicy.NewHandler(testenv.NewLogger(t), testenv.NewMeterProvider(t), realScanner, pub, metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()))

	req := newRequest("")
	req.SetMessageType("tool_request")
	req.SetToolCalls([]*riskv1.PromptPolicyAnalysis_ToolCall{
		riskv1.PromptPolicyAnalysis_ToolCall_builder{Name: new("db:drop"), Arguments: new(`{"table":"users"}`)}.Build(),
	})
	require.NoError(t, h.Handle(t.Context(), req, gcp.MessageMetadata{}))

	want := judgemessage.Render(judgemessage.NewForToolCalls([]judgemessage.ToolCall{
		judgemessage.NewToolCall("db:drop", `{"table":"users"}`),
	}))
	require.Len(t, *published, 1)
	f := (*published)[0]
	require.Equal(t, want, f.GetMatch())
	require.Equal(t, int32(len(want)), f.GetEndPos())
}

func TestHandle_CleanPromptPolicyContentPublishesNothing(t *testing.T) {
	t.Parallel()

	pub, published := capturingPub(t)
	evaluator := func(context.Context, promptpolicy.Input) (*promptpolicy.Verdict, error) {
		return &promptpolicy.Verdict{
			Matched:          false,
			Confidence:       0,
			Rationale:        "",
			CostUSD:          0,
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
			STokens:          1,
			Completed:        true,
			Model:            "test",
			Provider:         "test",
		}, nil
	}
	realScanner := promptpolicy.NewScanner(testenv.NewLogger(t), evaluator)
	h := promptpolicy.NewHandler(testenv.NewLogger(t), testenv.NewMeterProvider(t), realScanner, pub, metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()))

	require.NoError(t, h.Handle(t.Context(), newRequest("hello world"), gcp.MessageMetadata{}))
	require.Empty(t, *published)
}

func TestHandle_LegacyPolicyFieldsPublishFindingAndUsage(t *testing.T) {
	t.Parallel()

	pub, findings := capturingPub(t)
	meterPub, readings := capturingMeterPub(t)
	evaluator := func(context.Context, promptpolicy.Input) (*promptpolicy.Verdict, error) {
		return &promptpolicy.Verdict{
			Matched:          true,
			Confidence:       0.88,
			Rationale:        "matched",
			CostUSD:          0,
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
			STokens:          1,
			Completed:        true,
			Model:            "test",
			Provider:         "test",
		}, nil
	}
	scanner := promptpolicy.NewScanner(testenv.NewLogger(t), evaluator)
	h := promptpolicy.NewHandler(testenv.NewLogger(t), testenv.NewMeterProvider(t), scanner, pub, metering.NewRiskRecorder(meterPub))
	request := newRequest("delete production")
	request.ClearOriginRiskPolicyId()
	request.SetOriginRiskPolicyVersion(0)

	require.NoError(t, h.Handle(t.Context(), request, gcp.MessageMetadata{}))
	require.Len(t, *findings, 1)
	require.Len(t, *readings, 1)
	require.Equal(t, "018ffad2-1c32-7f73-8a54-85306c37a315", (*readings)[0].GetAttributes()[metering.AttributeRiskPolicyID])
	require.Equal(t, "3", (*readings)[0].GetAttributes()[metering.AttributeRiskPolicyVersion])
}

func TestHandle_MalformedMeteringMetadataDoesNotSuppressFinding(t *testing.T) {
	t.Parallel()

	pub, findings := capturingPub(t)
	evaluator := func(context.Context, promptpolicy.Input) (*promptpolicy.Verdict, error) {
		return &promptpolicy.Verdict{
			Matched:          true,
			Confidence:       0.88,
			Rationale:        "matched",
			CostUSD:          0,
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
			STokens:          1,
			Completed:        true,
			Model:            "test",
			Provider:         "test",
		}, nil
	}
	scanner := promptpolicy.NewScanner(testenv.NewLogger(t), evaluator)
	h := promptpolicy.NewHandler(testenv.NewLogger(t), testenv.NewMeterProvider(t), scanner, pub, metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()))
	request := newRequest("delete production")
	request.SetOriginRiskPolicyId("not-a-uuid")

	require.NoError(t, h.Handle(t.Context(), request, gcp.MessageMetadata{}))
	require.Len(t, *findings, 1)
}
