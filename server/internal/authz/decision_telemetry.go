package authz

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/authz/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

const (
	authorizationDecisionEvent = "authorization.decision"
	boundedUnknownValue        = "unknown"
)

// RecordAuthorizationDecision emits an identifier-only span event for the
// shared authorization path and intrinsic agent-management decisions. It must
// not grow to include checks, selectors, policy contents, names, emails, or errors.
func RecordAuthorizationDecision(ctx context.Context, operation repo.Operation, outcome repo.Outcome, reason repo.Reason) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}

	attrs := []attribute.KeyValue{
		attribute.String("gram.authorization.operation", boundedOperation(operation)),
		attribute.String("gram.authorization.result", boundedOutcome(outcome)),
		attribute.String("gram.authorization.reason", boundedReason(reason)),
	}
	for key, value := range contextvalues.ActorTelemetryAttributes(ctx) {
		if value != "" {
			attrs = append(attrs, attribute.String(key, value))
		}
	}
	if clientID, ok := contextvalues.GetOAuthClientID(ctx); ok {
		attrs = append(attrs, attribute.String("gram.authorization.oauth_client_id", clientID))
	}

	span.AddEvent(authorizationDecisionEvent, trace.WithAttributes(attrs...))
}

func boundedOperation(value repo.Operation) string {
	switch value {
	case repo.OperationRequire, repo.OperationRequireAny, repo.OperationFilter:
		return string(value)
	default:
		return boundedUnknownValue
	}
}

func boundedOutcome(value repo.Outcome) string {
	switch value {
	case repo.OutcomeAllow, repo.OutcomeDeny, repo.OutcomeError:
		return string(value)
	default:
		return boundedUnknownValue
	}
}

func boundedReason(value repo.Reason) string {
	switch value {
	case repo.ReasonGrantMatched, repo.ReasonNoGrants, repo.ReasonScopeUnsatisfied,
		repo.ReasonDenyGrant, repo.ReasonInvalidCheck, repo.ReasonRBACSkippedAPIKey,
		repo.ReasonDevOverride:
		return string(value)
	default:
		return boundedUnknownValue
	}
}
