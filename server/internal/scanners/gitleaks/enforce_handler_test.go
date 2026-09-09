package gitleaks_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"google.golang.org/protobuf/proto"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/requestreply"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/enforcereply"
	"github.com/speakeasy-api/gram/server/internal/scanners/gitleaks"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestNewEnforceHandlerRejectsNilRiskRecorder(t *testing.T) {
	t.Parallel()

	_, _, writer := newReplyWriter(t)
	meterProvider, _ := newTestMeterProvider(t)
	handler, err := gitleaks.NewEnforceHandler(
		testenv.NewLogger(t),
		meterProvider,
		writer,
		func(string, []byte) (string, error) { return "fingerprint", nil },
		gitleaks.EnforceHandlerConfig{},
		nil,
	)
	require.Nil(t, handler)
	require.Error(t, err)
}

func TestEnforceHandlerWritesSafePepperedReply(t *testing.T) {
	t.Parallel()

	mr, client, writer := newReplyWriter(t)
	meterProvider, _ := newTestMeterProvider(t)
	handler, fingerprinter := newTestEnforceHandler(t, meterProvider, writer, gitleaks.DefaultMaxRequestAge)
	content := `AccessKeyId: ` + fakeAccessKeyID + `, SecretAccessKey: ` + fakeSecret
	request := riskv1.GitleaksEnforcement_builder{
		RequestId:               new("scan-safe"),
		ProjectId:               new("018ffad2-1c32-7f73-8a54-85306c37a313"),
		OrganizationId:          new("org-safe"),
		CreatedAt:               new(time.Now().UTC().Format(time.RFC3339Nano)),
		Content:                 new(content),
		OriginRiskPolicyId:      new("018ffad2-1c32-7f73-8a54-85306c37a315"),
		OriginRiskPolicyVersion: new(int64(3)),
		MessageLinkReason:       new("enforcement_test_unlinked"),
		ExecutionPath:           new("inline"),
	}.Build()
	deliveryAttempt := 2
	require.NoError(t, handler.Handle(t.Context(), request, replyMetadata("replica-safe", "scan-safe", &deliveryAttempt)))
	require.Equal(t, 60*time.Second, mr.TTL(enforcereply.InboxKey("replica-safe")))

	payload, err := client.LPop(t.Context(), enforcereply.InboxKey("replica-safe")).Bytes()
	require.NoError(t, err)
	require.NotContains(t, string(payload), fakeSecret)
	reply := new(riskv1.EnforcementReply)
	require.NoError(t, proto.Unmarshal(payload, reply))
	require.Equal(t, "scan-safe", reply.GetCorrelationId())
	require.Equal(t, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK, reply.GetStatus())
	require.Equal(t, int32(2), reply.GetDiagnostics().GetDeliveryAttempt())
	require.NotEmpty(t, reply.GetDiagnostics().GetConsumerId())
	require.NotEmpty(t, reply.GetFindings())

	rawResult, err := gitleaks.NewScanner().Scan(t.Context(), content)
	require.NoError(t, err)
	expectedFingerprints := make(map[string]string, len(rawResult.Findings))
	for _, finding := range rawResult.Findings {
		sum, _, fingerprintErr := fingerprinter.TenantedHS256("org-safe", []byte(finding.Match))
		require.NoError(t, fingerprintErr)
		expectedFingerprints[finding.RuleID] = risk.EncodeFingerprint(sum)
	}
	for _, finding := range reply.GetFindings() {
		require.NotEqual(t, fakeSecret, finding.GetMaskedPreview())
		require.NotContains(t, finding.GetMaskedPreview(), fakeSecret)
		require.Equal(t, expectedFingerprints[finding.GetRuleId()], finding.GetFingerprint())
		require.NotEmpty(t, finding.GetCategory())
		require.Equal(t, "content", finding.GetSurface())
	}
}

