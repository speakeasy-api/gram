package access

import "context"

type contextKey string

const grantsContextKey contextKey = "access_grants"

// GrantsToContext stores resolved grants on the request context.
func GrantsToContext(ctx context.Context, grants *Grants) context.Context {
	return context.WithValue(ctx, grantsContextKey, grants)
}

// GrantsFromContext loads resolved grants from the request context.
func GrantsFromContext(ctx context.Context) (*Grants, bool) {
	grants, ok := ctx.Value(grantsContextKey).(*Grants)
	return grants, ok
}
