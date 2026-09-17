package hooks

import (
	"context"
	"strings"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// IngestOTLPLogs persists an OTLP logs export that the native /otel ingest
// already authenticated and published to the event feed. It runs the same
// attribution and telemetry_logs writers as the hooks endpoint, minus the
// event feed tee (the export is already there), so usage and cost
// attribution do not depend on which ingest edge a producer uses.
//
// The native edge is generic, so only resources from the Claude family or
// Codex are forwarded; the hooks writers would otherwise record any other
// producer as Claude usage.
func (s *Service) IngestOTLPLogs(ctx context.Context, payload *gen.LogsPayload) {
	logger := s.logger.With(
		attr.SlogHookSource("claude"),
		attr.SlogHookEvent("Logs"),
	)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		logger.ErrorContext(ctx, "OTEL logs sink called without project auth",
			attr.SlogEvent("otel_logs_sink_unauthenticated"),
		)
		return
	}
	payload = hooksSinkLogsPayload(payload)
	if payload == nil {
		return
	}
	logger = logger.With(
		attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		attr.SlogProjectID(authCtx.ProjectID.String()),
	)
	s.ingestOTLPLogs(ctx, logger, payload, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
}

// IngestOTLPMetrics is the metrics counterpart of IngestOTLPLogs.
func (s *Service) IngestOTLPMetrics(ctx context.Context, payload *gen.MetricsPayload) {
	logger := s.logger.With(
		attr.SlogHookSource("claude"),
		attr.SlogHookEvent("Metrics"),
	)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		logger.ErrorContext(ctx, "OTEL metrics sink called without project auth",
			attr.SlogEvent("otel_metrics_sink_unauthenticated"),
		)
		return
	}
	payload = hooksSinkMetricsPayload(payload)
	if payload == nil {
		return
	}
	s.ingestOTLPMetrics(ctx, logger, payload, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())
}

// hooksSinkAcceptsServiceName reports whether a resource's service.name
// identifies a producer the hooks writers know how to attribute: Codex, or
// the Claude family ("claude-code", "Claude Code", "cowork", ...). Anything
// else, including an unset name, stays event-feed only.
func hooksSinkAcceptsServiceName(name string) bool {
	if isCodexServiceName(name) {
		return true
	}
	n := strings.ToLower(strings.TrimSpace(name))
	return strings.Contains(n, "claude") || strings.Contains(n, "cowork")
}

func hooksSinkLogsPayload(payload *gen.LogsPayload) *gen.LogsPayload {
	if payload == nil {
		return nil
	}
	kept := make([]*gen.OTELResourceLog, 0, len(payload.ResourceLogs))
	for _, rl := range payload.ResourceLogs {
		if rl != nil && hooksSinkAcceptsServiceName(extractResourceAttribute(rl.Resource, "service.name")) {
			kept = append(kept, rl)
		}
	}
	if len(kept) == 0 {
		return nil
	}
	return &gen.LogsPayload{
		ResourceLogs:     kept,
		ApikeyToken:      payload.ApikeyToken,
		ProjectSlugInput: payload.ProjectSlugInput,
	}
}

func hooksSinkMetricsPayload(payload *gen.MetricsPayload) *gen.MetricsPayload {
	if payload == nil {
		return nil
	}
	kept := make([]*gen.OTELResourceMetrics, 0, len(payload.ResourceMetrics))
	for _, rm := range payload.ResourceMetrics {
		if rm != nil && hooksSinkAcceptsServiceName(extractResourceAttribute(rm.Resource, "service.name")) {
			kept = append(kept, rm)
		}
	}
	if len(kept) == 0 {
		return nil
	}
	return &gen.MetricsPayload{
		ResourceMetrics:  kept,
		ApikeyToken:      payload.ApikeyToken,
		ProjectSlugInput: payload.ProjectSlugInput,
	}
}
