package llmanalyzer_test

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/requestreply"
	"github.com/speakeasy-api/gram/server/internal/risk/enforcereply"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/llmanalyzer"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestEnforceHandler_WritesOKReplyWithFindings(t *testing.T) {
	t.Parallel()

	stub := flaggingStub(map[string]int{
		llmanalyzer.KeySecretsLeak:      1,
		llmanalyzer.KeyPersonalDataLeak: 1,
	}, "the message exposes a credential next to a home address")
	handler, mr, client, reader := newEnforceHandler(t, stub)
	body := "AKIA-synthetic-key for Jane Doe at 1 Example Street"
	deliveryAttempt := 2

	err := handler.Handle(t.Context(), userEnforcement("scan-ok", body), replyMetadata("replica-ok", "scan-ok", &deliveryAttempt))
	require.NoError(t, err)
	require.Equal(t, 60*time.Second, mr.TTL(enforcereply.InboxKey("replica-ok")))

	reply, payload := popReply(t, client, "replica-ok")
	require.NotContains(t, string(payload), body, "reply must never carry raw content")
	require.Equal(t, "scan-ok", reply.GetCorrelationId())
	require.Equal(t, riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_LLM_ANALYZER, reply.GetScanner())
	require.Equal(t, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK, reply.GetStatus())
	require.Empty(t, reply.GetReason())
	require.Empty(t, reply.GetPolicyId())
	require.Equal(t, int32(2), reply.GetDiagnostics().GetDeliveryAttempt())
	require.NotEmpty(t, reply.GetDiagnostics().GetConsumerId())
	require.GreaterOrEqual(t, reply.GetDiagnostics().GetScanDurationMs(), int64(0))

	require.Len(t, reply.GetFindings(), 2)
	byRule := make(map[string]*riskv1.EnforcementFinding, 2)
	for _, finding := range reply.GetFindings() {
		byRule[finding.GetRuleId()] = finding
	}
	require.Contains(t, byRule, llmanalyzer.RuleSecret)
	require.Contains(t, byRule, llmanalyzer.RulePII)
	require.Equal(t, "secrets", byRule[llmanalyzer.RuleSecret].GetCategory())
	require.Equal(t, "pii", byRule[llmanalyzer.RulePII].GetCategory())
	for _, finding := range reply.GetFindings() {
		require.InDelta(t, 1.0, finding.GetScore(), 0)
		require.Equal(t, scanners.SurfaceNone, finding.GetSurface())
		require.Equal(t, "the message exposes a credential next to a home address", finding.GetDescription())
		require.Zero(t, finding.GetStartPos())
		require.Zero(t, finding.GetEndPos())
		require.Empty(t, finding.GetMaskedPreview())
		require.Empty(t, finding.GetFingerprint())
		require.Empty(t, finding.GetField())
		require.Empty(t, finding.GetPath())
		require.Empty(t, finding.GetToolCallId())
	}

	calls := stub.CallsSnapshot()
	require.Len(t, calls, 1)
	require.Equal(t, llmanalyzer.CallInfo{OrgID: "org-enforce", OrgSlug: "acme", ScanMode: llmanalyzer.ScanModeSync}, calls[0].Info)
	require.Contains(t, calls[0].Messages[1].Content, "<content>\n"+body+"\n</content>")

	data := collectMetrics(t, reader)
	require.Equal(t, int64(1), counterValue(t, data, "risk.enforcement.llm.requests",
		attr.RiskScanMode(llmanalyzer.ScanModeSync), attr.Outcome(llmanalyzer.EnforceOutcomeOK)))
}

func TestEnforceHandler_CleanVerdictRepliesOKWithoutFindings(t *testing.T) {
	t.Parallel()

	stub := flaggingStub(map[string]int{}, "nothing risky here")
	handler, _, client, _ := newEnforceHandler(t, stub)

	err := handler.Handle(t.Context(), userEnforcement("scan-clean", "hello world"), replyMetadata("replica-clean", "scan-clean", nil))
	require.NoError(t, err)

	reply, _ := popReply(t, client, "replica-clean")
	require.Equal(t, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK, reply.GetStatus())
	require.Empty(t, reply.GetFindings())
	require.Equal(t, int32(0), reply.GetDiagnostics().GetDeliveryAttempt())
}

