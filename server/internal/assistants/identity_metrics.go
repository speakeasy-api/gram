package assistants

import (
	"context"
	"errors"

	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	identityAdmissionIssued           = "issued"
	identityAdmissionDenied           = "denied"
	identityAdmissionRetryableError   = "retryable_error"
	identityAdmissionModeThreadScoped = "THREAD_SCOPED"
	identityAdmissionModeUnknown      = "UNKNOWN"
)

func identityAdmissionResult(err error) string {
	switch {
	case err == nil:
		return identityAdmissionIssued
	case errors.Is(err, assistantidentity.ErrActorIneligible), errors.Is(err, assistantidentity.ErrInvalidIdentity), errors.Is(err, assistantidentity.ErrExecutionAdmissionRequired):
		return identityAdmissionDenied
	default:
		return identityAdmissionRetryableError
	}
}

func (s *ServiceCore) recordIdentityAdmission(ctx context.Context, e *assistantidentity.Execution, err error) {
	if s.identityAdmission == nil {
		return
	}
	mode, fallback := identityAdmissionModeThreadScoped, false
	if e != nil {
		mode = identityAdmissionModeUnknown
		if e.Mode == assistantidentity.ExecutionWorkload || e.Mode == assistantidentity.ExecutionWorkloadHuman {
			mode = string(e.Mode)
		}
		fallback = e.FallbackReason != ""
	}
	// Mode and fallback presence have a fixed cardinality; never label by IDs,
	// issuer subjects, user-controlled reason strings, or error messages.
	s.identityAdmission.Add(ctx, 1, metric.WithAttributes(attribute.String("result", identityAdmissionResult(err)), attribute.String("mode", mode), attribute.Bool("autonomous_fallback", fallback)))
}
