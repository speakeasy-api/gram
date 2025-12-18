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
	OrganizationSlug     string
	Email                *string
	AccountType          string
	ProjectSlug          *string
	APIKeyScopes         []string
}

type RequestContext struct {
	ReqURL string
	Host   string
	Method string
}

const (
	SessionTokenContextKey             contextKey = "sessionTokenKey"
	SessionValueContextKey             contextKey = "sessionValueKey"
	AdminOverrideContextKey            contextKey = "adminOverrideKey"
	RequestContextKey                  contextKey = "requestContextKey"
	ChatSessionAllowedOriginContextKey contextKey = "chatSessionAllowedOriginKey"
)

func SetChatSessionAllowedOriginInContext(ctx context.Context, value string) context.Context {
	return context.WithValue(ctx, ChatSessionAllowedOriginContextKey, value)
}

func GetChatSessionAllowedOriginFromContext(ctx context.Context) (string, bool) {
	value, ok := ctx.Value(ChatSessionAllowedOriginContextKey).(string)
	return value, ok
}

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

func SetRequestContext(ctx context.Context, value *RequestContext) context.Context {
	return context.WithValue(ctx, RequestContextKey, value)
}

func GetRequestContext(ctx context.Context) (*RequestContext, bool) {
	value, ok := ctx.Value(RequestContextKey).(*RequestContext)
	return value, ok
}