func TestEnforceHandler_ReportsTimeoutAsErrorReply(t *testing.T) {
	t.Parallel()

	stub := failingStub(llmanalyzer.ErrTimeout)
	handler, _, client, reader := newEnforceHandler(t, stub)

	err := handler.Handle(t.Context(), userEnforcement("scan-timeout", "hello"), replyMetadata("replica-timeout", "scan-timeout", nil))
	require.NoError(t, err, "model failures are acknowledged; redelivery cannot rescue an inline scan")

	reply, _ := popReply(t, client, "replica-timeout")
	require.Equal(t, "scan-timeout", reply.GetCorrelationId())
	require.Equal(t, riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_LLM_ANALYZER, reply.GetScanner())
	require.Equal(t, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_ERROR, reply.GetStatus())
	require.True(t, strings.HasPrefix(reply.GetReason(), llmanalyzer.ReasonTimeout+": "), reply.GetReason())
	require.Contains(t, reply.GetReason(), llmanalyzer.ErrTimeout.Error())
	require.Empty(t, reply.GetFindings())
	require.NotEmpty(t, reply.GetDiagnostics().GetConsumerId())

	data := collectMetrics(t, reader)
	require.Equal(t, int64(1), counterValue(t, data, "risk.enforcement.llm.requests",
		attr.RiskScanMode(llmanalyzer.ScanModeSync), attr.Outcome(llmanalyzer.EnforceOutcomeError)))
}

func TestEnforceHandler_ClassifiesUpstreamAndParseFailures(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		err    error
		reason string
	}{
		{name: "rate-limited", err: &llmanalyzer.UpstreamError{Status: 429, Body: "slow down"}, reason: llmanalyzer.ReasonRateLimited},
		{name: "upstream-5xx", err: &llmanalyzer.UpstreamError{Status: 503, Body: ""}, reason: llmanalyzer.ReasonUpstream5xx},
		{name: "empty", err: llmanalyzer.ErrEmptyCompletion, reason: llmanalyzer.ReasonEmptyCompletion},
		{name: "transport", err: errors.New("connection reset"), reason: llmanalyzer.ReasonRequestError},
	}
	for _, tc := range cases {
		handler, _, client, _ := newEnforceHandler(t, failingStub(tc.err))
		requestID := "scan-" + tc.name
		err := handler.Handle(t.Context(), userEnforcement(requestID, "hello"), replyMetadata("replica-"+tc.name, requestID, nil))
		require.NoError(t, err, tc.name)

		reply, _ := popReply(t, client, "replica-"+tc.name)
		require.Equal(t, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_ERROR, reply.GetStatus(), tc.name)
		require.True(t, strings.HasPrefix(reply.GetReason(), tc.reason+": "), "%s: %s", tc.name, reply.GetReason())
		require.NotContains(t, reply.GetReason(), "slow down", "%s: the upstream body is log-only and never travels in the reply", tc.name)
	}
}

func TestEnforceHandler_ReportsUnparsableVerdictAsErrorReply(t *testing.T) {
	t.Parallel()

	stub := &llmanalyzer.StubCompleter{
		Response:         "I cannot answer that",
		Err:              nil,
		PromptTokens:     0,
		CompletionTokens: 0,
		Model:            "risk-judge-4b",
		Calls:            nil,
		ParseFailures:    0,
	}
	handler, _, client, _ := newEnforceHandler(t, stub)

	err := handler.Handle(t.Context(), userEnforcement("scan-parse", "hello"), replyMetadata("replica-parse", "scan-parse", nil))
	require.NoError(t, err)

	reply, _ := popReply(t, client, "replica-parse")
	require.Equal(t, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_ERROR, reply.GetStatus())
	require.True(t, strings.HasPrefix(reply.GetReason(), llmanalyzer.ReasonParseError+": "), reply.GetReason())
	require.Equal(t, 1, stub.ParseFailures)
}

func TestEnforceHandler_TruncatesReasonTo256Runes(t *testing.T) {
	t.Parallel()

	stub := failingStub(errors.New(strings.Repeat("é", 600)))
	handler, _, client, _ := newEnforceHandler(t, stub)

	err := handler.Handle(t.Context(), userEnforcement("scan-long", "hello"), replyMetadata("replica-long", "scan-long", nil))
	require.NoError(t, err)

	reply, _ := popReply(t, client, "replica-long")
	require.Equal(t, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_ERROR, reply.GetStatus())
	require.Equal(t, 256, utf8.RuneCountInString(reply.GetReason()))
	require.True(t, strings.HasPrefix(reply.GetReason(), llmanalyzer.ReasonRequestError+": "), reply.GetReason())
}

