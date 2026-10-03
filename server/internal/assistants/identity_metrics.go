package assistants

import (
	"context"
	"errors"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

func identityAdmissionResult(err error) string {
	switch {
	case err == nil:
		return "issued"
	case errors.Is(err, assistantidentity.ErrRolloutDisabled):
		return "rollout_disabled"
	case errors.Is(err, assistantidentity.ErrActorIneligible), errors.Is(err, assistantidentity.ErrInvalidIdentity), errors.Is(err, assistantidentity.ErrExecutionAdmissionRequired):
		return "denied"
	default:
		return "retryable_error"
	}
}

func (s *ServiceCore) recordIdentityAdmission(ctx context.Context, e *assistantidentity.Execution, err error) {
	if s.identityAdmission == nil {
		return
	}
	mode, fallback := "LEGACY", false
	if e != nil {
		mode = "UNKNOWN"
		if e.Mode == assistantidentity.ExecutionWorkload || e.Mode == assistantidentity.ExecutionWorkloadHuman {
			mode = string(e.Mode)
		}
		fallback = e.FallbackReason != ""
	}
	// Mode and fallback presence have a fixed cardinality; never label by IDs,
	// issuer subjects, user-controlled reason strings, or error messages.
	s.identityAdmission.Add(ctx, 1, metric.WithAttributes(attribute.String("result", identityAdmissionResult(err)), attribute.String("mode", mode), attribute.Bool("autonomous_fallback", fallback)))
}
