package gitleaks_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"google.golang.org/protobuf/proto"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/enforcereply"
	"github.com/speakeasy-api/gram/server/internal/scanners/gitleaks"
)

func TestEnforceHandlerWritesSafePepperedReply(t *testing.T) {
	t.Parallel()

	mr, client, writer := newReplyWriter(t)
	meterProvider, _ := newTestMeterProvider(t)
	handler, fingerprinter := newTestEnforceHandler(t, meterProvider, writer, gitleaks.DefaultMaxRequestAge)
	content := `AccessKeyId: ` + fakeAccessKeyID + `, SecretAccessKey: ` + fakeSecret
	request := riskv1.GitleaksEnforcement_builder{
		RequestId:      new("scan-safe"),
		ProjectId:      new("project-safe"),
		OrganizationId: new("org-safe"),
		CreatedAt:      new(time.Now().UTC().Format(time.RFC3339Nano)),
		ReplyUrn:       new(enforcereply.ReplyURN("replica-safe", "scan-safe")),
		Content:        new(content),
	}.Build()
	deliveryAttempt := 2
	require.NoError(t, handler.Handle(t.Context(), request, gcp.MessageMetadata{DeliveryAttempt: &deliveryAttempt}))
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

	rawFindings, err := gitleaks.NewScanner().Scan(t.Context(), content)
	require.NoError(t, err)
	expectedFingerprints := make(map[string]string, len(rawFindings))
	for _, finding := range rawFindings {
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
		ReplyUrn:       new(enforcereply.ReplyURN("replica-stale", "scan-stale")),
		Content:        new(fakeSecret),
	}.Build()

	require.NoError(t, handler.Handle(t.Context(), request, gcp.MessageMetadata{DeliveryAttempt: nil}))
	require.False(t, mr.Exists(enforcereply.InboxKey("replica-stale")))
	require.Equal(t, int64(1), counterValue(t, reader, "risk.enforcement.gitleaks.stale_dropped"))
}

func TestEnforceHandlerAcknowledgesReplyWriteFailure(t *testing.T) {
	t.Parallel()

	_, client, writer := newReplyWriter(t)
	meterProvider, reader := newTestMeterProvider(t)
	handler, _ := newTestEnforceHandler(t, meterProvider, writer, gitleaks.DefaultMaxRequestAge)
	require.NoError(t, client.Close())
	request := riskv1.GitleaksEnforcement_builder{
		RequestId:      new("scan-write-failure"),
		ProjectId:      new("project-write-failure"),
		OrganizationId: new("org-write-failure"),
		CreatedAt:      new(time.Now().UTC().Format(time.RFC3339Nano)),
		ReplyUrn:       new(enforcereply.ReplyURN("replica-write-failure", "scan-write-failure")),
		Content:        new("safe content"),
	}.Build()

	require.NoError(t, handler.Handle(t.Context(), request, gcp.MessageMetadata{DeliveryAttempt: nil}))
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
		ReplyUrn:       new(enforcereply.ReplyURN("replica-malformed", "scan-malformed")),
		Content:        new("safe content"),
	}.Build()

	err := handler.Handle(t.Context(), request, gcp.MessageMetadata{DeliveryAttempt: nil})
	require.ErrorContains(t, err, "parse enforcement created_at")
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
