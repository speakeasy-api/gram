package hooks

import (
	"context"
	"strings"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hookevents/vocab"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

// eventTypeClientInstallFailed is the event type a plugin's bootstrap script
// sends when it could not install the hooks binary. It carries
// data.install_failure and nothing else, so it is never agent activity.
const eventTypeClientInstallFailed = vocab.EventTypeClientInstallFailed

var installFailureCauses = vocab.Set(vocab.InstallFailureCauses)

func isInstallFailureReport(payload *gen.IngestPayload) bool {
	return payload != nil && payload.Event != nil && strings.TrimSpace(payload.Event.Type) == eventTypeClientInstallFailed
}

func canonicalInstallFailure(payload *gen.IngestPayload) *gen.HookInstallFailureData {
	if payload != nil && payload.Data != nil {
		return payload.Data.InstallFailure
	}
	return nil
}

// validateInstallFailurePayload pairs the report block with its event type,
// the same way validateDeliveryFailurePayload does, which also rejects a
// delivery_failure block here. No idempotency key is
// required: the bootstrap script sends each report once and never retries.
func validateInstallFailurePayload(payload *gen.IngestPayload) error {
	report := canonicalInstallFailure(payload)
	switch {
	case !isInstallFailureReport(payload) && report != nil:
		return oops.E(oops.CodeInvalid, nil, "data.install_failure is only valid on client.install_failed events")
	case !isInstallFailureReport(payload):
		return nil
	case report == nil:
		return oops.E(oops.CodeInvalid, nil, "data.install_failure is required for client.install_failed events")
	case payload.Session != nil:
		return oops.E(oops.CodeInvalid, nil, "session is not valid on client.install_failed events")
	}
	rest := *payload.Data
	rest.InstallFailure = nil
	if block := activityBlockName(&rest); block != "" {
		return oops.E(oops.CodeInvalid, nil, "data.%s is not valid on client.install_failed events", block)
	}
	if _, ok := installFailureCauses[strings.TrimSpace(string(report.Cause))]; !ok {
		return oops.E(oops.CodeInvalid, nil, "data.install_failure.cause is not a known install failure cause")
	}
	return nil
}

func installFailureTime(payload *gen.IngestPayload, now time.Time) time.Time {
	raw := ""
	if payload != nil && payload.Event != nil {
		raw = strings.TrimSpace(conv.PtrValOr(payload.Event.OccurredAt, ""))
	}
	if raw == "" {
		if report := canonicalInstallFailure(payload); report != nil {
			raw = strings.TrimSpace(report.AttemptedAt)
		}
	}
	return clampEventTime(raw, now)
}

// recordInstallFailure sinks one bootstrap install failure report: a counter
// for alerting and a diagnostic telemetry row for investigation. The row
// counts as an error for the machine in hook_install_summaries, and the
// version the script tried to install stays off the device binary version so
// the fleet never reports a binary that did not install.
func (s *Service) recordInstallFailure(ctx context.Context, payload *gen.IngestPayload, authCtx *contextvalues.AuthContext, actor canonicalActor) {
	ctx, span := s.tracer.Start(ctx, "hooks.recordInstallFailure")
	defer span.End()

	report := canonicalInstallFailure(payload)
	if report == nil {
		return
	}
	adapter := strings.TrimSpace(payload.Source.Adapter)
	cause := strings.TrimSpace(string(report.Cause))
	harness := middleware.SanitizeDeviceValue(conv.PtrValOr(report.Harness, ""))
	deviceOS := middleware.SanitizeDeviceValue(conv.PtrValOr(report.Os, ""))
	arch := middleware.SanitizeDeviceValue(conv.PtrValOr(report.Arch, ""))
	version := middleware.SanitizeDeviceValue(conv.PtrValOr(report.Version, ""))

	s.metrics.RecordInstallFailure(ctx, installFailureMetricParams{
		Cause:   cause,
		Harness: harness,
		OS:      deviceOS,
		Version: version,
		OrgSlug: authCtx.OrganizationSlug,
	})

	s.logger.InfoContext(ctx, "hook install failure reported",
		attr.SlogEvent("hooks_install_failure"),
		attr.SlogHookSource(adapter),
		attr.SlogHookInstallCause(cause),
		attr.SlogHookInstallVersion(version),
		attr.SlogHookDeviceHarness(harness),
		attr.SlogHookDeviceOS(deviceOS),
		attr.SlogHookDeviceArch(arch),
		attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		attr.SlogProjectID(authCtx.ProjectID.String()),
	)

	if s.telemetryLogger == nil {
		return
	}
	attrs := map[attr.Key]any{
		attr.EventSourceKey:      string(telemetry.EventSourceHookDiagnostic),
		attr.HookEventKey:        eventTypeClientInstallFailed,
		attr.HookSourceKey:       adapter,
		attr.ProjectIDKey:        authCtx.ProjectID.String(),
		attr.OrganizationIDKey:   authCtx.ActiveOrganizationID,
		attr.SpanIDKey:           generateSpanID(),
		attr.TraceIDKey:          generateTraceID(),
		attr.LogBodyKey:          "Hook install failure: " + cause,
		attr.HookInstallCauseKey: cause,
	}
	optional := map[attr.Key]string{
		attr.HookHostnameKey:       strings.TrimSpace(conv.PtrValOr(payload.Source.Hostname, "")),
		attr.HookInstallVersionKey: version,
		attr.HookDeviceOSKey:       deviceOS,
		attr.HookDeviceArchKey:     arch,
		attr.HookDeviceHarnessKey:  harness,
	}
	for key, value := range optional {
		if value != "" {
			attrs[key] = value
		}
	}

	s.telemetryLogger.Log(ctx, telemetry.LogParams{
		Timestamp: installFailureTime(payload, s.now()),
		ToolInfo: telemetry.ToolInfo{
			Name:           "",
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
