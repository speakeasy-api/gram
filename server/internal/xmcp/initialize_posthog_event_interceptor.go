package xmcp

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
)

// eventMCPInitialized is the PostHog event name emitted for every observed
// `initialize` request. AGE-1902 tracks unifying this with the equivalent
// `/mcp` event so both runtimes share a single product-analytics schema.
const eventMCPInitialized = "mcp_initialized"

// InitializePostHogEventInterceptor emits the [eventMCPInitialized] PostHog
// event for every JSON-RPC `initialize` request observed by `/x/mcp`. It is
// a [proxy.InitializeRequestInterceptor]: the proxy routes only initialize
// requests to it, so the interceptor never has to dispatch on method.
// Analytics emission is best-effort and never rejects.
type InitializePostHogEventInterceptor struct {
	posthog *posthog.Posthog
	logger  *slog.Logger
}

var _ proxy.InitializeRequestInterceptor = (*InitializePostHogEventInterceptor)(nil)

// NewInitializePostHogEventInterceptor constructs an interceptor bound to the
// given PostHog client. The same instance can be reused across requests.
func NewInitializePostHogEventInterceptor(posthogClient *posthog.Posthog, logger *slog.Logger) *InitializePostHogEventInterceptor {
	return &InitializePostHogEventInterceptor{
		posthog: posthogClient,
		logger:  logger,
	}
}

// Name implements [proxy.InitializeRequestInterceptor].
func (i *InitializePostHogEventInterceptor) Name() string {
	return "initialize-posthog-event"
}

// InterceptInitializeRequest implements [proxy.InitializeRequestInterceptor].
// Always returns nil — the interceptor emits the event as a side-effect;
// PostHog enqueue failures are logged but do not surface to the user.
func (i *InitializePostHogEventInterceptor) InterceptInitializeRequest(ctx context.Context, init *proxy.InitializeRequest) error {
	if i.posthog == nil || init == nil || init.UserRequest == nil {
		return nil
	}

	requestContext, _ := contextvalues.GetRequestContext(ctx)
	if requestContext == nil {
		return nil
	}

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	authenticated := authCtx != nil && authCtx.ProjectID != nil

	var projectID string
	if authenticated {
		projectID = authCtx.ProjectID.String()
	}

	// Match /mcp's parseMcpSessionID behavior: the client may not send a
	// session header on the very first initialize request, so synthesize a
	// UUID for the PostHog distinct ID rather than emitting an empty one.
	sessionID := ""
	if init.UserRequest.UserHTTPRequest != nil {
		sessionID = init.UserRequest.UserHTTPRequest.Header.Get("Mcp-Session-Id")
	}
	if sessionID == "" {
		sessionID = uuid.NewString()
	}

	if err := i.posthog.CaptureEvent(ctx, eventMCPInitialized, sessionID, map[string]any{
		"project_id":           projectID,
		"authenticated":        authenticated,
		"mcp_domain":           requestContext.Host,
		"mcp_url":              requestContext.Host + requestContext.ReqURL,
		"disable_notification": true,
		"mcp_session_id":       sessionID,
	}); err != nil {
		i.logger.ErrorContext(ctx, "failed to capture "+eventMCPInitialized+" event", attr.SlogError(err))
	}

	return nil
}
