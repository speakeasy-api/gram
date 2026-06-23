package middleware

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	goa "goa.design/goa/v3/pkg"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TraceMethods(tracer trace.Tracer) func(goa.Endpoint) goa.Endpoint {
	return func(next goa.Endpoint) goa.Endpoint {
		return func(ctx context.Context, req any) (any, error) {
			svc, ok := ctx.Value(goa.ServiceKey).(string)
			if !ok {
				panic("goa.ServiceKey not found in context")
			}

			method, ok := ctx.Value(goa.MethodKey).(string)
			if !ok {
				panic("goa.MethodKey not found in context")
			}

			ctx, span := tracer.Start(ctx, fmt.Sprintf("%s.%s", svc, method))
			defer span.End()

			val, err := next(ctx, req)
			if err != nil {
				// A boundary logging helper (LogError/LogWarn/LogInfo) already
				// applies span treatment, including deliberate cancellation- and
				// client-fault-noise suppression. Re-recording here would duplicate
				// the exception event and undo that suppression, so only act as a
				// fallback for errors that never passed through such a helper.
				if se, ok := errors.AsType[*oops.ShareableError](err); ok {
					if !se.SpanHandled() {
						span.SetStatus(codes.Error, se.String())
						span.RecordError(se, trace.WithStackTrace(true))
					}
				} else {
					span.SetStatus(codes.Error, err.Error())
					span.RecordError(err, trace.WithStackTrace(true))
				}
				return nil, err
			}

			return val, nil
		}
	}
}
