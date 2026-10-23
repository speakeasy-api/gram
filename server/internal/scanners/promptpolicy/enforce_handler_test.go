package promptpolicy

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/requestreply"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type captureEnforcementReply struct {
	reply *riskv1.EnforcementReply
}

func (w *captureEnforcementReply) Reply(_ context.Context, _ string, reply *riskv1.EnforcementReply) error {
	w.reply = reply
	return nil
}

type incompletePromptPolicyScanner struct {
	config  Config
	message judgemessage.Message
}

func (s *incompletePromptPolicyScanner) ScanWithVerdict(_ context.Context, _, _, _, _ string, config Config, message judgemessage.Message) (scanners.Result, *Verdict) {
	s.config = config
	s.message = message
	return scanners.Result{
		Findings:  []scanners.Finding{NewFinding(Verdict{Matched: true, Rationale: "judge unavailable"})},
		Completed: false,
	}, nil
}

func TestEnforceHandlerPreservesFailClosedFindingOnIncompleteJudge(t *testing.T) {
	t.Parallel()

	scanner := &incompletePromptPolicyScanner{}
	writer := &captureEnforcementReply{}
	handler, err := NewEnforceHandler(
		testenv.NewLogger(t),
		testenv.NewMeterProvider(t),
		scanner,
		writer,
		metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()),
		EnforceHandlerConfig{},
	)
	require.NoError(t, err)

	policyID := "0198f1f4-0000-7000-8000-000000000001"
	correlationID := "0198f1f4-0000-7000-8000-000000000000"
	err = handler.Handle(t.Context(), riskv1.PromptPolicyEnforcement_builder{
		CreatedAt:      new(time.Now().UTC().Format(time.RFC3339Nano)),
		OrganizationId: new("org-test"),
		ProjectId:      new("project-test"),
		RiskPolicyId:   new(policyID),
		Content:        new(`{"command":"rm -rf /"}`),
		Prompt:         new("deny unsafe requests"),
		ModelConfig:    []byte(`{"fail_open":false}`),
		MessageType:    new("tool_request"),
		ToolCalls: []*riskv1.PromptPolicyEnforcement_ToolCall{
			riskv1.PromptPolicyEnforcement_ToolCall_builder{
				Name:      new("Bash"),
				Arguments: new(`{"command":"rm -rf /"}`),
			}.Build(),
		},
	}.Build(), gcp.MessageMetadata{Attributes: map[string]string{
		requestreply.ReplyURNAttribute: "urn:gram:risk:enforce:replica-test:" + correlationID,
	}})
	require.NoError(t, err)
	require.False(t, scanner.config.FailOpen)
	require.Equal(t, message.ToolRequest, scanner.message.Type)
	require.Equal(t, "Bash", scanner.message.ToolName)
	require.JSONEq(t, `{"command":"rm -rf /"}`, scanner.message.Body)
	require.Empty(t, scanner.message.ToolCalls)
	require.NotNil(t, writer.reply)
	require.Equal(t, policyID, writer.reply.GetPolicyId())
	require.True(t, writer.reply.GetIncomplete())
	require.Len(t, writer.reply.GetFindings(), 1)
	require.Equal(t, Rule, writer.reply.GetFindings()[0].GetRuleId())
	require.Equal(t, "judge unavailable", writer.reply.GetFindings()[0].GetDescription())
}

func TestEnforceHandlerDoesNotReportUnusedContentTruncation(t *testing.T) {
	t.Parallel()

	scanner := &incompletePromptPolicyScanner{}
	writer := &captureEnforcementReply{}
	handler, err := NewEnforceHandler(
		testenv.NewLogger(t),
		testenv.NewMeterProvider(t),
		scanner,
		writer,
		metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()),
		EnforceHandlerConfig{MaxContentBytes: 4},
	)
	require.NoError(t, err)

	err = handler.Handle(t.Context(), riskv1.PromptPolicyEnforcement_builder{
		CreatedAt:      new(time.Now().UTC().Format(time.RFC3339Nano)),
		OrganizationId: new("org-test"),
		ProjectId:      new("project-test"),
		RiskPolicyId:   new("0198f1f4-0000-7000-8000-000000000001"),
		Content:        new(strings.Repeat("x", 16)),
		Body:           new("safe"),
		Prompt:         new("deny unsafe requests"),
		MessageType:    new("user"),
	}.Build(), gcp.MessageMetadata{Attributes: map[string]string{
		requestreply.ReplyURNAttribute: "urn:gram:risk:enforce:replica-test:0198f1f4-0000-7000-8000-000000000002",
	}})
	require.NoError(t, err)
	require.Equal(t, "safe", scanner.message.Body)
	require.False(t, writer.reply.GetContentTruncated())
}

func TestEnforceHandlerReportsJudgePayloadTruncation(t *testing.T) {
	t.Parallel()

	scanner := &incompletePromptPolicyScanner{}
	writer := &captureEnforcementReply{}
	handler, err := NewEnforceHandler(
		testenv.NewLogger(t),
		testenv.NewMeterProvider(t),
		scanner,
		writer,
		metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()),
		EnforceHandlerConfig{},
	)
	require.NoError(t, err)

	body := strings.Repeat("x", 16001)
	err = handler.Handle(t.Context(), riskv1.PromptPolicyEnforcement_builder{
		CreatedAt:      new(time.Now().UTC().Format(time.RFC3339Nano)),
		OrganizationId: new("org-test"),
		ProjectId:      new("project-test"),
		RiskPolicyId:   new("0198f1f4-0000-7000-8000-000000000001"),
		Content:        new(body),
		Body:           new(body),
		Prompt:         new("deny unsafe requests"),
		MessageType:    new("user"),
	}.Build(), gcp.MessageMetadata{Attributes: map[string]string{
		requestreply.ReplyURNAttribute: "urn:gram:risk:enforce:replica-test:0198f1f4-0000-7000-8000-000000000002",
	}})
	require.NoError(t, err)
	require.Equal(t, body, scanner.message.Body)
	require.True(t, writer.reply.GetContentTruncated())
}

func TestEnforceHandlerDropsRequestsOlderThanLaneWait(t *testing.T) {
	t.Parallel()

	scanner := &incompletePromptPolicyScanner{}
	handler, err := NewEnforceHandler(
		testenv.NewLogger(t),
		testenv.NewMeterProvider(t),
		scanner,
		&captureEnforcementReply{},
		metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()),
		EnforceHandlerConfig{},
	)
	require.NoError(t, err)

	err = handler.Handle(t.Context(), riskv1.PromptPolicyEnforcement_builder{
		CreatedAt:      new(time.Now().Add(-16 * time.Second).UTC().Format(time.RFC3339Nano)),
		OrganizationId: new("org-test"),
		ProjectId:      new("project-test"),
		RiskPolicyId:   new("0198f1f4-0000-7000-8000-000000000001"),
		Content:        new("must not be scanned"),
		Prompt:         new("deny unsafe requests"),
		MessageType:    new("user"),
	}.Build(), gcp.MessageMetadata{Attributes: map[string]string{
		requestreply.ReplyURNAttribute: "urn:gram:risk:enforce:replica-test:0198f1f4-0000-7000-8000-000000000002",
	}})
	require.NoError(t, err)
	require.Zero(t, scanner.message)
}
