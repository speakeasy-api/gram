package registration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oauth/registration"
)

type logContextKey struct{}

type contextCaptureHandler struct {
	slog.Handler
	want any
	seen *bool
}

func (h *contextCaptureHandler) Handle(ctx context.Context, record slog.Record) error {
	*h.seen = ctx.Value(logContextKey{}) == h.want
	if err := h.Handler.Handle(ctx, record); err != nil {
		return fmt.Errorf("handle captured log record: %w", err)
	}
	return nil
}

func (h *contextCaptureHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextCaptureHandler{Handler: h.Handler.WithAttrs(attrs), want: h.want, seen: h.seen}
}

func (h *contextCaptureHandler) WithGroup(name string) slog.Handler {
	return &contextCaptureHandler{Handler: h.Handler.WithGroup(name), want: h.want, seen: h.seen}
}

func TestMetricsRecordStableBoundedAttributes(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	var logs bytes.Buffer
	contextSeen := false
	marker := new(int)
	logger := slog.New(&contextCaptureHandler{
		Handler: slog.NewJSONHandler(&logs, nil),
		want:    marker,
		seen:    &contextSeen,
	})
	metrics := registration.NewMetrics(logger, provider)
	status := 429
	providerMessage := "  provider\nmessage  " + strings.Repeat("x", registration.ProviderMessageMaxRunes)
	ctx := context.WithValue(t.Context(), logContextKey{}, marker)
	metrics.RecordFailure(ctx, registration.MethodDCR, registration.Failure{
		Outcome:         registration.OutcomeUnreachable,
		Reason:          registration.ReasonRateLimited,
		Retryable:       true,
		HTTPStatus:      &status,
		ProviderMessage: &providerMessage,
	})

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	require.Len(t, rm.ScopeMetrics, 1)
	require.Len(t, rm.ScopeMetrics[0].Metrics, 1)
	got := rm.ScopeMetrics[0].Metrics[0]
	require.Equal(t, "gram.oauth.client_registration.failures", got.Name)
	sum, ok := got.Data.(metricdata.Sum[int64])
	require.True(t, ok)
	require.Len(t, sum.DataPoints, 1)
	require.Equal(t, attribute.NewSet(
		attr.OAuthRegistrationMethod(registration.MethodDCR),
		attr.OAuthRegistrationOutcome(registration.OutcomeUnreachable),
		attr.OAuthRegistrationReason(registration.ReasonRateLimited),
		attr.OAuthRegistrationRetryable(true),
		attr.HTTPResponseStatusCode(status),
	), sum.DataPoints[0].Attributes)

	require.True(t, contextSeen, "the event must retain the request context for trace correlation")
	var event map[string]any
	require.NoError(t, json.Unmarshal(logs.Bytes(), &event))
	require.Equal(t, "oauth client registration failed", event["msg"])
	require.Equal(t, "oauth.client_registration.failure", event["event"])
	require.Equal(t, "dcr", event["gram.oauth.registration_method"])
	require.Equal(t, "unreachable", event["gram.oauth.registration_outcome"])
	require.Equal(t, "rate_limited", event["gram.oauth.registration_reason"])
	require.Equal(t, true, event["gram.oauth.registration_retryable"])
	require.EqualValues(t, status, event["http.response.status_code"])
	require.Equal(t, registration.SanitizeProviderMessage(providerMessage), event["gram.oauth.error_description"])
}

func TestMetricsRejectUnboundedValues(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	var logs bytes.Buffer
	metrics := registration.NewMetrics(slog.New(slog.NewJSONHandler(&logs, nil)), provider)
	metrics.RecordFailure(t.Context(), registration.Method("provider-controlled"), registration.Failure{
		Outcome:         registration.Outcome("new-outcome"),
		Reason:          registration.Reason("provider-message"),
		Retryable:       false,
		HTTPStatus:      nil,
		ProviderMessage: nil,
	})

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	require.Empty(t, rm.ScopeMetrics)
	require.Empty(t, logs.String())
}
