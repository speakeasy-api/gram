package middleware

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	goa "goa.design/goa/v3/pkg"
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
				span.SetStatus(codes.Error, err.Error())
				return nil, err
			}

			return val, nil
		}
	}
}