func TestEnforceHandlerRecordsCompletedNoPolicyScan(t *testing.T) {
	t.Parallel()

	_, _, writer := newReplyWriter(t)
	meterProvider, _ := newTestMeterProvider(t)
	meterPub, readings := capturingMeterPub(t)
	handler, err := gitleaks.NewEnforceHandler(
		testenv.NewLogger(t), meterProvider, writer,
		func(string, []byte) (string, error) { return "fingerprint", nil },
		gitleaks.EnforceHandlerConfig{},
		metering.NewRiskRecorder(testenv.NewLogger(t), meterPub),
	)
	require.NoError(t, err)
	requestID := uuid.NewString()
	request := riskv1.GitleaksEnforcement_builder{
		RequestId:               new(requestID),
		ProjectId:               new(uuid.NewString()),
		OrganizationId:          new("org-no-policy"),
		CreatedAt:               new(time.Now().UTC().Format(time.RFC3339Nano)),
		Content:                 new("safe content"),
		OriginRiskPolicyId:      nil,
		OriginRiskPolicyVersion: new(int64(0)),
		PolicyLinkReason:        new("realtime_no_matching_policy"),
		MessageLinkReason:       new("realtime_not_persisted"),
		ExecutionPath:           new("realtime_streams"),
	}.Build()

	require.NoError(t, handler.Handle(t.Context(), request, replyMetadata("replica-no-policy", requestID, nil)))
	require.Len(t, *readings, 1)
	require.Positive(t, (*readings)[0].GetValue())
	attributes := (*readings)[0].GetAttributes()
	require.Equal(t, "unlinked", attributes[metering.AttributeRiskPolicyLinkStatus])
	require.Equal(t, "realtime_no_matching_policy", attributes[metering.AttributeRiskPolicyLinkReason])
	require.NotContains(t, attributes, metering.AttributeRiskPolicyID)
	require.NotContains(t, attributes, metering.AttributeRiskPolicyVersion)
}

func TestEnforceHandlerSeparatesRequestsLinkedToOneMessage(t *testing.T) {
	t.Parallel()

	_, _, writer := newReplyWriter(t)
	meterProvider, _ := newTestMeterProvider(t)
	meterPub, readings := capturingMeterPub(t)
	handler, err := gitleaks.NewEnforceHandler(
		testenv.NewLogger(t), meterProvider, writer,
		func(string, []byte) (string, error) { return "fingerprint", nil },
		gitleaks.EnforceHandlerConfig{},
		metering.NewRiskRecorder(testenv.NewLogger(t), meterPub),
	)
	require.NoError(t, err)
	request := riskv1.GitleaksEnforcement_builder{
		ProjectId:               new(uuid.NewString()),
		OrganizationId:          new("org-realtime-identity"),
		CreatedAt:               new(time.Now().UTC().Format(time.RFC3339Nano)),
		Content:                 new("safe content"),
		OriginRiskPolicyId:      new(uuid.NewString()),
		OriginRiskPolicyVersion: new(int64(3)),
		ChatMessageId:           new(uuid.NewString()),
		ExecutionPath:           new("realtime_streams"),
	}.Build()

	for _, requestID := range []string{"first-request", "first-request", "second-request"} {
		request.SetRequestId(requestID)
		require.NoError(t, handler.Handle(t.Context(), request, replyMetadata("replica-identity", requestID, nil)))
	}
	require.Len(t, *readings, 3)
	require.Equal(t, (*readings)[0].GetId(), (*readings)[1].GetId())
	require.NotEqual(t, (*readings)[0].GetId(), (*readings)[2].GetId())
}

func TestEnforceHandlerAcknowledgesStaleRequest(t *testing.T) {
	t.Parallel()

	mr, _, writer := newReplyWriter(t)
	meterProvider, reader := newTestMeterProvider(t)
	handler, _ := newTestEnforceHandler(t, meterProvider, writer, 30*time.Second)
	request := riskv1.GitleaksEnforcement_builder{
		RequestId:      new("scan-stale"),
		ProjectId:      new("project-stale"),
		OrganizationId: new("org-stale"),
		CreatedAt:      new(time.Now().Add(-31 * time.Second).UTC().Format(time.RFC3339Nano)),
		Content:        new(fakeSecret),
	}.Build()

	require.NoError(t, handler.Handle(t.Context(), request, replyMetadata("replica-stale", "scan-stale", nil)))
	require.False(t, mr.Exists(enforcereply.InboxKey("replica-stale")))
	require.Equal(t, int64(1), counterValue(t, reader, "risk.enforcement.gitleaks.stale_dropped"))
}

