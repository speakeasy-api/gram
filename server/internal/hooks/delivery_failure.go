package hooks

import (
	"context"
	"strings"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

// eventTypeClientDeliveryFailed is the canonical event type a hook sender
// uses to report that an earlier event could not reach the control plane. It
// carries data.delivery_failure and nothing else: no session, tool, or prompt
// content, so it is never agent activity.
const eventTypeClientDeliveryFailed = "client.delivery_failed"

func isDeliveryFailureReport(payload *gen.IngestPayload) bool {
	return payload != nil && payload.Event != nil && strings.TrimSpace(payload.Event.Type) == eventTypeClientDeliveryFailed
}

func canonicalDeliveryFailure(payload *gen.IngestPayload) *gen.HookDeliveryFailureData {
	if payload != nil && payload.Data != nil {
		return payload.Data.DeliveryFailure
	}
	return nil
}

// validateDeliveryFailurePayload pairs the report block with its event type: a
// report without the block has nothing to record, and the block on any other
// event would let an ordinary hook row carry diagnostic fields.
func validateDeliveryFailurePayload(payload *gen.IngestPayload) error {
	report := canonicalDeliveryFailure(payload)
	switch {
	case isDeliveryFailureReport(payload) && report == nil:
		return oops.E(oops.CodeInvalid, nil, "data.delivery_failure is required for client.delivery_failed events")
	case !isDeliveryFailureReport(payload) && report != nil:
		return oops.E(oops.CodeInvalid, nil, "data.delivery_failure is only valid on client.delivery_failed events")
	default:
		return nil
	}
}

// deliveryFailureTime resolves the row timestamp for a report: the envelope's
// occurred_at, else the report's own attempted_at, under the same clamp every
// other ingested event gets. Both are device-side times, which is the point —
// the report arrives on a later drain and its arrival says nothing about when
// the failure happened.
func deliveryFailureTime(payload *gen.IngestPayload, now time.Time) time.Time {
	raw := ""
	if payload != nil && payload.Event != nil {
		raw = strings.TrimSpace(conv.PtrValOr(payload.Event.OccurredAt, ""))
	}
	if raw == "" {
		if report := canonicalDeliveryFailure(payload); report != nil {
			raw = strings.TrimSpace(report.AttemptedAt)
		}
	}
	return clampEventTime(raw, now)
}

// recordDeliveryFailure sinks one device-side delivery failure report: a
// counter for alerting and a diagnostic telemetry row for investigation. The
// row is stamped with the device-side attempt time and with the device
// attributes the report carries, not the request headers, which describe the
// drain run that delivered it. Nothing here touches enforcement, chat, skill,
// or MCP state.
func (s *Service) recordDeliveryFailure(ctx context.Context, payload *gen.IngestPayload, authCtx *contextvalues.AuthContext, actor canonicalActor, replayed bool) {
	ctx, span := s.tracer.Start(ctx, "hooks.recordDeliveryFailure")
	defer span.End()

	report := canonicalDeliveryFailure(payload)
	if report == nil {
		return
	}
	adapter := strings.TrimSpace(payload.Source.Adapter)
	eventType := strings.TrimSpace(report.EventType)
	phase := strings.TrimSpace(string(report.DeliveryPhase))
	cause := strings.TrimSpace(string(report.Cause))
	harness := strings.TrimSpace(conv.PtrValOr(report.Harness, ""))
	deviceOS := strings.TrimSpace(conv.PtrValOr(report.Os, ""))
	arch := strings.TrimSpace(conv.PtrValOr(report.Arch, ""))
	binaryVersion := strings.TrimSpace(conv.PtrValOr(report.BinaryVersion, ""))
	statusCode := conv.PtrValOr(report.StatusCode, 0)
	elapsedMs := conv.PtrValOr(report.ElapsedMs, 0)
	attempts := conv.PtrValOr(report.Attempts, 0)
	spoolDepth := conv.PtrValOr(report.SpoolDepth, 0)

	s.metrics.RecordDeliveryFailure(ctx, deliveryFailureMetricParams{
		EventType:     eventType,
		Phase:         phase,
		Cause:         cause,
		Harness:       harness,
		OS:            deviceOS,
		BinaryVersion: binaryVersion,
		OrgSlug:       authCtx.OrganizationSlug,
	})

	// INFO rather than the rare-info default: this is the only server-side
	// trace of a device-side outage until the row lands in ClickHouse, and
	// volume is bounded by the device's own spool caps.
	s.logger.InfoContext(ctx, "hook delivery failure reported",
		attr.SlogEvent("hooks_delivery_failure"),
		attr.SlogHookSource(adapter),
		attr.SlogHookDeliveryEventType(eventType),
		attr.SlogHookDeliveryPhase(phase),
		attr.SlogHookDeliveryCause(cause),
		attr.SlogHookDeliveryStatusCode(statusCode),
		attr.SlogHookDeliveryElapsedMs(elapsedMs),
		attr.SlogHookDeliveryAttempts(attempts),
		attr.SlogHookDeliverySpoolDepth(spoolDepth),
		attr.SlogHookDeviceHarness(harness),
		attr.SlogHookDeviceOS(deviceOS),
		attr.SlogHookDeviceArch(arch),
		attr.SlogHookDeviceBinaryVersion(binaryVersion),
		attr.SlogHookReplayed(replayed),
		attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		attr.SlogProjectID(authCtx.ProjectID.String()),
	)

	if s.telemetryLogger == nil {
		return
	}
	attrs := map[attr.Key]any{
		attr.EventSourceKey:             string(telemetry.EventSourceHookDiagnostic),
		attr.HookEventKey:               eventTypeClientDeliveryFailed,
		attr.HookSourceKey:              adapter,
		attr.ProjectIDKey:               authCtx.ProjectID.String(),
		attr.OrganizationIDKey:          authCtx.ActiveOrganizationID,
		attr.SpanIDKey:                  generateSpanID(),
		attr.TraceIDKey:                 generateTraceID(),
		attr.LogBodyKey:                 "Hook delivery failure: " + eventType + " (" + cause + ")",
		attr.HookDeliveryEventTypeKey:   eventType,
		attr.HookDeliveryPhaseKey:       phase,
		attr.HookDeliveryCauseKey:       cause,
		attr.HookDeliveryStatusCodeKey:  statusCode,
		attr.HookDeliveryElapsedMsKey:   elapsedMs,
		attr.HookDeliveryAttemptsKey:    attempts,
		attr.HookDeliveryGatingKey:      conv.PtrValOr(report.Gating, false),
		attr.HookDeliveryAttemptedAtKey: strings.TrimSpace(report.AttemptedAt),
		attr.HookDeliverySpoolDepthKey:  spoolDepth,
	}
	if replayed {
		attrs[attr.HookReplayedKey] = true
	}
	optional := map[attr.Key]string{
		attr.HookHostnameKey:                     strings.TrimSpace(conv.PtrValOr(payload.Source.Hostname, "")),
		attr.HookDeliveryFailedIdempotencyKeyKey: strings.TrimSpace(conv.PtrValOr(report.FailedIdempotencyKey, "")),
		attr.HookDeliverySessionIDKey:            strings.TrimSpace(conv.PtrValOr(report.SessionID, "")),
		attr.HookDeviceOSKey:                     deviceOS,
		attr.HookDeviceArchKey:                   arch,
		attr.HookDeviceBinaryVersionKey:          binaryVersion,
		attr.HookDeviceHarnessKey:                harness,
		attr.HookDeviceHarnessVariantKey:         strings.TrimSpace(conv.PtrValOr(report.HarnessVariant, "")),
		attr.HookDeviceHarnessVersionKey:         strings.TrimSpace(conv.PtrValOr(report.HarnessVersion, "")),
	}
	for key, value := range optional {
		if value != "" {
			attrs[key] = value
		}
	}

	s.telemetryLogger.Log(ctx, telemetry.LogParams{
		Timestamp: deliveryFailureTime(payload, s.now()),
		ToolInfo: telemetry.ToolInfo{
			Name:           eventTypeClientDeliveryFailed,
			OrganizationID: authCtx.ActiveOrganizationID,
			ProjectID:      authCtx.ProjectID.String(),
			ID:             "",
			URN:            "",
			DeploymentID:   "",
			FunctionID:     nil,
		},
		UserInfo:   telemetry.UserInfoByIDAndEmail(actor.UserID, actor.Email),
		Attributes: withAgentActor(ctx, attrs),
	})
}
