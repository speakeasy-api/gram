package xmcp_test

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
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/xmcp"
)

// fakeBillingRepo implements the subset of [billing.Repository] exercised by
// ToolsCallUsageLimitsInterceptor. Methods the interceptor does not call panic so
// accidental use is loud in tests. Tests set storedUsage / storedErr to
// control what GetStoredPeriodUsage returns.
type fakeBillingRepo struct {
	billing.Repository // embedded for the unused-methods-panic effect: calling any unoverridden method is a nil-pointer panic, which is equivalent to failing the test
	storedUsage        *usage.PeriodUsage
	storedErr          error
}

func (f *fakeBillingRepo) GetStoredPeriodUsage(_ context.Context, _ string) (*usage.PeriodUsage, error) {
	return f.storedUsage, f.storedErr
}

func newToolsCallRequestForInterceptor(t *testing.T, ctx context.Context) *proxy.ToolsCallRequest {
	t.Helper()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "/", http.NoBody)
	require.NoError(t, err)

	return &proxy.ToolsCallRequest{
		UserRequest: &proxy.UserRequest{
			UserHTTPRequest: httpReq,
			JSONRPCMessages: nil,
		},
		Params: nil,
	}
}

func TestToolsCallUsageLimitsInterceptor_Name(t *testing.T) {
	t.Parallel()

	interceptor := xmcp.NewToolsCallUsageLimitsInterceptor(&fakeBillingRepo{storedUsage: nil, storedErr: nil}, testenv.NewLogger(t))
	require.Equal(t, "tools-call-usage-limits", interceptor.Name())
}

func TestToolsCallUsageLimitsInterceptor_NoAuthContextPassesThrough(t *testing.T) {
	t.Parallel()

	// Billing repo deliberately left without behavior: the interceptor must
	// not reach it when auth context is missing.
	repo := &fakeBillingRepo{storedUsage: nil, storedErr: errors.New("must not be called")}
	interceptor := xmcp.NewToolsCallUsageLimitsInterceptor(repo, testenv.NewLogger(t))

	ctx := t.Context()
	call := newToolsCallRequestForInterceptor(t, ctx)

	require.NoError(t, interceptor.InterceptToolsCallRequest(ctx, call))
}

func TestToolsCallUsageLimitsInterceptor_NonBaseTierPassesThrough(t *testing.T) {
	t.Parallel()

	repo := &fakeBillingRepo{storedUsage: nil, storedErr: errors.New("must not be called")}
	interceptor := xmcp.NewToolsCallUsageLimitsInterceptor(repo, testenv.NewLogger(t))

	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-pro",
		AccountType:          string(billing.TierPro),
	})
	call := newToolsCallRequestForInterceptor(t, ctx)

	require.NoError(t, interceptor.InterceptToolsCallRequest(ctx, call))
}

func TestToolsCallUsageLimitsInterceptor_BillingErrorPassesThrough(t *testing.T) {
	t.Parallel()

	// Billing cache miss must not take down tool invocation — the interceptor
	// logs and continues.
	repo := &fakeBillingRepo{storedUsage: nil, storedErr: errors.New("cache miss")}
	interceptor := xmcp.NewToolsCallUsageLimitsInterceptor(repo, testenv.NewLogger(t))

	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-free",
		AccountType:          string(billing.TierBase),
	})
	call := newToolsCallRequestForInterceptor(t, ctx)

	require.NoError(t, interceptor.InterceptToolsCallRequest(ctx, call))
}

func TestToolsCallUsageLimitsInterceptor_ActiveSubscriptionPassesThrough(t *testing.T) {
	t.Parallel()

	repo := &fakeBillingRepo{
		storedUsage: &usage.PeriodUsage{
			HasActiveSubscription: true,
			ToolCalls:             999_999, // far over any conceivable hard limit
			IncludedToolCalls:     0,
		},
		storedErr: nil,
	}
	interceptor := xmcp.NewToolsCallUsageLimitsInterceptor(repo, testenv.NewLogger(t))

	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-free-with-sub",
		AccountType:          string(billing.TierBase),
	})
	call := newToolsCallRequestForInterceptor(t, ctx)

	require.NoError(t, interceptor.InterceptToolsCallRequest(ctx, call))
}

func TestToolsCallUsageLimitsInterceptor_UnderHardLimitPassesThrough(t *testing.T) {
	t.Parallel()

	// IncludedToolCalls=100 → hardLimit=200. Usage at 199 is under.
	repo := &fakeBillingRepo{
		storedUsage: &usage.PeriodUsage{
			ToolCalls:             199,
			IncludedToolCalls:     100,
			HasActiveSubscription: false,
		},
		storedErr: nil,
	}
	interceptor := xmcp.NewToolsCallUsageLimitsInterceptor(repo, testenv.NewLogger(t))

	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-free",
		AccountType:          string(billing.TierBase),
	})
	call := newToolsCallRequestForInterceptor(t, ctx)

	require.NoError(t, interceptor.InterceptToolsCallRequest(ctx, call))
}

func TestToolsCallUsageLimitsInterceptor_AtOrOverHardLimitRejects(t *testing.T) {
	t.Parallel()

	// IncludedToolCalls=100 → hardLimit=200. Usage at 200 is exactly at the
	// cap; >= should reject.
	repo := &fakeBillingRepo{
		storedUsage: &usage.PeriodUsage{
			ToolCalls:             200,
			IncludedToolCalls:     100,
			HasActiveSubscription: false,
		},
		storedErr: nil,
	}
	interceptor := xmcp.NewToolsCallUsageLimitsInterceptor(repo, testenv.NewLogger(t))

	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-free",
		AccountType:          string(billing.TierBase),
	})
	call := newToolsCallRequestForInterceptor(t, ctx)

	err := interceptor.InterceptToolsCallRequest(ctx, call)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestToolsCallUsageLimitsInterceptor_ZeroIncludedUsesDefaultLimit(t *testing.T) {
	t.Parallel()

	// IncludedToolCalls=0 → hardLimit falls back to 2000. Usage at 2000
	// exceeds.
	repo := &fakeBillingRepo{
		storedUsage: &usage.PeriodUsage{
			ToolCalls:             2000,
			IncludedToolCalls:     0,
			HasActiveSubscription: false,
		},
		storedErr: nil,
	}
	interceptor := xmcp.NewToolsCallUsageLimitsInterceptor(repo, testenv.NewLogger(t))

	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-free",
		AccountType:          string(billing.TierBase),
	})
	call := newToolsCallRequestForInterceptor(t, ctx)

	err := interceptor.InterceptToolsCallRequest(ctx, call)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}
