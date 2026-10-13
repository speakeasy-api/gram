package internalsessionissuers

import (
	"context"
	"errors"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// meterOperations counts issuer operations by operation and outcome. A
// misconfigured outcome is a platform problem to alert on; refused is an
// organization's own state.
const meterOperations = "gram.internal_session_issuer.operations"

const (
	opSign              = "sign"
	opEnable            = "enable"
	opDisable           = "disable"
	opRotate            = "rotate"
	opRotateCompromised = "rotate_compromised"
)

type metrics struct {
	operations metric.Int64Counter
}

func newMetrics(logger *slog.Logger, meterProvider metric.MeterProvider) *metrics {
	operations, err := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/internalsessionissuers").Int64Counter(
		meterOperations,
		metric.WithDescription("Internal session issuer operations by operation and outcome."),
		metric.WithUnit("{operation}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterOperations), attr.SlogError(err))
	}
	return &metrics{operations: operations}
}

func (m *metrics) record(ctx context.Context, op string, err error) {
	if m == nil || m.operations == nil {
		return
	}
	m.operations.Add(ctx, 1, metric.WithAttributes(attr.InternalSessionIssuerOperation(op), attr.Outcome(outcome(err))))
}

func outcome(err error) string {
	_, refused := errors.AsType[*SigningRefusedError](err)
	_, pending := errors.AsType[*RotationPendingError](err)
	switch {
	case err == nil, pending:
		return "success"
	case refused, errors.Is(err, ErrNotFound):
		return "refused"
	case errors.Is(err, ErrInvalidClaims), errors.Is(err, ErrUnknownKid):
		return "invalid"
	case errors.Is(err, ErrMisconfigured):
		return "misconfigured"
	default:
		return "failure"
	}
}
