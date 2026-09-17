package hooks

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// The native /otel ingest has already published the export to the event feed,
// so the sink must persist the hooks telemetry rows without teeing again.
func TestIngestOTLPLogs_PersistsClaudeRowsWithoutEventFeedTee(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)
	publisher := gcp.NewMockPublisher[*otelv1.InboundLogRecord]()
	ti.service.otelLogPublisher = publisher

	sessionID := "claude-sink-session-1"
	timestamp := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)

	ti.service.IngestOTLPLogs(ctx, claudeLogsPayload(
		[]*gen.OTELResourceAttribute{resourceStrAttr("service.name", "claude-code")},
		&gen.OTELScope{Name: new("claude-code"), Version: new("1.0.0")},
		&gen.OTELLogRecord{
			TimeUnixNano: new(nanoString(timestamp)),
			Body:         &gen.OTELLogBody{StringValue: new("claude_code.api_request")},
			Attributes: []*gen.OTELAttribute{
				strAttr("session.id", sessionID),
				strAttr("user.email", "dev@example.com"),
				strAttr("prompt.id", "prompt-1"),
				strAttr("event.name", "api_request"),
				strAttr("model", "claude-opus-4-8"),
			},
		},
	))

	logs := waitForHookLogs(t, ctx, chClient, authCtx.ProjectID.String(), claudeOTELLogsURN, timestamp, 1)
	row := logs[0]
	require.Equal(t, timestamp.UnixNano(), row.TimeUnixNano)
	require.Equal(t, "claude_code.api_request", row.Body)
	require.NotNil(t, row.GramChatID)
	require.Equal(t, sessionID, *row.GramChatID)
	require.Contains(t, row.Attributes, "prompt-1")
	// dev@example.com is a member of the test org, so the row is attributed.
	require.Contains(t, row.Attributes, `"account_type":"team"`)

	publisher.AssertNotCalled(t, "Publish")
}

func TestIngestOTLPLogs_RequiresProjectAuth(t *testing.T) {
	t.Parallel()

	// A bare service: any attempt to attribute or write would dereference a
	// nil dependency, so returning cleanly proves the sink stopped at auth.
	s := &Service{logger: testenv.NewLogger(t)}
	s.IngestOTLPLogs(t.Context(), claudeLogsPayload(
		[]*gen.OTELResourceAttribute{resourceStrAttr("service.name", "claude-code")},
		&gen.OTELScope{Name: new("claude-code"), Version: new("1.0.0")},
		&gen.OTELLogRecord{
			TimeUnixNano: new(nanoString(time.Now())),
			Body:         &gen.OTELLogBody{StringValue: new("claude_code.api_request")},
			Attributes:   []*gen.OTELAttribute{strAttr("session.id", "claude-sink-unauth")},
		},
	))
	s.IngestOTLPMetrics(t.Context(), codexMetricsPayload())
}

func TestHooksSinkAcceptsServiceName(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"claude-code", "Claude Code", "claudecode", "cowork", "claude-code-desktop", "codex", "codex_cli_rs", "codex-app-server"} {
		require.True(t, hooksSinkAcceptsServiceName(name), name)
	}
	for _, name := range []string{"", "producer", "cursor", "litellm", "my-gateway"} {
		require.False(t, hooksSinkAcceptsServiceName(name), name)
	}
}

func TestHooksSinkLogsPayloadDropsUnknownProducers(t *testing.T) {
	t.Parallel()

	record := &gen.OTELLogRecord{
		TimeUnixNano: new(nanoString(time.Now())),
		Body:         &gen.OTELLogBody{StringValue: new("hello")},
		Attributes:   []*gen.OTELAttribute{strAttr("session.id", "s")},
	}
	unknown := claudeLogsPayload([]*gen.OTELResourceAttribute{resourceStrAttr("service.name", "producer")}, nil, record)
	require.Nil(t, hooksSinkLogsPayload(unknown))
	require.Nil(t, hooksSinkLogsPayload(claudeLogsPayload(nil, nil, record)), "unset service.name stays event-feed only")

	mixed := &gen.LogsPayload{ResourceLogs: append(
		unknown.ResourceLogs,
		claudeLogsPayload([]*gen.OTELResourceAttribute{resourceStrAttr("service.name", "Claude Code")}, nil, record).ResourceLogs...,
	)}
	kept := hooksSinkLogsPayload(mixed)
	require.NotNil(t, kept)
	require.Len(t, kept.ResourceLogs, 1)
	require.Equal(t, "Claude Code", extractResourceAttribute(kept.ResourceLogs[0].Resource, "service.name"))
}

// The shapes protojson produces for a Claude Code metrics export: enum-name
// temporality and string-encoded integers.
func TestIngestOTLPMetrics_PersistsClaudeUsageFromProtoJSONShapes(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)
	now := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)

	name := "claude_code.token.usage"
	ti.service.IngestOTLPMetrics(ctx, &gen.MetricsPayload{
		ResourceMetrics: []*gen.OTELResourceMetrics{{
			Resource: &gen.OTELResource{Attributes: []*gen.OTELResourceAttribute{resourceStrAttr("service.name", "Claude Code")}},
			ScopeMetrics: []*gen.OTELScopeMetrics{{
				Metrics: []*gen.OTELMetric{{
					Name: &name,
					Sum: &gen.OTELSum{
						AggregationTemporality: "AGGREGATION_TEMPORALITY_DELTA",
						DataPoints: []*gen.OTELNumberDataPoint{{
							TimeUnixNano: new(nanoString(now)),
							AsInt:        "100",
							Attributes: []*gen.OTELAttribute{
								strAttr("session.id", "claude-sink-metrics-session"),
								strAttr("model", "claude-opus-4-8"),
								strAttr("type", "input"),
								strAttr("user.email", "dev@example.com"),
							},
						}},
					},
				}},
			}},
		}},
	})

	logs := waitForHookLogs(t, ctx, chClient, authCtx.ProjectID.String(), "claude-code:usage:metrics", now, 1)
	require.Contains(t, logs[0].Attributes, providerAnthropic)
	require.NotNil(t, logs[0].GramChatID)
	require.Equal(t, "claude-sink-metrics-session", *logs[0].GramChatID)
}

func TestIngestOTLPMetrics_PersistsCodexMetricRows(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)
	timestamp := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)

	toolCalls := "codex.tool.call"
	unit := "1"
	ti.service.IngestOTLPMetrics(ctx, codexMetricsPayload(&gen.OTELMetric{
		Name: &toolCalls,
		Unit: &unit,
		Sum: &gen.OTELSum{
			AggregationTemporality: "AGGREGATION_TEMPORALITY_CUMULATIVE",
			DataPoints: []*gen.OTELNumberDataPoint{{
				TimeUnixNano: new(nanoString(timestamp)),
				AsInt:        "3",
				Attributes: []*gen.OTELAttribute{
					strAttr("tool_name", "shell"),
					strAttr("conversation.id", "conv-sink-metrics-1"),
				},
			}},
		},
	}))

	logs := waitForHookLogs(t, ctx, chClient, authCtx.ProjectID.String(), codexOTELMetricsURN, timestamp, 1)
	require.Equal(t, "codex.tool.call", logs[0].Body)
	require.NotNil(t, logs[0].GramChatID)
	require.Equal(t, "conv-sink-metrics-1", *logs[0].GramChatID)
}
