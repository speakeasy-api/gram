package contextvalues

import (
	"context"

	"github.com/google/uuid"
)

type contextKey string

type AuthContext struct {
	ActiveOrganizationID string
	UserID               string
	ExternalUserID       string // Customer-provided user identifier (e.g., from chat session JWTs)
	APIKeyID             string
	// APIKeyToolsetID, when set, restricts the API key's authority to MCP
	// requests targeting this single toolset. Populated for plugin-scoped
	// keys (rfc-plugin-scoped-keys.md); nil for org-wide keys and non-key
	// auth (sessions, OAuth). Enforced at the MCP entrypoint.
	APIKeyToolsetID       *uuid.UUID
	SessionID             *string
	ProjectID             *uuid.UUID
	OrganizationSlug      string
	Email                 *string
	AccountType           string
	HasActiveSubscription bool
	Whitelisted           bool
	ProjectSlug           *string
	APIKeyScopes          []string
	IsAdmin               bool
}

type RequestContext struct {
	ReqID       string
	ReqURL      string
	Host        string
	Method      string
	Referer     string
	RefererHost string
	UserAgent   string
}

const (
	SessionTokenContextKey      contextKey = "sessionTokenKey"
	SessionValueContextKey      contextKey = "sessionValueKey"
	AdminOverrideContextKey     contextKey = "adminOverrideKey"
	RequestContextKey           contextKey = "requestContextKey"
	RBACScopeOverrideContextKey contextKey = "rbacScopeOverrideKey"
	AssistantPrincipalKey       contextKey = "assistantPrincipalKey"
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

func SetRequestContext(ctx context.Context, value *RequestContext) context.Context {
	return context.WithValue(ctx, RequestContextKey, value)
}

func GetRequestContext(ctx context.Context) (*RequestContext, bool) {
	value, ok := ctx.Value(RequestContextKey).(*RequestContext)
	return value, ok
}

func SetRBACScopeOverride(ctx context.Context, value string) context.Context {
	return context.WithValue(ctx, RBACScopeOverrideContextKey, value)
}

func GetRBACScopeOverride(ctx context.Context) (string, bool) {
	value, ok := ctx.Value(RBACScopeOverrideContextKey).(string)
	return value, ok && value != ""
}

// AssistantPrincipal marks an auth context that was established via an
// assistant runtime token. It signals to RBAC that grants should be loaded
// and enforced against the assistant's owning user (stamped as UserID on
// the AuthContext) rather than being skipped as they would be for a non-
// session request.
type AssistantPrincipal struct {
	AssistantID uuid.UUID
	ThreadID    uuid.UUID
}

func SetAssistantPrincipal(ctx context.Context, value AssistantPrincipal) context.Context {
	return context.WithValue(ctx, AssistantPrincipalKey, value)
}

func GetAssistantPrincipal(ctx context.Context) (AssistantPrincipal, bool) {
	value, ok := ctx.Value(AssistantPrincipalKey).(AssistantPrincipal)
	return value, ok
}
