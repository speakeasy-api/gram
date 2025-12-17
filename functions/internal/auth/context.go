package auth

import "context"

type ctxkey int

const (
	authContextKey ctxkey = iota
)

type AuthContext struct {
	InvocationID string
	Subject      string
}

func WithContext(ctx context.Context, ac *AuthContext) context.Context {
	return context.WithValue(ctx, authContextKey, ac)
}

func FromContext(ctx context.Context) *AuthContext {
	ac, ok := ctx.Value(authContextKey).(*AuthContext)
	if !ok {
		return nil
	}

	return ac
}
