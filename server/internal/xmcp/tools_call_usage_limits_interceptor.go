package xmcp

import (
	"context"
	"errors"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// defaultHardToolCallsLimit is the fallback hard cap applied when the billing
// repo reports an IncludedToolCalls value of zero. It mirrors the value used
// by the existing tool usage check in the /mcp endpoint.
const defaultHardToolCallsLimit = 2000

// ToolsCallUsageLimitsInterceptor enforces the free-tier hard cap on tools/call
// invocations by consulting the billing repository's cached period usage.
// It is a [proxy.ToolsCallRequestInterceptor]: it runs after the generic
// user-request chain and before the request is forwarded upstream. Non-free
// tiers and orgs with an active subscription skip the check.
//
// The interceptor intentionally fails open when cached usage is unavailable
// (the billing cache should always be warm, but a transient miss must not
// take down tool invocation). Failures are logged with the originating org ID
// so operators can spot them in dashboards.
type ToolsCallUsageLimitsInterceptor struct {
	billing billing.Repository
	logger  *slog.Logger
}

var _ proxy.ToolsCallRequestInterceptor = (*ToolsCallUsageLimitsInterceptor)(nil)

// NewToolsCallUsageLimitsInterceptor constructs an interceptor bound to the given
// billing repository. The same instance can be reused across requests.
func NewToolsCallUsageLimitsInterceptor(billingRepo billing.Repository, logger *slog.Logger) *ToolsCallUsageLimitsInterceptor {
	return &ToolsCallUsageLimitsInterceptor{
		billing: billingRepo,
		logger:  logger,
	}
}

// Name implements [proxy.ToolsCallRequestInterceptor].
func (i *ToolsCallUsageLimitsInterceptor) Name() string {
	return "tools-call-usage-limits"
}

// InterceptToolsCallRequest implements [proxy.ToolsCallRequestInterceptor].
// It reads the organization and account type from the request's auth
// context, consults cached billing usage, and returns a forbidden error when
// the org has exceeded its hard cap.
func (i *ToolsCallUsageLimitsInterceptor) InterceptToolsCallRequest(ctx context.Context, _ *proxy.ToolsCallRequest) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		// No auth context means the runtime handler did not install one —
		// fail open rather than guess at an identity to rate-limit against.
		return nil
	}

	if authCtx.AccountType != string(billing.TierBase) {
		return nil
	}

	// Hot-path: only read cached usage. A miss here is treated as a
	// best-effort skip so billing cache issues never break tool invocation.
	periodUsage, err := i.billing.GetStoredPeriodUsage(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		i.logger.ErrorContext(ctx, "failed to get stored period usage",
			attr.SlogError(err),
			attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
		return nil
	}

	if periodUsage == nil || periodUsage.HasActiveSubscription {
		return nil
	}

	hardToolCallsLimit := 2 * periodUsage.IncludedToolCalls
	if hardToolCallsLimit == 0 {
		hardToolCallsLimit = defaultHardToolCallsLimit
	}

	if periodUsage.ToolCalls >= hardToolCallsLimit {
		return oops.E(oops.CodeForbidden, errors.New("tool usage limit reached"), "tool usage limit reached").Log(ctx, i.logger, attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	}

	return nil
}
