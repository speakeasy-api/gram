package contextvalues

import (
	"context"

	"github.com/google/uuid"
)

type contextKey string

type AuthContext struct {
	ActiveOrganizationID string
	UserID               string
	SessionID            *string
	ProjectID            *uuid.UUID
}

type CustomDomainContext struct {
	ProjectID uuid.UUID
	Domain    string
	DomainID  uuid.UUID
}

const (
	SessionTokenContextKey  contextKey = "sessionTokenKey"
	SessionValueContextKey  contextKey = "sessionValueKey"
	AdminOverrideContextKey contextKey = "adminOverrideKey"
	CustomDomainContextKey  contextKey = "customDomainKey"
)

func SetSessionTokenInContext(ctx context.Context, value string) context.Context {
	return context.WithValue(ctx, SessionTokenContextKey, value)
}

func GetSessionTokenFromContext(ctx context.Context) (string, bool) {
	value, ok := ctx.Value(SessionTokenContextKey).(string)
	return value, ok
}

func SetAdminOverrideInContext(ctx context.Context, value string) context.Context {
	return context.WithValue(ctx, AdminOverrideContextKey, value)
}

func GetAdminOverrideFromContext(ctx context.Context) (string, bool) {
	value, ok := ctx.Value(AdminOverrideContextKey).(string)
	return value, ok
}

func SetAuthContext(ctx context.Context, value *AuthContext) context.Context {
	return context.WithValue(ctx, SessionValueContextKey, value)
}

func GetAuthContext(ctx context.Context) (*AuthContext, bool) {
	value, ok := ctx.Value(SessionValueContextKey).(*AuthContext)
	return value, ok
}

func SetCustomDomainContext(ctx context.Context, value *CustomDomainContext) context.Context {
	return context.WithValue(ctx, CustomDomainContextKey, value)
}

func GetCustomDomainContext(ctx context.Context) (*CustomDomainContext, bool) {
	value, ok := ctx.Value(CustomDomainContextKey).(*CustomDomainContext)
	return value, ok
}
