package customruleanalyzer

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
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

type captureCustomRulesScanner struct {
	request ScanRequest
	skip    bool
}

func (s *captureCustomRulesScanner) ScanForEnforcement(_ context.Context, request ScanRequest) (scanners.Result, bool, error) {
	s.request = request
	if s.skip {
		return scanners.Result{}, false, nil
	}
	return scanners.Result{Completed: true}, true, nil
}

func TestEnforceHandlerSharesEngineBudgetAcrossContentAndArguments(t *testing.T) {
	t.Parallel()

	scanner := &captureCustomRulesScanner{}
	writer := &captureEnforcementReply{}
	handler, err := NewEnforceHandler(
		testenv.NewLogger(t),
		testenv.NewMeterProvider(t),
		scanner,
		writer,
		metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()),
		EnforceHandlerConfig{MaxContentBytes: 5},
	)
	require.NoError(t, err)

	correlationID := "0198f1f4-0000-7000-8000-000000000000"
	err = handler.Handle(t.Context(), riskv1.CustomRulesEnforcement_builder{
		CreatedAt:      new(time.Now().UTC().Format(time.RFC3339Nano)),
		OrganizationId: new("org-test"),
		ProjectId:      new("0198f1f4-0000-7000-8000-000000000001"),
		Content:        new("abcd"),
		MessageType:    new("tool_request"),
		ToolCalls: []*riskv1.CustomRulesEnforcement_ToolCall{
			riskv1.CustomRulesEnforcement_ToolCall_builder{Name: new("run"), Arguments: new("éx")}.Build(),
		},
	}.Build(), gcp.MessageMetadata{Attributes: map[string]string{
		requestreply.ReplyURNAttribute: "urn:gram:risk:enforce:replica-test:" + correlationID,
	}})
	require.NoError(t, err)
	require.Equal(t, "abcd", scanner.request.Content)
	require.Len(t, scanner.request.ToolCalls, 1)
	require.Empty(t, scanner.request.ToolCalls[0].Arguments)
	require.NotNil(t, writer.reply)
	require.True(t, writer.reply.GetContentTruncated())
	require.False(t, writer.reply.GetIncomplete())
}

func TestEnforceHandlerDoesNotReportSkippedRulesAsIncomplete(t *testing.T) {
	t.Parallel()

	scanner := &captureCustomRulesScanner{skip: true}
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

	err = handler.Handle(t.Context(), riskv1.CustomRulesEnforcement_builder{
		CreatedAt:      new(time.Now().UTC().Format(time.RFC3339Nano)),
		OrganizationId: new("org-test"),
		ProjectId:      new("0198f1f4-0000-7000-8000-000000000001"),
		Content:        new("nothing to evaluate"),
		MessageType:    new("user"),
	}.Build(), gcp.MessageMetadata{Attributes: map[string]string{
		requestreply.ReplyURNAttribute: "urn:gram:risk:enforce:replica-test:0198f1f4-0000-7000-8000-000000000002",
	}})
	require.NoError(t, err)
	require.NotNil(t, writer.reply)
	require.False(t, writer.reply.GetIncomplete())
}

func TestEnforceHandlerDropsRequestsOlderThanLaneWait(t *testing.T) {
	t.Parallel()

	scanner := &captureCustomRulesScanner{}
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

	err = handler.Handle(t.Context(), riskv1.CustomRulesEnforcement_builder{
		CreatedAt:      new(time.Now().Add(-6 * time.Second).UTC().Format(time.RFC3339Nano)),
		OrganizationId: new("org-test"),
		ProjectId:      new("0198f1f4-0000-7000-8000-000000000001"),
		Content:        new("must not be scanned"),
		MessageType:    new("user"),
	}.Build(), gcp.MessageMetadata{Attributes: map[string]string{
		requestreply.ReplyURNAttribute: "urn:gram:risk:enforce:replica-test:0198f1f4-0000-7000-8000-000000000002",
	}})
	require.NoError(t, err)
	require.Zero(t, scanner.request)
	require.Nil(t, writer.reply)
}
