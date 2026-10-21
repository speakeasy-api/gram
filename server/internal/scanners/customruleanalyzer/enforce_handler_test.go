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
}

func (s *captureCustomRulesScanner) Scan(_ context.Context, request ScanRequest) (scanners.Result, error) {
	s.request = request
	return scanners.Result{Completed: true}, nil
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
