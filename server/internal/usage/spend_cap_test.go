package usage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/oops"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type captureSpendCapScheduler struct {
	operationID    string
	organizationID string
	limit          int
	actor          urn.Principal
	err            error
}

func (c *captureSpendCapScheduler) SetOpenRouterSpendCap(_ context.Context, operationID, organizationID string, limit int, actor urn.Principal, _ *string) error {
	c.operationID = operationID
	c.organizationID = organizationID
	c.limit = limit
	c.actor = actor
	return c.err
}

func TestSetSpendCapWaitsForDedicatedOperation(t *testing.T) {
	t.Parallel()

	organizationID := "org-spend-cap-success"
	service, db, _, _ := newTUMTestService(t, organizationID)
	setTestOrganizationAccountType(t, db, organizationID, billing.TierPayg)
	scheduler := &captureSpendCapScheduler{}
	service.keyRefresher = scheduler

	result, err := service.SetSpendCap(billingEmailAdminContext(t, organizationID), &gen.SetSpendCapPayload{MonthlyCredits: 600})
	require.NoError(t, err)
	require.Equal(t, 600, result.MonthlyCredits)
	require.NotEmpty(t, scheduler.operationID)
	require.Equal(t, organizationID, scheduler.organizationID)
	require.Equal(t, 600, scheduler.limit)
	require.Equal(t, "user-billing-email-admin", scheduler.actor.ID)
}

func TestSetSpendCapRejectsActiveTrial(t *testing.T) {
	t.Parallel()

	organizationID := "org-spend-cap-trial"
	service, db, _, _ := newTUMTestService(t, organizationID)
	setTestOrganizationAccountType(t, db, organizationID, billing.TierPayg)
	require.NoError(t, trialsrepo.New(db).CreateTrial(t.Context(), trialsrepo.CreateTrialParams{
		OrganizationID: organizationID,
		Tier:           string(billing.TierEnterprise),
		EndsAt:         pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true},
	}))
	service.keyRefresher = &captureSpendCapScheduler{}

	_, err := service.SetSpendCap(billingEmailAdminContext(t, organizationID), &gen.SetSpendCapPayload{MonthlyCredits: 200})
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestSetSpendCapRejectsNonPaygOrganization(t *testing.T) {
	t.Parallel()

	organizationID := "org-spend-cap-enterprise"
	service, db, _, _ := newTUMTestService(t, organizationID)
	setTestOrganizationAccountType(t, db, organizationID, billing.TierEnterprise)
	service.keyRefresher = &captureSpendCapScheduler{}

	_, err := service.SetSpendCap(billingEmailAdminContext(t, organizationID), &gen.SetSpendCapPayload{MonthlyCredits: 200})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestSetSpendCapRejectsNonAdmin(t *testing.T) {
	t.Parallel()

	organizationID := "org-spend-cap-member"
	service, db, _, _ := newTUMTestService(t, organizationID)
	setTestOrganizationAccountType(t, db, organizationID, billing.TierPayg)
	service.keyRefresher = &captureSpendCapScheduler{}
	ctx := authztest.WithExactGrants(t, billingEmailAdminContext(t, organizationID), authz.NewGrant(authz.ScopeOrgRead, organizationID))

	_, err := service.SetSpendCap(ctx, &gen.SetSpendCapPayload{MonthlyCredits: 200})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestSetSpendCapRejectsOutOfRangeValues(t *testing.T) {
	t.Parallel()

	organizationID := "org-spend-cap-range"
	service, db, _, _ := newTUMTestService(t, organizationID)
	setTestOrganizationAccountType(t, db, organizationID, billing.TierPayg)
	service.keyRefresher = &captureSpendCapScheduler{}

	for _, value := range []int{0, 10001} {
		_, err := service.SetSpendCap(billingEmailAdminContext(t, organizationID), &gen.SetSpendCapPayload{MonthlyCredits: value})
		requireOopsCode(t, err, oops.CodeInvalid)
	}
}

func TestSetSpendCapDoesNotAuditFailedScheduling(t *testing.T) {
	t.Parallel()

	organizationID := "org-spend-cap-schedule-failure"
	service, db, _, _ := newTUMTestService(t, organizationID)
	setTestOrganizationAccountType(t, db, organizationID, billing.TierPayg)
	service.keyRefresher = &captureSpendCapScheduler{err: errors.New("scheduler unavailable")}

	_, err := service.SetSpendCap(billingEmailAdminContext(t, organizationID), &gen.SetSpendCapPayload{MonthlyCredits: 200})
	requireOopsCode(t, err, oops.CodeUnexpected)
}