func TestEnforceHandler_DisabledAnalyzerRepliesDeadLetter(t *testing.T) {
	t.Parallel()

	handler, _, client, reader := newEnforceHandler(t, nil)

	err := handler.Handle(t.Context(), userEnforcement("scan-disabled", "hello"), replyMetadata("replica-disabled", "scan-disabled", nil))
	require.NoError(t, err)

	reply, _ := popReply(t, client, "replica-disabled")
	require.Equal(t, "scan-disabled", reply.GetCorrelationId())
	require.Equal(t, riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_LLM_ANALYZER, reply.GetScanner())
	require.Equal(t, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_DEAD_LETTER, reply.GetStatus())
	require.Equal(t, llmanalyzer.ReasonDisabled, reply.GetReason())
	require.Empty(t, reply.GetFindings())

	data := collectMetrics(t, reader)
	require.Equal(t, int64(1), counterValue(t, data, "risk.enforcement.llm.requests",
		attr.RiskScanMode(llmanalyzer.ScanModeSync), attr.Outcome(llmanalyzer.EnforceOutcomeDeadLetter)))
}

func TestEnforceHandler_AcknowledgesStaleRequest(t *testing.T) {
	t.Parallel()

	stub := flaggingStub(map[string]int{llmanalyzer.KeySecretsLeak: 1}, "leak")
	handler, mr, _, reader := newEnforceHandler(t, stub, llmanalyzer.WithMaxRequestAge(30*time.Second))
	request := userEnforcement("scan-stale", "hello")
	request.SetCreatedAt(time.Now().Add(-31 * time.Second).UTC().Format(time.RFC3339Nano))

	err := handler.Handle(t.Context(), request, replyMetadata("replica-stale", "scan-stale", nil))
	require.NoError(t, err)
	require.False(t, mr.Exists(enforcereply.InboxKey("replica-stale")))
	require.Empty(t, stub.CallsSnapshot(), "stale requests must not spend model budget")

	data := collectMetrics(t, reader)
	require.Equal(t, int64(1), counterValue(t, data, "risk.enforcement.llm.stale_dropped", attr.RiskScanMode(llmanalyzer.ScanModeSync)))
	require.Equal(t, int64(0), counterValue(t, data, "risk.enforcement.llm.requests"))
}

func TestEnforceHandler_AcknowledgesFarFutureRequest(t *testing.T) {
	t.Parallel()

	stub := flaggingStub(map[string]int{}, "clean")
	handler, mr, _, reader := newEnforceHandler(t, stub)
	request := userEnforcement("scan-future", "hello")
	request.SetCreatedAt(time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339Nano))

	err := handler.Handle(t.Context(), request, replyMetadata("replica-future", "scan-future", nil))
	require.NoError(t, err)
	require.False(t, mr.Exists(enforcereply.InboxKey("replica-future")))
	require.Empty(t, stub.CallsSnapshot())

	data := collectMetrics(t, reader)
	require.Equal(t, int64(1), counterValue(t, data, "risk.enforcement.llm.stale_dropped"))
}

func TestEnforceHandler_AcknowledgesReplyWriteFailure(t *testing.T) {
	t.Parallel()

	stub := flaggingStub(map[string]int{}, "clean")
	handler, _, client, reader := newEnforceHandler(t, stub)
	require.NoError(t, client.Close())

	err := handler.Handle(t.Context(), userEnforcement("scan-write-failure", "hello"), replyMetadata("replica-write-failure", "scan-write-failure", nil))
	require.NoError(t, err, "a reply the requester can no longer receive must not be redelivered")

	data := collectMetrics(t, reader)
	require.Equal(t, int64(1), counterValue(t, data, "risk.enforcement.llm.reply_write_errors", attr.RiskScanMode(llmanalyzer.ScanModeSync)))
	require.Equal(t, int64(1), counterValue(t, data, "risk.enforcement.llm.requests", attr.Outcome(llmanalyzer.EnforceOutcomeOK)))
}

