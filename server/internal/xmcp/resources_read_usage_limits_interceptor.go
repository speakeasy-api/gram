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

// ResourcesReadUsageLimitsInterceptor enforces the free-tier hard cap on
// resources/read invocations by consulting the billing repository's cached
// period usage. It mirrors [ToolsCallUsageLimitsInterceptor] — resource reads
// are metered alongside tool calls in the same `ToolCalls` counter, so the
// same cap applies. It is a [proxy.ResourcesReadRequestInterceptor]: it runs
// after the generic user-request chain and before the request is forwarded
// upstream. Non-free tiers and orgs with an active subscription skip the
// check.
//
// The interceptor intentionally fails open when cached usage is unavailable
// (the billing cache should always be warm, but a transient miss must not
// take down resource reads). Failures are logged with the originating org ID
// so operators can spot them in dashboards.
type ResourcesReadUsageLimitsInterceptor struct {
	billing billing.Repository
	logger  *slog.Logger
}

var _ proxy.ResourcesReadRequestInterceptor = (*ResourcesReadUsageLimitsInterceptor)(nil)

// NewResourcesReadUsageLimitsInterceptor constructs an interceptor bound to the
// given billing repository. The same instance can be reused across requests.
func NewResourcesReadUsageLimitsInterceptor(billingRepo billing.Repository, logger *slog.Logger) *ResourcesReadUsageLimitsInterceptor {
	return &ResourcesReadUsageLimitsInterceptor{
		billing: billingRepo,
		logger:  logger,
	}
}

// Name implements [proxy.ResourcesReadRequestInterceptor].
func (i *ResourcesReadUsageLimitsInterceptor) Name() string {
	return "resources-read-usage-limits"
}

// InterceptResourcesReadRequest implements [proxy.ResourcesReadRequestInterceptor].
// It reads the organization and account type from the request's auth
// context, consults cached billing usage, and returns a forbidden error when
// the org has exceeded its hard cap. The cap shared with tools/call is
// intentional — resource reads are accounted to the same `ToolCalls`
// counter — so a free-tier org that has exhausted its quota on tool calls
// is also blocked from resource reads, and vice versa.
func (i *ResourcesReadUsageLimitsInterceptor) InterceptResourcesReadRequest(ctx context.Context, _ *proxy.ResourcesReadRequest) error {
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
	// best-effort skip so billing cache issues never break resource reads.
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
