package middleware

import (
	"context"
	"errors"

	"github.com/speakeasy-api/gram/internal/oops"
	goa "goa.design/goa/v3/pkg"
)

func MapErrors() func(goa.Endpoint) goa.Endpoint {
	return func(next goa.Endpoint) goa.Endpoint {
		return func(ctx context.Context, req any) (any, error) {
			val, err := next(ctx, req)

			var se *oops.ShareableError
			if err != nil && errors.As(err, &se) {
				return nil, se.AsGoa()
			}

			return val, err
		}
	}
}