func TestEnforceHandlerAcknowledgesFarFutureRequest(t *testing.T) {
	t.Parallel()

	mr, _, writer := newReplyWriter(t)
	meterProvider, reader := newTestMeterProvider(t)
	handler, _ := newTestEnforceHandler(t, meterProvider, writer, 30*time.Second)
	request := riskv1.GitleaksEnforcement_builder{
		RequestId:      new("scan-future"),
		ProjectId:      new("project-future"),
		OrganizationId: new("org-future"),
		CreatedAt:      new(time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339Nano)),
		Content:        new(fakeSecret),
	}.Build()

	require.NoError(t, handler.Handle(t.Context(), request, replyMetadata("replica-future", "scan-future", nil)))
	require.False(t, mr.Exists(enforcereply.InboxKey("replica-future")))
	require.Equal(t, int64(1), counterValue(t, reader, "risk.enforcement.gitleaks.stale_dropped"))
}

func TestEnforceHandlerAcknowledgesReplyWriteFailure(t *testing.T) {
	t.Parallel()

	_, client, writer := newReplyWriter(t)
	meterProvider, reader := newTestMeterProvider(t)
	handler, _ := newTestEnforceHandler(t, meterProvider, writer, gitleaks.DefaultMaxRequestAge)
	require.NoError(t, client.Close())
	request := riskv1.GitleaksEnforcement_builder{
		RequestId:               new("scan-write-failure"),
		ProjectId:               new("018ffad2-1c32-7f73-8a54-85306c37a313"),
		OrganizationId:          new("org-write-failure"),
		CreatedAt:               new(time.Now().UTC().Format(time.RFC3339Nano)),
		Content:                 new("safe content"),
		OriginRiskPolicyId:      new("018ffad2-1c32-7f73-8a54-85306c37a315"),
		OriginRiskPolicyVersion: new(int64(3)),
		MessageLinkReason:       new("enforcement_test_unlinked"),
		ExecutionPath:           new("inline"),
	}.Build()

	require.NoError(t, handler.Handle(t.Context(), request, replyMetadata("replica-write-failure", "scan-write-failure", nil)))
	require.Equal(t, int64(1), counterValue(t, reader, "risk.enforcement.gitleaks.reply_write_errors"))
}

func TestEnforceHandlerRejectsMalformedCreatedAt(t *testing.T) {
	t.Parallel()

	_, _, writer := newReplyWriter(t)
	meterProvider, _ := newTestMeterProvider(t)
	handler, _ := newTestEnforceHandler(t, meterProvider, writer, gitleaks.DefaultMaxRequestAge)
	request := riskv1.GitleaksEnforcement_builder{
		RequestId:      new("scan-malformed"),
		ProjectId:      new("project-malformed"),
		OrganizationId: new("org-malformed"),
		CreatedAt:      new("not-a-timestamp"),
		Content:        new("safe content"),
	}.Build()

	err := handler.Handle(t.Context(), request, replyMetadata("replica-malformed", "scan-malformed", nil))
	require.ErrorContains(t, err, "parse enforcement created_at")
}

func TestEnforceHandlerRejectsMissingReplyURNAttribute(t *testing.T) {
	t.Parallel()

	_, _, writer := newReplyWriter(t)
	meterProvider, _ := newTestMeterProvider(t)
	handler, _ := newTestEnforceHandler(t, meterProvider, writer, gitleaks.DefaultMaxRequestAge)
	request := riskv1.GitleaksEnforcement_builder{
		RequestId:      new("scan-missing-reply-urn"),
		ProjectId:      new("project-missing-reply-urn"),
		OrganizationId: new("org-missing-reply-urn"),
		CreatedAt:      new(time.Now().UTC().Format(time.RFC3339Nano)),
		Content:        new("safe content"),
	}.Build()

	err := handler.Handle(t.Context(), request, gcp.MessageMetadata{})
	require.ErrorContains(t, err, "enforcement reply urn attribute is required")
}

func replyMetadata(replicaID, correlationID string, deliveryAttempt *int) gcp.MessageMetadata {
	return gcp.MessageMetadata{
		Attributes: map[string]string{
			requestreply.ReplyURNAttribute: enforcereply.ReplyURN(replicaID, correlationID),
		},
		DeliveryAttempt: deliveryAttempt,
	}
}

func counterValue(t *testing.T, reader interface {
	Collect(context.Context, *metricdata.ResourceMetrics) error
}, name string) int64 {
	t.Helper()
	var metrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &metrics))
	for _, scope := range metrics.ScopeMetrics {
		for _, candidate := range scope.Metrics {
			if candidate.Name != name {
				continue
			}
			sum, ok := candidate.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			require.Len(t, sum.DataPoints, 1)
			return sum.DataPoints[0].Value
		}
	}
	require.Fail(t, "metric not found", name)
	return 0
}
