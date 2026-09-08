package dialect

import (
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/stretchr/testify/require"
)

func TestForMetricReturnsNilDialectForNilMetric(t *testing.T) {
	t.Parallel()

	require.IsType(t, NilMetric{}, ForMetric(nil))
}

func TestForMetricUsesSemanticConventionsByDefault(t *testing.T) {
	t.Parallel()

	dialect := ForMetric((&otelv1.InboundMetric_builder{Name: new("gen_ai.client.token.usage")}).Build())
	point := metricDialectPoint(
		metricDialectStringAttribute("gen_ai.conversation.id", "conversation-id"),
		metricDialectStringAttribute("user.id", "user-id"),
		metricDialectStringAttribute("user.email", "user@example.invalid"),
		metricDialectStringAttribute("gen_ai.response.id", "response-id"),
	)

	key, value, err := dialect.SessionID(point)
	require.NoError(t, err)
	require.Equal(t, "gen_ai.conversation.id", key)
	require.Equal(t, "conversation-id", value)

	key, value, err = dialect.ExternalUserID(point)
	require.NoError(t, err)
	require.Equal(t, "user.id", key)
	require.Equal(t, "user-id", value)

	key, value, err = dialect.ExternalUserEmail(point)
	require.NoError(t, err)
	require.Equal(t, "user.email", key)
	require.Equal(t, "user@example.invalid", value)

	key, value, err = dialect.ResponseID(point)
	require.NoError(t, err)
	require.Equal(t, "gen_ai.response.id", key)
	require.Equal(t, "response-id", value)
}

func TestForMetricUsesClaudeCodeWithSemanticFallback(t *testing.T) {
	t.Parallel()

	dialect := ForMetric((&otelv1.InboundMetric_builder{Name: new("claude_code.token.usage")}).Build())
	point := metricDialectPoint(
		metricDialectStringAttribute("session.id", "session-id"),
		metricDialectStringAttribute("user.id", "semconv-user-id"),
	)

	key, value, err := dialect.SessionID(point)
	require.NoError(t, err)
	require.Equal(t, "session.id", key)
	require.Equal(t, "session-id", value)

	key, value, err = dialect.ExternalUserID(point)
	require.NoError(t, err)
	require.Equal(t, "user.id", key)
	require.Equal(t, "semconv-user-id", value)
}

func TestForMetricFallsBackWhenClaudeIdentifierIsEmpty(t *testing.T) {
	t.Parallel()

	dialect := ForMetric((&otelv1.InboundMetric_builder{Name: new("claude_code.token.usage")}).Build())
	point := metricDialectPoint(
		metricDialectStringAttribute("session.id", ""),
		metricDialectStringAttribute("gen_ai.conversation.id", "conversation-id"),
	)

	key, value, err := dialect.SessionID(point)

	require.NoError(t, err)
	require.Equal(t, "gen_ai.conversation.id", key)
	require.Equal(t, "conversation-id", value)
}

func TestForMetricUsesCodexResourceFamily(t *testing.T) {
	t.Parallel()

	metric := (&otelv1.InboundMetric_builder{
		Name: new("codex.tool.call"),
		Resource: (&otelv1.InboundMetric_Resource_builder{
			Attributes: []*otelv1.InboundMetric_KeyValue{
				metricDialectStringAttribute("service.name", "codex_exec"),
			},
		}).Build(),
	}).Build()
	dialect := ForMetric(metric)
	point := metricDialectPoint(
		metricDialectStringAttribute("conversation.id", "conversation-id"),
		metricDialectStringAttribute("user.account_id", "account-id"),
	)

	key, value, err := dialect.SessionID(point)
	require.NoError(t, err)
	require.Equal(t, "conversation.id", key)
	require.Equal(t, "conversation-id", value)

	key, value, err = dialect.ExternalUserID(point)
	require.NoError(t, err)
	require.Equal(t, "user.account_id", key)
	require.Equal(t, "account-id", value)
}

func TestCodexMetricRejectsUnrelatedServicePrefix(t *testing.T) {
	t.Parallel()

	metric := (&otelv1.InboundMetric_builder{
		Resource: (&otelv1.InboundMetric_Resource_builder{
			Attributes: []*otelv1.InboundMetric_KeyValue{
				metricDialectStringAttribute("service.name", "codexish-tool"),
			},
		}).Build(),
	}).Build()

	require.False(t, (CodexMetric{}).AppliesTo(metric))
}

func metricDialectPoint(attributes ...*otelv1.InboundMetric_KeyValue) *otelv1.InboundMetric_NumberDataPoint {
	return (&otelv1.InboundMetric_NumberDataPoint_builder{Attributes: attributes}).Build()
}

func metricDialectStringAttribute(key, value string) *otelv1.InboundMetric_KeyValue {
	return (&otelv1.InboundMetric_KeyValue_builder{
		Key:   &key,
		Value: (&otelv1.InboundMetric_AnyValue_builder{StringValue: &value}).Build(),
	}).Build()
}

var _ MetricDataPoint = (*otelv1.InboundMetric_NumberDataPoint)(nil)
var _ MetricDataPoint = (*otelv1.InboundMetric_HistogramDataPoint)(nil)
var _ MetricDataPoint = (*otelv1.InboundMetric_ExponentialHistogramDataPoint)(nil)
var _ MetricDataPoint = (*otelv1.InboundMetric_SummaryDataPoint)(nil)
