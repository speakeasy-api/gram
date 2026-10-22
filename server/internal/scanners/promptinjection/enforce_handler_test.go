package promptinjection

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

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

type capturePromptInjectionScanner struct {
	content string
	message judgemessage.Message
}

func (s *capturePromptInjectionScanner) ScanWithVerdict(_ context.Context, content, _, _, _ string, message judgemessage.Message, _ ...judgemessage.Trajectory) (scanners.Result, Result, error) {
	s.content = content
	s.message = message
	return scanners.Result{Completed: true}, Result{}, nil
}

func enforceCounterValue(t *testing.T, reader *sdkmetric.ManualReader, name string) int64 {
	t.Helper()
	var data metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &data))
	for _, scope := range data.ScopeMetrics {
		for _, candidate := range scope.Metrics {
			if candidate.Name != name {
				continue
			}
			sum, ok := candidate.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			var total int64
			for _, point := range sum.DataPoints {
				total += point.Value
			}
			return total
		}
	}
	return 0
}

func TestEnforceHandlerScansBoundedPrefixAndMarksTruncated(t *testing.T) {
	t.Parallel()

	scanner := &capturePromptInjectionScanner{}
	writer := &captureEnforcementReply{}
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, meterProvider.Shutdown(t.Context())) })
	handler, err := NewEnforceHandler(
		testenv.NewLogger(t),
		meterProvider,
		scanner,
		writer,
		metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()),
		EnforceHandlerConfig{MaxContentBytes: 4},
	)
	require.NoError(t, err)

	correlationID := "0198f1f4-0000-7000-8000-000000000000"
	err = handler.Handle(t.Context(), riskv1.PromptInjectionEnforcement_builder{
		CreatedAt:      new(time.Now().UTC().Format(time.RFC3339Nano)),
		ChatMessageId:  new("0198f1f4-0000-7000-8000-000000000001"),
		ChatId:         new("0198f1f4-0000-7000-8000-000000000002"),
		OrganizationId: new("org-test"),
		ProjectId:      new("project-test"),
		Content:        new("abcé"),
		Body:           new("body"),
		MessageType:    new("user"),
	}.Build(), gcp.MessageMetadata{Attributes: map[string]string{
		requestreply.ReplyURNAttribute: "urn:gram:risk:enforce:replica-test:" + correlationID,
	}})
	require.NoError(t, err)
	require.Equal(t, "abc", scanner.content)
	require.Equal(t, "body", scanner.message.Body)
	require.Equal(t, uuid.MustParse("0198f1f4-0000-7000-8000-000000000001"), scanner.message.AnchorID)
	require.Equal(t, uuid.MustParse("0198f1f4-0000-7000-8000-000000000002"), scanner.message.ChatID)
	require.NotNil(t, writer.reply)
	require.Equal(t, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK, writer.reply.GetStatus())
	require.True(t, writer.reply.GetContentTruncated())
	require.False(t, writer.reply.GetIncomplete())
	require.Equal(t, int64(1), enforceCounterValue(t, reader, "risk.enforcement.engine_truncations"))
}

func TestEnforceHandlerPreservesSingleToolRequestShape(t *testing.T) {
	t.Parallel()

	scanner := &capturePromptInjectionScanner{}
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

	err = handler.Handle(t.Context(), riskv1.PromptInjectionEnforcement_builder{
		CreatedAt:      new(time.Now().UTC().Format(time.RFC3339Nano)),
		OrganizationId: new("org-test"),
		ProjectId:      new("project-test"),
		Content:        new(`{"command":"rm -rf /"}`),
		MessageType:    new("tool_request"),
		ToolCalls: []*riskv1.PromptInjectionEnforcement_ToolCall{
			riskv1.PromptInjectionEnforcement_ToolCall_builder{
				Name:      new("Bash"),
				Arguments: new(`{"command":"rm -rf /"}`),
			}.Build(),
		},
	}.Build(), gcp.MessageMetadata{Attributes: map[string]string{
		requestreply.ReplyURNAttribute: "urn:gram:risk:enforce:replica-test:0198f1f4-0000-7000-8000-000000000003",
	}})
	require.NoError(t, err)
	require.Equal(t, message.ToolRequest, scanner.message.Type)
	require.Equal(t, "Bash", scanner.message.ToolName)
	require.JSONEq(t, `{"command":"rm -rf /"}`, scanner.message.Body)
	require.Empty(t, scanner.message.ToolCalls)
}

func TestEnforceHandlerDropsRequestsOlderThanLaneWait(t *testing.T) {
	t.Parallel()

	scanner := &capturePromptInjectionScanner{}
	handler, err := NewEnforceHandler(
		testenv.NewLogger(t),
		testenv.NewMeterProvider(t),
		scanner,
		&captureEnforcementReply{},
		metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()),
		EnforceHandlerConfig{},
	)
	require.NoError(t, err)

	err = handler.Handle(t.Context(), riskv1.PromptInjectionEnforcement_builder{
		CreatedAt:      new(time.Now().Add(-61 * time.Second).UTC().Format(time.RFC3339Nano)),
		OrganizationId: new("org-test"),
		ProjectId:      new("project-test"),
		Content:        new("must not be scanned"),
		MessageType:    new("user"),
	}.Build(), gcp.MessageMetadata{Attributes: map[string]string{
		requestreply.ReplyURNAttribute: "urn:gram:risk:enforce:replica-test:0198f1f4-0000-7000-8000-000000000004",
	}})
	require.NoError(t, err)
	require.Empty(t, scanner.content)
}