func TestEnforceHandler_RejectsMissingReplyURNAttribute(t *testing.T) {
	t.Parallel()

	stub := flaggingStub(map[string]int{}, "clean")
	handler, mr, _, _ := newEnforceHandler(t, stub)

	err := handler.Handle(t.Context(), userEnforcement("scan-no-urn", "hello"), gcp.MessageMetadata{Attributes: nil, DeliveryAttempt: nil})
	require.ErrorContains(t, err, "llm enforcement reply urn attribute is required")
	require.Empty(t, stub.CallsSnapshot())
	require.Empty(t, mr.Keys())
}

func TestEnforceHandler_RejectsMalformedReplyURN(t *testing.T) {
	t.Parallel()

	stub := flaggingStub(map[string]int{}, "clean")
	handler, _, _, _ := newEnforceHandler(t, stub)
	meta := replyMetadata("replica-bad", "scan-bad", nil)
	meta.Attributes[requestreply.ReplyURNAttribute] = "not-a-urn"

	err := handler.Handle(t.Context(), userEnforcement("scan-bad", "hello"), meta)
	require.ErrorContains(t, err, "parse llm enforcement reply urn")
	require.Empty(t, stub.CallsSnapshot())
}

func TestEnforceHandler_RejectsMalformedCreatedAt(t *testing.T) {
	t.Parallel()

	stub := flaggingStub(map[string]int{}, "clean")
	handler, _, _, _ := newEnforceHandler(t, stub)
	request := userEnforcement("scan-malformed", "hello")
	request.SetCreatedAt("not-a-timestamp")

	err := handler.Handle(t.Context(), request, replyMetadata("replica-malformed", "scan-malformed", nil))
	require.ErrorContains(t, err, "parse llm enforcement created_at")
	require.Empty(t, stub.CallsSnapshot())
}

func TestEnforceHandler_RejectsMissingTenant(t *testing.T) {
	t.Parallel()

	stub := flaggingStub(map[string]int{}, "clean")
	handler, _, _, _ := newEnforceHandler(t, stub)

	noOrg := userEnforcement("scan-no-org", "hello")
	noOrg.SetOrganizationId("")
	err := handler.Handle(t.Context(), noOrg, replyMetadata("replica-no-org", "scan-no-org", nil))
	require.ErrorContains(t, err, "organization id is required")

	noProject := userEnforcement("scan-no-project", "hello")
	noProject.SetProjectId("")
	err = handler.Handle(t.Context(), noProject, replyMetadata("replica-no-project", "scan-no-project", nil))
	require.ErrorContains(t, err, "project id is required")
	require.Empty(t, stub.CallsSnapshot())
}

func TestEnforceHandler_RendersToolCallsWithHarnessIDs(t *testing.T) {
	t.Parallel()

	stub := flaggingStub(map[string]int{llmanalyzer.KeyDestructiveToolCall: 1}, "drops the production table")
	handler, _, client, _ := newEnforceHandler(t, stub)
	request := userEnforcement("scan-tools", "")
	request.SetMessageType("tool_request")
	request.SetToolCalls([]*riskv1.LLMEnforcement_ToolCall{
		riskv1.LLMEnforcement_ToolCall_builder{
			Id:        new("call_alpha"),
			Name:      new("mcp__db__run_sql"),
			Arguments: new(`{"sql":"DROP TABLE users"}`),
		}.Build(),
		riskv1.LLMEnforcement_ToolCall_builder{
			Id:        new("call_beta"),
			Name:      new("Bash"),
			Arguments: new(`{"command":"rm -rf /"}`),
		}.Build(),
	})

	err := handler.Handle(t.Context(), request, replyMetadata("replica-tools", "scan-tools", nil))
	require.NoError(t, err)

	calls := stub.CallsSnapshot()
	require.Len(t, calls, 1)
	prompt := calls[0].Messages[1].Content
	require.Contains(t, prompt, "<content>\n\n</content>")
	require.Contains(t, prompt, `{"id": "call_alpha", "type": "function", "function": {"name": "mcp__db__run_sql", "arguments": "{\"sql\":\"DROP TABLE users\"}"}}`)
	require.Contains(t, prompt, `{"id": "call_beta", "type": "function", "function": {"name": "Bash", "arguments": "{\"command\":\"rm -rf /\"}"}}`)

	reply, _ := popReply(t, client, "replica-tools")
	require.Equal(t, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK, reply.GetStatus())
	require.Len(t, reply.GetFindings(), 1)
	require.Equal(t, llmanalyzer.RuleDestructiveTool, reply.GetFindings()[0].GetRuleId())
	require.Equal(t, "destructive_tool", reply.GetFindings()[0].GetCategory())
	require.Equal(t, "drops the production table", reply.GetFindings()[0].GetDescription())
}

