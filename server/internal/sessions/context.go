package sessions

import "context"

type contextKey string

const (
	SessionTokenContextKey contextKey = "sessionTokenKey"
	SessionValueContextKey contextKey = "sessionValueKey"
)

func SetSessionTokenInContext(ctx context.Context, value string) context.Context {
	return context.WithValue(ctx, SessionTokenContextKey, value)
}

func GetSessionTokenFromContext(ctx context.Context) (string, bool) {
	value, ok := ctx.Value(SessionTokenContextKey).(string)
	return value, ok
}

func SetSessionValueInContext(ctx context.Context, value *GramSession) context.Context {
	return context.WithValue(ctx, SessionValueContextKey, value)
}

func GetSessionValueFromContext(ctx context.Context) (*GramSession, bool) {
	value, ok := ctx.Value(SessionValueContextKey).(*GramSession)
	return value, ok
}
