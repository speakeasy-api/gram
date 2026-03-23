package access

import "context"

type contextKey string

const grantsContextKey contextKey = "access_grants"

func GrantsToContext(ctx context.Context, grants *Grants) context.Context {
	return context.WithValue(ctx, grantsContextKey, grants)
}

func GrantsFromContext(ctx context.Context) (*Grants, bool) {
	grants, ok := ctx.Value(grantsContextKey).(*Grants)
	return grants, ok
}