func TestEnforceHandler_RendersSingleToolRequestWithHarnessID(t *testing.T) {
	t.Parallel()

	stub := flaggingStub(map[string]int{}, "clean")
	handler, _, _, _ := newEnforceHandler(t, stub)
	request := userEnforcement("scan-single-tool", `{"command":"ls -la"}`)
	request.SetMessageType("tool_request")
	request.SetToolName("Bash")
	request.SetToolCallId("toolu_harness_1")

	err := handler.Handle(t.Context(), request, replyMetadata("replica-single-tool", "scan-single-tool", nil))
	require.NoError(t, err)

	calls := stub.CallsSnapshot()
	require.Len(t, calls, 1)
	prompt := calls[0].Messages[1].Content
	require.Contains(t, prompt, "<content>\n\n</content>")
	require.Contains(t, prompt, `{"id": "toolu_harness_1", "type": "function", "function": {"name": "Bash", "arguments": "{\"command\":\"ls -la\"}"}}`)
}

func TestEnforceHandler_RendersToolResultAsContent(t *testing.T) {
	t.Parallel()

	stub := flaggingStub(map[string]int{}, "clean")
	handler, _, _, _ := newEnforceHandler(t, stub)
	request := userEnforcement("scan-tool-result", "total 0\ndrwxr-xr-x  2 user  staff  64 Jan  1 00:00 .")
	request.SetMessageType("tool_response")
	request.SetToolName("Bash")
	request.SetToolCallId("toolu_harness_2")

	err := handler.Handle(t.Context(), request, replyMetadata("replica-tool-result", "scan-tool-result", nil))
	require.NoError(t, err)

	calls := stub.CallsSnapshot()
	require.Len(t, calls, 1)
	prompt := calls[0].Messages[1].Content
	require.Contains(t, prompt, "<content>\ntotal 0\ndrwxr-xr-x  2 user  staff  64 Jan  1 00:00 .\n</content>")
	require.Contains(t, prompt, "<tool_calls>\n[]\n</tool_calls>")
}

func TestEnforceHandler_MetersCompletedAnalysis(t *testing.T) {
	t.Parallel()

	stub := flaggingStub(map[string]int{llmanalyzer.KeySecretsLeak: 1}, "leaked key")
	meterPub, readings := capturingMeterPub(t)
	handler, _, client, _ := newEnforceHandler(t, stub, llmanalyzer.WithRiskRecorder(metering.NewRiskRecorder(meterPub)))
	request := userEnforcement("scan-metered", "AKIA-synthetic-key")
	request.SetOriginRiskPolicyId("018ffad2-1c32-7f73-8a54-85306c37a315")
	request.SetOriginRiskPolicyVersion(3)
	request.SetMessageLinkReason("realtime_not_persisted")

	err := handler.Handle(t.Context(), request, replyMetadata("replica-metered", "scan-metered", nil))
	require.NoError(t, err)

	reply, _ := popReply(t, client, "replica-metered")
	require.Equal(t, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK, reply.GetStatus())

	require.Len(t, *readings, 1)
	reading := (*readings)[0]
	require.Equal(t, string(metering.MeterRiskLLMAnalyzer), reading.GetMeterId())
	require.Positive(t, reading.GetValue())
	require.Equal(t, "018ffad2-1c32-7f73-8a54-85306c37a315", reading.GetAttributes()[metering.AttributeRiskPolicyID])
	require.Equal(t, "3", reading.GetAttributes()[metering.AttributeRiskPolicyVersion])
	require.Equal(t, "risk-judge-4b", reading.GetAttributes()[metering.AttributeModel])
	require.Equal(t, llmanalyzer.Provider, reading.GetAttributes()[metering.AttributeProvider])
	require.Equal(t, "realtime_streams", reading.GetAttributes()[metering.AttributeScanExecutionPath])
}

