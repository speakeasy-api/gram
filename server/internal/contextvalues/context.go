package contextvalues

import (
	"context"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
)

type contextKey string

type AuthContext struct {
	ActiveOrganizationID  string
	UserID                string
	ExternalUserID        string // Customer-provided user identifier (e.g., from chat session JWTs)
	APIKeyID              string
	APIKeyName            string // Dashboard-visible key name
	OrgWidePluginHooksKey bool   // Authenticated key carries the publish-minted hooks token/name marker
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
	// SupportOrganizationID is set only after session authentication validates
	// a time-bounded platform-admin support session for this organization.
	SupportOrganizationID     string
	gramSessionValidated      bool
	supportSessionValidated   bool
	legacySessionImpersonated bool
}

// WithValidatedGramSession records provenance established by sessions.Authenticate.
// Other authentication paths must not call this function.
func WithValidatedGramSession(ctx context.Context, authCtx *AuthContext, legacyImpersonated bool) context.Context {
	validated := *authCtx
	validated.gramSessionValidated = true
	validated.legacySessionImpersonated = legacyImpersonated
	return SetAuthContext(ctx, &validated)
}

// HasValidatedGramSession reports whether ordinary Gram session authentication
// positively validated the request credential.
func HasValidatedGramSession(ctx context.Context) bool {
	authCtx, ok := GetAuthContext(ctx)
	return ok && authCtx != nil && authCtx.gramSessionValidated
}

// IsOrdinaryGramUserSession reports whether the request has only authenticated
// user-session provenance, with no alternate or elevated acting surface.
func IsOrdinaryGramUserSession(ctx context.Context) bool {
	authCtx, ok := GetAuthContext(ctx)
	if !ok || authCtx == nil || !authCtx.gramSessionValidated || authCtx.SessionID == nil || *authCtx.SessionID == "" ||
		authCtx.ActiveOrganizationID == "" || authCtx.UserID == "" || authCtx.APIKeyID != "" || authCtx.APIKeyName != "" ||
		len(authCtx.APIKeyScopes) != 0 || authCtx.OrgWidePluginHooksKey || IsSupportSession(ctx) || IsLegacyImpersonatedSession(ctx) {
		return false
	}
	if _, ok := GetAssistantPrincipal(ctx); ok {
		return false
	}
	if _, ok := GetOAuthClientID(ctx); ok {
		return false
	}
	if _, ok := GetActingSurface(ctx); ok {
		return false
	}
	if _, ok := GetRBACScopeOverride(ctx); ok {
		return false
	}
	return true
}

// IsLegacyImpersonatedSession reports legacy WorkOS impersonation propagated by
// sessions.Authenticate without exposing the impersonator identity.
func IsLegacyImpersonatedSession(ctx context.Context) bool {
	authCtx, ok := GetAuthContext(ctx)
	return ok && authCtx != nil && authCtx.gramSessionValidated && authCtx.legacySessionImpersonated
}

// WithValidatedSupportSession records the support decision made during session
// authentication. Keeping this immutable for the request ensures grants and
// support safeguards cannot disagree when the session expires mid-request.
func WithValidatedSupportSession(ctx context.Context, authCtx *AuthContext) context.Context {
	validated := *authCtx
	validated.supportSessionValidated = true
	return SetAuthContext(ctx, &validated)
}

