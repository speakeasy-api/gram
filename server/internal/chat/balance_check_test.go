package chat

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// fakeBillingRepo embeds billing.Repository so any method we don't override
// panics with a nil-pointer dereference — making accidental calls loud.
type fakeBillingRepo struct {
	billing.Repository
	storedUsage *gen.PeriodUsage
	storedErr   error
}

func (f *fakeBillingRepo) GetStoredPeriodUsage(_ context.Context, _ string) (*gen.PeriodUsage, error) {
	return f.storedUsage, f.storedErr
}

func newServiceWithBilling(t *testing.T, repo billing.Repository) *Service {
	t.Helper()
	return &Service{
		logger:      testenv.NewLogger(t),
		tracer:      testenv.NewTracerProvider(t).Tracer("test"),
		billingRepo: repo,
	}
}

func TestCheckCreditBalance_AllowsWhenUnderIncluded(t *testing.T) {
	t.Parallel()

	svc := newServiceWithBilling(t, &fakeBillingRepo{
		storedUsage: &gen.PeriodUsage{Credits: 10, IncludedCredits: 100},
	})

	require.NoError(t, svc.checkCreditBalance(t.Context(), "org-1", "free"))
}

func TestCheckCreditBalance_RejectsWhenAtLimit(t *testing.T) {
	t.Parallel()

	svc := newServiceWithBilling(t, &fakeBillingRepo{
		storedUsage: &gen.PeriodUsage{Credits: 100, IncludedCredits: 100},
	})

	err := svc.checkCreditBalance(t.Context(), "org-1", "free")
	require.Error(t, err)
	var se *oops.ShareableError
	require.ErrorAs(t, err, &se)
	require.Equal(t, oops.CodeInsufficientCredits, se.Code)
}

func TestCheckCreditBalance_RejectsWhenOverLimit(t *testing.T) {
	t.Parallel()

	svc := newServiceWithBilling(t, &fakeBillingRepo{
		storedUsage: &gen.PeriodUsage{Credits: 250, IncludedCredits: 100},
	})

	err := svc.checkCreditBalance(t.Context(), "org-1", "free")
	require.Error(t, err)
	var se *oops.ShareableError
	require.ErrorAs(t, err, &se)
	require.Equal(t, oops.CodeInsufficientCredits, se.Code)
}

func TestCheckCreditBalance_AllowsWhenIncludedZero(t *testing.T) {
	t.Parallel()

	// IncludedCredits == 0 means no cap configured yet (fresh org, missing
	// product config). Don't block — let the request through and rely on
	// upstream OpenRouter key limit as the backstop.
	svc := newServiceWithBilling(t, &fakeBillingRepo{
		storedUsage: &gen.PeriodUsage{Credits: 0, IncludedCredits: 0},
	})

	require.NoError(t, svc.checkCreditBalance(t.Context(), "org-1", "free"))
}

func TestCheckCreditBalance_BypassesSpecialLimitOrgs(t *testing.T) {
	t.Parallel()

	// repo would error if called — special org should never reach it.
	repo := &fakeBillingRepo{storedErr: errors.New("must not be called")}
	svc := newServiceWithBilling(t, repo)

	require.NoError(t, svc.checkCreditBalance(t.Context(), "5a25158b-24dc-4d49-b03d-e85acfbea59c", "free"))
}

func TestCheckCreditBalance_BypassesPaidAccountTypes(t *testing.T) {
	t.Parallel()

	// Pro/enterprise are bounded by the OpenRouter key cap, not this gate
	// (Phase 0). Repo would error if called.
	repo := &fakeBillingRepo{storedErr: errors.New("must not be called")}
	svc := newServiceWithBilling(t, repo)

	require.NoError(t, svc.checkCreditBalance(t.Context(), "org-pro", "pro"))
	require.NoError(t, svc.checkCreditBalance(t.Context(), "org-ent", "enterprise"))
}

func TestCheckCreditBalance_AllowsOnCacheMiss(t *testing.T) {
	t.Parallel()

	// Fail-open: cache miss must NOT call live Polar from the hot path.
	// fakeBillingRepo doesn't implement GetPeriodUsage, so a fallback call
	// would nil-deref and fail this test.
	svc := newServiceWithBilling(t, &fakeBillingRepo{
		storedErr: errors.New("cache miss"),
	})

	require.NoError(t, svc.checkCreditBalance(t.Context(), "org-1", "free"))
}
