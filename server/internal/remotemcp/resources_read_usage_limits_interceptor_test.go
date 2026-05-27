package remotemcp_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newResourcesReadRequestForInterceptor(t *testing.T, ctx context.Context) *proxy.ResourcesReadRequest {
	t.Helper()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "/", http.NoBody)
	require.NoError(t, err)

	return &proxy.ResourcesReadRequest{
		UserRequest: &proxy.UserRequest{
			UserHTTPRequest: httpReq,
			JSONRPCMessages: nil,
		},
		Params: nil,
	}
}

func TestResourcesReadUsageLimitsInterceptor_Name(t *testing.T) {
	t.Parallel()

	interceptor := remotemcp.NewResourcesReadUsageLimitsInterceptor(&fakeBillingRepo{storedUsage: nil, storedErr: nil}, testenv.NewLogger(t))
	require.Equal(t, "resources-read-usage-limits", interceptor.Name())
}

func TestResourcesReadUsageLimitsInterceptor_NoAuthContextPassesThrough(t *testing.T) {
	t.Parallel()

	// Billing repo deliberately left without behavior: the interceptor must
	// not reach it when auth context is missing.
	repo := &fakeBillingRepo{storedUsage: nil, storedErr: errors.New("must not be called")}
	interceptor := remotemcp.NewResourcesReadUsageLimitsInterceptor(repo, testenv.NewLogger(t))

	ctx := t.Context()
	read := newResourcesReadRequestForInterceptor(t, ctx)

	require.NoError(t, interceptor.InterceptResourcesReadRequest(ctx, read))
}

func TestResourcesReadUsageLimitsInterceptor_NonBaseTierPassesThrough(t *testing.T) {
	t.Parallel()

	repo := &fakeBillingRepo{storedUsage: nil, storedErr: errors.New("must not be called")}
	interceptor := remotemcp.NewResourcesReadUsageLimitsInterceptor(repo, testenv.NewLogger(t))

	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-pro",
		AccountType:          string(billing.TierPro),
	})
	read := newResourcesReadRequestForInterceptor(t, ctx)

	require.NoError(t, interceptor.InterceptResourcesReadRequest(ctx, read))
}

func TestResourcesReadUsageLimitsInterceptor_BillingErrorPassesThrough(t *testing.T) {
	t.Parallel()

	// Billing cache miss must not take down resource reads — the interceptor
	// logs and continues.
	repo := &fakeBillingRepo{storedUsage: nil, storedErr: errors.New("cache miss")}
	interceptor := remotemcp.NewResourcesReadUsageLimitsInterceptor(repo, testenv.NewLogger(t))

	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-free",
		AccountType:          string(billing.TierBase),
	})
	read := newResourcesReadRequestForInterceptor(t, ctx)

	require.NoError(t, interceptor.InterceptResourcesReadRequest(ctx, read))
}

func TestResourcesReadUsageLimitsInterceptor_ActiveSubscriptionPassesThrough(t *testing.T) {
	t.Parallel()

	repo := &fakeBillingRepo{
		storedUsage: &usage.PeriodUsage{
			HasActiveSubscription: true,
			ToolCalls:             999_999,
			IncludedToolCalls:     0,
		},
		storedErr: nil,
	}
	interceptor := remotemcp.NewResourcesReadUsageLimitsInterceptor(repo, testenv.NewLogger(t))

	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-free-with-sub",
		AccountType:          string(billing.TierBase),
	})
	read := newResourcesReadRequestForInterceptor(t, ctx)

	require.NoError(t, interceptor.InterceptResourcesReadRequest(ctx, read))
}

func TestResourcesReadUsageLimitsInterceptor_UnderHardLimitPassesThrough(t *testing.T) {
	t.Parallel()

	repo := &fakeBillingRepo{
		storedUsage: &usage.PeriodUsage{
			ToolCalls:             199,
			IncludedToolCalls:     100,
			HasActiveSubscription: false,
		},
		storedErr: nil,
	}
	interceptor := remotemcp.NewResourcesReadUsageLimitsInterceptor(repo, testenv.NewLogger(t))

	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-free",
		AccountType:          string(billing.TierBase),
	})
	read := newResourcesReadRequestForInterceptor(t, ctx)

	require.NoError(t, interceptor.InterceptResourcesReadRequest(ctx, read))
}

func TestResourcesReadUsageLimitsInterceptor_AtOrOverHardLimitRejects(t *testing.T) {
	t.Parallel()

	// Resource reads share the same hard cap counter as tool calls.
	repo := &fakeBillingRepo{
		storedUsage: &usage.PeriodUsage{
			ToolCalls:             200,
			IncludedToolCalls:     100,
			HasActiveSubscription: false,
		},
		storedErr: nil,
	}
	interceptor := remotemcp.NewResourcesReadUsageLimitsInterceptor(repo, testenv.NewLogger(t))

	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-free",
		AccountType:          string(billing.TierBase),
	})
	read := newResourcesReadRequestForInterceptor(t, ctx)

	err := interceptor.InterceptResourcesReadRequest(ctx, read)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestResourcesReadUsageLimitsInterceptor_ZeroIncludedUsesDefaultLimit(t *testing.T) {
	t.Parallel()

	repo := &fakeBillingRepo{
		storedUsage: &usage.PeriodUsage{
			ToolCalls:             2000,
			IncludedToolCalls:     0,
			HasActiveSubscription: false,
		},
		storedErr: nil,
	}
	interceptor := remotemcp.NewResourcesReadUsageLimitsInterceptor(repo, testenv.NewLogger(t))

	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-free",
		AccountType:          string(billing.TierBase),
	})
	read := newResourcesReadRequestForInterceptor(t, ctx)

	err := interceptor.InterceptResourcesReadRequest(ctx, read)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}
