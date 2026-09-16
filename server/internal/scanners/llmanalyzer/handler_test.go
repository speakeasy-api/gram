package llmanalyzer_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/scanners/llmanalyzer"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const (
	testChatMessageID = "018ffad2-1c32-7f73-8a54-85306c37a314"
	testProjectID     = "018ffad2-1c32-7f73-8a54-85306c37a313"
	testPolicyID      = "018ffad2-1c32-7f73-8a54-85306c37a315"
)

func capturingFindingsPub(t *testing.T) (*gcp.MockPublisher[*riskv1.Finding], *[]*riskv1.Finding) {
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

func newAnalysis(body string) *riskv1.LLMAnalysis {
	return riskv1.LLMAnalysis_builder{
		RequestId:               new("req-1"),
		ChatMessageId:           new(testChatMessageID),
		ProjectId:               new(testProjectID),
		OrganizationId:          new("org-1"),
		RiskPolicyId:            new(testPolicyID),
		RiskPolicyVersion:       new(int64(3)),
		CreatedAt:               new("2026-09-16T00:00:00Z"),
		Content:                 &body,
		UserId:                  new("user-1"),
		MessageType:             new("user_message"),
		Body:                    &body,
		ToolName:                new(""),
		ToolCalls:               nil,
		ContentPartId:           nil,
		ChatId:                  nil,
		ParentChatMessageId:     nil,
		OriginRiskPolicyId:      new(testPolicyID),
		OriginRiskPolicyVersion: new(int64(3)),
		MessageLinkReason:       nil,
		ExecutionPath:           new("async"),
		ToolCallId:              nil,
		HookSource:              nil,
		PolicyLinkReason:        nil,
		ExternalConversationId:  nil,
		OrganizationSlug:        new("org-slug"),
		Sources:                 []string{"gitleaks", "presidio"},
		ContentTruncated:        new(false),
	}.Build()
}

func newHandler(t *testing.T, stub *llmanalyzer.StubCompleter, findingsPub gcp.Publisher[*riskv1.Finding], meterPub gcp.Publisher[*meteringv1.MeterReading]) *llmanalyzer.Handler {
	t.Helper()
	var completer llmanalyzer.Completer
	if stub != nil {
		completer = stub
	}
	analyzer := llmanalyzer.NewAnalyzer(testenv.NewLogger(t), testenv.NewTracerProvider(t), completer)
	return llmanalyzer.NewHandler(testenv.NewLogger(t), testenv.NewMeterProvider(t), analyzer, findingsPub, metering.NewRiskRecorder(meterPub))
}

func TestHandle_PublishesOneFindingPerFlaggedRisk(t *testing.T) {
	t.Parallel()

	pub, findings := capturingFindingsPub(t)
	meterPub, readings := capturingMeterPub(t)
	stub := &llmanalyzer.StubCompleter{
		Response: llmanalyzer.VerdictJSON(map[string]int{
			llmanalyzer.KeySecretsLeak:      1,
			llmanalyzer.KeyPersonalDataLeak: 1,
		}, "Plaintext credential next to a home address."),
		Err:              nil,
		PromptTokens:     120,
		CompletionTokens: 40,
		Model:            "risk-judge-4b",
		Calls:            nil,
		ParseFailures:    0,
	}
	h := newHandler(t, stub, pub, meterPub)

	require.NoError(t, h.Handle(t.Context(), newAnalysis("AKIA0000000000000000 lives at 1 Main St"), gcp.MessageMetadata{}))

	require.Len(t, *findings, 2)
	ruleIDs := make([]string, 0, 2)
	for _, f := range *findings {
		ruleIDs = append(ruleIDs, f.GetRuleId())
		require.Equal(t, llmanalyzer.Source, f.GetSource())
		require.Equal(t, "Plaintext credential next to a home address.", f.GetDescription())
		require.Equal(t, "req-1", f.GetRequestId())
		require.Equal(t, testChatMessageID, f.GetChatMessageId())
		require.Equal(t, testProjectID, f.GetProjectId())
		require.Equal(t, "org-1", f.GetOrganizationId())
		require.Equal(t, testPolicyID, f.GetRiskPolicyId())
		require.Equal(t, int64(3), f.GetRiskPolicyVersion())
		require.NotEmpty(t, f.GetId())
		require.Empty(t, f.GetMatch())
		require.Empty(t, f.GetDeadLetterReason())
	}
	require.ElementsMatch(t, []string{llmanalyzer.RuleSecret, llmanalyzer.RulePII}, ruleIDs)

	require.Len(t, *readings, 1)
	reading := (*readings)[0]
	require.Equal(t, string(metering.MeterRiskLLMAnalyzer), reading.GetMeterId())
	require.Positive(t, reading.GetValue())
	require.Equal(t, testPolicyID, reading.GetAttributes()[metering.AttributeRiskPolicyID])
	require.Equal(t, "3", reading.GetAttributes()[metering.AttributeRiskPolicyVersion])
	require.Equal(t, "risk-judge-4b", reading.GetAttributes()[metering.AttributeModel])
	require.Equal(t, llmanalyzer.Provider, reading.GetAttributes()[metering.AttributeProvider])
	require.Equal(t, "async", reading.GetAttributes()[metering.AttributeScanExecutionPath])

	calls := stub.CallsSnapshot()
	require.Len(t, calls, 1)
	require.Equal(t, "org-1", calls[0].Info.OrgID)
	require.Equal(t, "org-slug", calls[0].Info.OrgSlug)
	require.Equal(t, "async", calls[0].Info.Lane)
}

func TestHandle_CleanVerdictPublishesNothingButMeters(t *testing.T) {
	t.Parallel()

	pub, findings := capturingFindingsPub(t)
	meterPub, readings := capturingMeterPub(t)
	stub := &llmanalyzer.StubCompleter{
		Response:         llmanalyzer.VerdictJSON(nil, "Nothing risky."),
		Err:              nil,
		PromptTokens:     0,
		CompletionTokens: 0,
		Model:            "risk-judge-4b",
		Calls:            nil,
		ParseFailures:    0,
	}
	h := newHandler(t, stub, pub, meterPub)

	require.NoError(t, h.Handle(t.Context(), newAnalysis("hello world"), gcp.MessageMetadata{}))
	require.Empty(t, *findings)
	require.Len(t, *readings, 1)
	require.Equal(t, string(metering.MeterRiskLLMAnalyzer), (*readings)[0].GetMeterId())
}

func TestHandle_AnalyzerFailureAcksWithoutFindingsOrUsage(t *testing.T) {
	t.Parallel()

	pub, findings := capturingFindingsPub(t)
	meterPub, readings := capturingMeterPub(t)
	stub := &llmanalyzer.StubCompleter{
		Response:         "",
		Err:              errors.New("model unreachable"),
		PromptTokens:     0,
		CompletionTokens: 0,
		Model:            "risk-judge-4b",
		Calls:            nil,
		ParseFailures:    0,
	}
	h := newHandler(t, stub, pub, meterPub)

	require.NoError(t, h.Handle(t.Context(), newAnalysis("delete production"), gcp.MessageMetadata{}))
	require.Empty(t, *findings)
	require.Empty(t, *readings)
	require.Len(t, stub.CallsSnapshot(), 1)
}

func TestHandle_UnparsableReplyAcksWithoutFindingsOrUsage(t *testing.T) {
	t.Parallel()

	pub, findings := capturingFindingsPub(t)
	meterPub, readings := capturingMeterPub(t)
	stub := &llmanalyzer.StubCompleter{
		Response:         "I cannot help with that.",
		Err:              nil,
		PromptTokens:     0,
		CompletionTokens: 0,
		Model:            "risk-judge-4b",
		Calls:            nil,
		ParseFailures:    0,
	}
	h := newHandler(t, stub, pub, meterPub)

	require.NoError(t, h.Handle(t.Context(), newAnalysis("delete production"), gcp.MessageMetadata{}))
	require.Empty(t, *findings)
	require.Empty(t, *readings)
	require.Equal(t, 1, stub.ParseFailures)
}

func TestHandle_PublishFailureNacks(t *testing.T) {
	t.Parallel()

	pub := gcp.NewMockPublisher[*riskv1.Finding]()
	pub.On("Publish", mock.Anything, mock.Anything).Return(gcp.NewErrPublishResult(errors.New("topic unavailable")))
	meterPub, readings := capturingMeterPub(t)
	stub := &llmanalyzer.StubCompleter{
		Response:         llmanalyzer.VerdictJSON(map[string]int{llmanalyzer.KeySecretsLeak: 1}, "Credential in plaintext."),
		Err:              nil,
		PromptTokens:     0,
		CompletionTokens: 0,
		Model:            "risk-judge-4b",
		Calls:            nil,
		ParseFailures:    0,
	}
	h := newHandler(t, stub, pub, meterPub)

	err := h.Handle(t.Context(), newAnalysis("AKIA0000000000000000"), gcp.MessageMetadata{})
	require.Error(t, err)
	require.ErrorContains(t, err, "topic unavailable")
	// Usage is recorded on the completed scan even though the message will be
	// redelivered: the reading id is deterministic, so a replay converges.
	require.Len(t, *readings, 1)
}

func TestHandle_DisabledAnalyzerAcksWithoutPublishing(t *testing.T) {
	t.Parallel()

	pub, findings := capturingFindingsPub(t)
	meterPub, readings := capturingMeterPub(t)
	h := newHandler(t, nil, pub, meterPub)

	require.NoError(t, h.Handle(t.Context(), newAnalysis("AKIA0000000000000000"), gcp.MessageMetadata{}))
	require.NoError(t, h.Handle(t.Context(), newAnalysis("AKIA0000000000000000"), gcp.MessageMetadata{}))
	require.Empty(t, *findings)
	require.Empty(t, *readings)
}

func TestHandle_ToolCallsRenderRealIDsAndNames(t *testing.T) {
	t.Parallel()

	pub, findings := capturingFindingsPub(t)
	meterPub, _ := capturingMeterPub(t)
	stub := &llmanalyzer.StubCompleter{
		Response:         llmanalyzer.VerdictJSON(map[string]int{llmanalyzer.KeyDestructiveToolCall: 1}, "Drops the production database."),
		Err:              nil,
		PromptTokens:     0,
		CompletionTokens: 0,
		Model:            "risk-judge-4b",
		Calls:            nil,
		ParseFailures:    0,
	}
	h := newHandler(t, stub, pub, meterPub)

	request := newAnalysis("")
	request.SetMessageType("tool_request")
	request.SetToolCalls([]*riskv1.LLMAnalysis_ToolCall{
		riskv1.LLMAnalysis_ToolCall_builder{
			Id:        new("call_prod_drop"),
			Name:      new("run_sql"),
			Arguments: new(`{"statement":"DROP DATABASE prod"}`),
		}.Build(),
		riskv1.LLMAnalysis_ToolCall_builder{
			Id:        new("call_list"),
			Name:      new("list_tables"),
			Arguments: new(`{}`),
		}.Build(),
	})

	require.NoError(t, h.Handle(t.Context(), request, gcp.MessageMetadata{}))

	calls := stub.CallsSnapshot()
	require.Len(t, calls, 1)
	require.Len(t, calls[0].Messages, 2)
	userPrompt := calls[0].Messages[1].Content
	require.Contains(t, userPrompt, "call_prod_drop")
	require.Contains(t, userPrompt, "run_sql")
	require.Contains(t, userPrompt, "DROP DATABASE prod")
	require.Contains(t, userPrompt, "call_list")
	require.Contains(t, userPrompt, "list_tables")

	require.Len(t, *findings, 1)
	require.Equal(t, llmanalyzer.RuleDestructiveTool, (*findings)[0].GetRuleId())
}

func TestHandle_SingleToolRequestRendersRequestToolCallID(t *testing.T) {
	t.Parallel()

	pub, _ := capturingFindingsPub(t)
	meterPub, _ := capturingMeterPub(t)
	stub := &llmanalyzer.StubCompleter{
		Response:         llmanalyzer.VerdictJSON(nil, "Benign."),
		Err:              nil,
		PromptTokens:     0,
		CompletionTokens: 0,
		Model:            "risk-judge-4b",
		Calls:            nil,
		ParseFailures:    0,
	}
	h := newHandler(t, stub, pub, meterPub)

	request := newAnalysis(`{"path":"README.md"}`)
	request.SetMessageType("tool_request")
	request.SetToolName("read_file")
	request.SetToolCallId("call_read_readme")

	require.NoError(t, h.Handle(t.Context(), request, gcp.MessageMetadata{}))

	calls := stub.CallsSnapshot()
	require.Len(t, calls, 1)
	userPrompt := calls[0].Messages[1].Content
	require.Contains(t, userPrompt, "call_read_readme")
	require.Contains(t, userPrompt, "read_file")
	require.Contains(t, userPrompt, "README.md")
}