func TestEnforceHandler_SeparatesMeteredRequestsLinkedToOneMessage(t *testing.T) {
	t.Parallel()

	stub := flaggingStub(map[string]int{}, "clean")
	meterPub, readings := capturingMeterPub(t)
	handler, _, _, _ := newEnforceHandler(t, stub, llmanalyzer.WithRiskRecorder(metering.NewRiskRecorder(meterPub)))
	request := userEnforcement("", "hello")
	request.SetOriginRiskPolicyId("018ffad2-1c32-7f73-8a54-85306c37a315")
	request.SetOriginRiskPolicyVersion(3)
	request.SetChatMessageId("018ffad2-1c32-7f73-8a54-85306c37a316")

	for _, requestID := range []string{"first-request", "first-request", "second-request"} {
		request.SetRequestId(requestID)
		require.NoError(t, handler.Handle(t.Context(), request, replyMetadata("replica-identity", requestID, nil)))
	}
	require.Len(t, *readings, 3)
	require.Equal(t, (*readings)[0].GetId(), (*readings)[1].GetId())
	require.NotEqual(t, (*readings)[0].GetId(), (*readings)[2].GetId())
}

func TestEnforceHandler_DoesNotMeterFailedAnalysis(t *testing.T) {
	t.Parallel()

	meterPub, readings := capturingMeterPub(t)
	recorder := metering.NewRiskRecorder(meterPub)

	errored, _, client, _ := newEnforceHandler(t, failingStub(llmanalyzer.ErrTimeout), llmanalyzer.WithRiskRecorder(recorder))
	request := userEnforcement("scan-error-unmetered", "hello")
	request.SetOriginRiskPolicyId("018ffad2-1c32-7f73-8a54-85306c37a315")
	request.SetOriginRiskPolicyVersion(3)
	request.SetMessageLinkReason("realtime_not_persisted")
	require.NoError(t, errored.Handle(t.Context(), request, replyMetadata("replica-error-unmetered", "scan-error-unmetered", nil)))
	reply, _ := popReply(t, client, "replica-error-unmetered")
	require.Equal(t, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_ERROR, reply.GetStatus())

	disabled, _, client, _ := newEnforceHandler(t, nil, llmanalyzer.WithRiskRecorder(recorder))
	request.SetRequestId("scan-disabled-unmetered")
	require.NoError(t, disabled.Handle(t.Context(), request, replyMetadata("replica-disabled-unmetered", "scan-disabled-unmetered", nil)))
	reply, _ = popReply(t, client, "replica-disabled-unmetered")
	require.Equal(t, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_DEAD_LETTER, reply.GetStatus())

	require.Empty(t, *readings)
}

func TestEnforceHandler_MeterFailurePreservesReply(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	meterPub := gcp.NewMockPublisher[*meteringv1.MeterReading]()
	meterPub.On("Publish", mock.Anything, mock.Anything).Return(errors.New("meter unavailable"))
	_, client, writer := newReplyWriter(t)
	analyzer := llmanalyzer.NewAnalyzer(testenv.NewLogger(t), testenv.NewTracerProvider(t), flaggingStub(map[string]int{}, "clean"))
	handler := llmanalyzer.NewEnforceHandler(
		slog.New(slog.NewTextHandler(&logs, nil)),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		analyzer,
		writer,
		llmanalyzer.WithRiskRecorder(metering.NewRiskRecorder(meterPub)),
	)
	request := userEnforcement("scan-meter-failure", "hello")
	request.SetOriginRiskPolicyId("018ffad2-1c32-7f73-8a54-85306c37a315")
	request.SetOriginRiskPolicyVersion(3)
	request.SetMessageLinkReason("realtime_not_persisted")

	require.NoError(t, handler.Handle(t.Context(), request, replyMetadata("replica-meter-failure", "scan-meter-failure", nil)))
	reply, _ := popReply(t, client, "replica-meter-failure")
	require.Equal(t, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK, reply.GetStatus())
	require.Contains(t, logs.String(), "meter unavailable")
}

func TestEnforceHandler_SkipsMeteringWithInvalidAttribution(t *testing.T) {
	t.Parallel()

	stub := flaggingStub(map[string]int{}, "clean")
	meterPub, readings := capturingMeterPub(t)
	handler, _, client, _ := newEnforceHandler(t, stub, llmanalyzer.WithRiskRecorder(metering.NewRiskRecorder(meterPub)))

	// No policy, no policy-link reason, no message link: attribution is
	// incomplete, so the reply still lands but nothing is metered.
	err := handler.Handle(t.Context(), userEnforcement("scan-unattributed", "hello"), replyMetadata("replica-unattributed", "scan-unattributed", nil))
	require.NoError(t, err)
	reply, _ := popReply(t, client, "replica-unattributed")
	require.Equal(t, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK, reply.GetStatus())
	require.Empty(t, *readings)
}
