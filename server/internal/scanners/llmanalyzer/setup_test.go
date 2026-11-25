package llmanalyzer_test

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"google.golang.org/protobuf/proto"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/requestreply"
	"github.com/speakeasy-api/gram/server/internal/risk/enforcereply"
	"github.com/speakeasy-api/gram/server/internal/scanners/llmanalyzer"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newReplyWriter(t *testing.T) (*miniredis.Miniredis, *redis.Client, *enforcereply.Writer) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr(), Protocol: 2})
	t.Cleanup(func() { _ = client.Close() })
	return mr, client, enforcereply.NewWriter(client)
}

func newEnforceMeterProvider(t *testing.T) (*sdkmetric.MeterProvider, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })
	return provider, reader
}

func collectMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var data metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &data))
	return data
}

// newEnforceHandler wires a handler over the stub completer, a miniredis
// reply writer and a manual-reader meter provider.
func newEnforceHandler(t *testing.T, stub *llmanalyzer.StubCompleter, opts ...llmanalyzer.EnforceHandlerOption) (*llmanalyzer.EnforceHandler, *miniredis.Miniredis, *redis.Client, *sdkmetric.ManualReader) {
	t.Helper()
	mr, client, writer := newReplyWriter(t)
	meterProvider, reader := newEnforceMeterProvider(t)
	var completer llmanalyzer.Completer
	if stub != nil {
		completer = stub
	}
	analyzer := llmanalyzer.NewAnalyzer(testenv.NewLogger(t), testenv.NewTracerProvider(t), completer)
	handler := llmanalyzer.NewEnforceHandler(testenv.NewLogger(t), testenv.NewTracerProvider(t), meterProvider, analyzer, writer, opts...)
	return handler, mr, client, reader
}

func flaggingStub(scores map[string]int, reasoning string) *llmanalyzer.StubCompleter {
	return &llmanalyzer.StubCompleter{
		Response:         llmanalyzer.VerdictJSON(scores, reasoning),
		Err:              nil,
		PromptTokens:     12,
		CompletionTokens: 4,
		Model:            "risk-judge-4b",
		Calls:            nil,
		ParseFailures:    0,
	}
}

func failingStub(err error) *llmanalyzer.StubCompleter {
	return &llmanalyzer.StubCompleter{
		Response:         "",
		Err:              err,
		PromptTokens:     0,
		CompletionTokens: 0,
		Model:            "risk-judge-4b",
		Calls:            nil,
		ParseFailures:    0,
	}
}

func userEnforcement(requestID, body string) *riskv1.LLMEnforcement {
	return riskv1.LLMEnforcement_builder{
		RequestId:        new(requestID),
		ProjectId:        new("018ffad2-1c32-7f73-8a54-85306c37a313"),
		OrganizationId:   new("org-enforce"),
		OrganizationSlug: new("acme"),
		CreatedAt:        new(time.Now().UTC().Format(time.RFC3339Nano)),
		Content:          new(body),
		Body:             new(body),
		MessageType:      new("user_message"),
		ExecutionPath:    new("realtime_streams"),
	}.Build()
}

func replyMetadata(replicaID, correlationID string, deliveryAttempt *int) gcp.MessageMetadata {
	return gcp.MessageMetadata{
		Attributes: map[string]string{
			requestreply.ReplyURNAttribute: enforcereply.ReplyURN(replicaID, correlationID),
		},
		DeliveryAttempt: deliveryAttempt,
	}
}

func popReply(t *testing.T, client *redis.Client, replicaID string) (*riskv1.EnforcementReply, []byte) {
	t.Helper()
	payload, err := client.LPop(t.Context(), enforcereply.InboxKey(replicaID)).Bytes()
	require.NoError(t, err)
	reply := new(riskv1.EnforcementReply)
	require.NoError(t, proto.Unmarshal(payload, reply))
	return reply, payload
}