// IsSupportSession is the single trusted predicate for support-only behavior.
// Request headers and legacy cookies never satisfy it directly.
func IsSupportSession(ctx context.Context) bool {
	authCtx, ok := GetAuthContext(ctx)
	return ok && authCtx != nil && authCtx.supportSessionValidated && authCtx.IsAdmin &&
		authCtx.SupportOrganizationID != "" &&
		authCtx.SupportOrganizationID == authCtx.ActiveOrganizationID
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

// AdminAuthContext carries identity information for an admin member
// authenticated against Google for the admin service. It is intentionally
// separate from AuthContext — admin auth is a fully isolated system and does
// not share session, RBAC, or organization state with Speakeasy IDP auth.
type AdminAuthContext struct {
	SessionID   string
	Email       string
	OIDCSubject string
	Name        string
	HD          string
}

type RPCContext struct {
	ID mcpjsonrpc.ID
}

const (
	SessionTokenContextKey         contextKey = "sessionTokenKey"
	sessionCookieRefreshContextKey contextKey = "sessionCookieRefreshKey"
	SessionValueContextKey         contextKey = "sessionValueKey"
	RequestContextKey              contextKey = "requestContextKey"
	RBACScopeOverrideContextKey    contextKey = "rbacScopeOverrideKey"
	AssistantPrincipalKey          contextKey = "assistantPrincipalKey"
	AdminSessionTokenContextKey    contextKey = "adminSessionTokenKey"
	AdminAuthContextKey            contextKey = "adminAuthKey"
	RPCContextKey                  contextKey = "rpcContextKey"
	pubsubSubscriberContextKey     contextKey = "pubsubSubscriberKey"
	oauthClientIDContextKey        contextKey = "oauthClientIDKey"
	actingSurfaceContextKey        contextKey = "actingSurfaceKey"
)

func SetSessionTokenInContext(ctx context.Context, value string) context.Context {
	return context.WithValue(ctx, SessionTokenContextKey, value)
}

func GetSessionTokenFromContext(ctx context.Context) (string, bool) {
	value, ok := ctx.Value(SessionTokenContextKey).(string)
	return value, ok
}

func WithSessionCookieRefresh(ctx context.Context, refresh func(sessionID string)) context.Context {
	return context.WithValue(ctx, sessionCookieRefreshContextKey, refresh)
}

func RefreshSessionCookie(ctx context.Context, sessionID string) {
	if refresh, ok := ctx.Value(sessionCookieRefreshContextKey).(func(string)); ok && refresh != nil {
		refresh(sessionID)
	}
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

func SetRPCContext(ctx context.Context, value *RPCContext) context.Context {
	return context.WithValue(ctx, RPCContextKey, value)
}

func GetRPCContext(ctx context.Context) (*RPCContext, bool) {
	value, ok := ctx.Value(RPCContextKey).(*RPCContext)
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

// SetOAuthClientID records the OAuth client the request's bearer token was
// issued to.
//
// It lives outside AuthContext on purpose: an anonymous session on a public
// MCP server has a real registered OAuth client but deliberately never gets an
// AuthContext, since stamping the endpoint's organization would misrepresent
// an unknown caller as a member of it.
func SetOAuthClientID(ctx context.Context, value string) context.Context {
	return context.WithValue(ctx, oauthClientIDContextKey, value)
}

// GetOAuthClientID returns the OAuth client the request authenticated as. The
// second result is false when the request carried no OAuth client — an
// unauthenticated caller, a non-OAuth credential, or a token minted before the
// `client_id` claim existed.
func GetOAuthClientID(ctx context.Context) (string, bool) {
	value, ok := ctx.Value(oauthClientIDContextKey).(string)
	return value, ok && value != ""
}

// SetActingSurface marks the surface a request arrives through, for surfaces
// that cannot be told apart from the auth context alone.
//
// Audit derives most surfaces from signals it can already see — a session, an
// API key, an assistant principal. A surface that authenticates its own way,
// such as Platform MCP, has no such signal and says so explicitly here.
//
// The value is a plain string so that packages carrying a surface do not have
// to depend on the audit package. Audit accepts only the values on its own
// allowlist, so an unrecognized one records an unknown surface rather than
// widening what the column can hold.
// ActingSurfacePlatformMCP marks a call arriving through the OAuth-authenticated
// Platform MCP endpoint. It is named here rather than only in that package
// because authorization has to recognize the surface: a Platform MCP call
// carries a real user but no browser session, and the session is what tells
// RBAC to enforce for every other human-driven surface.
const ActingSurfacePlatformMCP = "platform_mcp"

func SetActingSurface(ctx context.Context, value string) context.Context {
	return context.WithValue(ctx, actingSurfaceContextKey, value)
}

// GetActingSurface returns the explicitly marked acting surface. The second
// result is false when nothing marked one, which is the ordinary case for
// requests whose surface audit can derive on its own.
func GetActingSurface(ctx context.Context) (string, bool) {
	value, ok := ctx.Value(actingSurfaceContextKey).(string)
	return value, ok && value != ""
}

func SetAdminSessionTokenInContext(ctx context.Context, value string) context.Context {
	return context.WithValue(ctx, AdminSessionTokenContextKey, value)
}

func GetAdminSessionTokenFromContext(ctx context.Context) (string, bool) {
	value, ok := ctx.Value(AdminSessionTokenContextKey).(string)
	return value, ok
}

func SetAdminAuthContext(ctx context.Context, value *AdminAuthContext) context.Context {
	return context.WithValue(ctx, AdminAuthContextKey, value)
}

func GetAdminAuthContext(ctx context.Context) (*AdminAuthContext, bool) {
	value, ok := ctx.Value(AdminAuthContextKey).(*AdminAuthContext)
	return value, ok && value != nil
}

type PubSubSubscriberContext struct {
	TopicProtoName        string
	SubscriptionProtoName string
}

func SetPubSubSubscriberContext(ctx context.Context, value PubSubSubscriberContext) context.Context {
	return context.WithValue(ctx, pubsubSubscriberContextKey, &value)
}

func GetPubSubSubscriberContext(ctx context.Context) (*PubSubSubscriberContext, bool) {
	value, ok := ctx.Value(pubsubSubscriberContextKey).(*PubSubSubscriberContext)
	return value, ok
}
