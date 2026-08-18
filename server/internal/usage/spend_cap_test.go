package usage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	openrouterrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
)

type captureSpendCapScheduler struct {
	operationID    string
	organizationID string
	keyType        openrouter.KeyType
	limit          int
	actor          urn.Principal
	err            error
}

type captureInferenceCredits struct {
	openrouter.Provisioner
	mu    sync.Mutex
	calls []openrouter.KeyType
}

func (c *captureInferenceCredits) GetCreditsUsed(_ context.Context, _ string, keyType openrouter.KeyType) (float64, int, error) {
	c.mu.Lock()
	c.calls = append(c.calls, keyType)
	c.mu.Unlock()
	switch keyType {
	case openrouter.KeyTypeChat:
		return 25.5, 100, nil
	case openrouter.KeyTypeInternal:
		return 7.25, 50, nil
	default:
		return 0, 0, errors.New("unexpected key type")
	}
}

func (c *captureSpendCapScheduler) SetOpenRouterSpendCap(_ context.Context, operationID, organizationID string, keyType openrouter.KeyType, limit int, actor urn.Principal, _ *string) error {
	c.operationID = operationID
	c.organizationID = organizationID
	c.keyType = keyType
	c.limit = limit
	c.actor = actor
	return c.err
}

func createUsageInferenceKey(t *testing.T, db repo.DBTX, organizationID string, keyType openrouter.KeyType, monthlyCredits int64) {
	t.Helper()
	_, err := openrouterrepo.New(db).CreateOpenRouterAPIKey(t.Context(), openrouterrepo.CreateOpenRouterAPIKeyParams{
		OrganizationID: organizationID,
		KeyType:        string(keyType),
		KeyEncrypted:   pgtype.Text{},
		KeyHash:        "hash_placeholder_" + string(keyType),
		MonthlyCredits: monthlyCredits,
	})
	require.NoError(t, err)
}

func TestSetSpendCapWaitsForDedicatedOperation(t *testing.T) {
	t.Parallel()

	organizationID := "org-spend-cap-success"
	service, db, _, _ := newTUMTestService(t, organizationID)
	setTestOrganizationAccountType(t, db, organizationID, billing.TierPayg)
	createUsageInferenceKey(t, db, organizationID, openrouter.KeyTypeChat, 100)
	scheduler := &captureSpendCapScheduler{}
	service.keyRefresher = scheduler

	result, err := service.SetSpendCap(billingEmailAdminContext(t, organizationID), &gen.SetSpendCapPayload{MonthlyCredits: 600})
	require.NoError(t, err)
	require.Equal(t, 600, result.MonthlyCredits)
	require.NotEmpty(t, scheduler.operationID)
	require.Equal(t, organizationID, scheduler.organizationID)
	require.Equal(t, openrouter.KeyTypeChat, scheduler.keyType)
	require.Equal(t, 600, scheduler.limit)
	require.Equal(t, "user-billing-email-admin", scheduler.actor.ID)
}

func TestSetSpendCapTargetsSecurityInferenceKey(t *testing.T) {
	t.Parallel()

	organizationID := "org-security-inference-cap"
	service, db, _, _ := newTUMTestService(t, organizationID)
	setTestOrganizationAccountType(t, db, organizationID, billing.TierPayg)
	createUsageInferenceKey(t, db, organizationID, openrouter.KeyTypeInternal, 50)
	scheduler := &captureSpendCapScheduler{}
	service.keyRefresher = scheduler
	keyType := string(openrouter.KeyTypeInternal)

	result, err := service.SetSpendCap(billingEmailAdminContext(t, organizationID), &gen.SetSpendCapPayload{
		KeyType:        &keyType,
		MonthlyCredits: 75,
	})
	require.NoError(t, err)
	require.Equal(t, string(openrouter.KeyTypeInternal), result.KeyType)
	require.Equal(t, openrouter.KeyTypeInternal, scheduler.keyType)
	require.Equal(t, 75, scheduler.limit)
}

func TestGetInferenceSpendCapsListsMaterializedPlatformKeys(t *testing.T) {
	t.Parallel()

	organizationID := "org-inference-cap-list"
	service, db, _, _ := newTUMTestService(t, organizationID)
	createUsageInferenceKey(t, db, organizationID, openrouter.KeyTypeChat, 100)
	createUsageInferenceKey(t, db, organizationID, openrouter.KeyTypeInternal, 50)
	credits := &captureInferenceCredits{Provisioner: openrouter.NewDevelopment("key_placeholder")}
	service.openRouter = credits

	result, err := service.GetInferenceSpendCaps(
		authztest.WithExactGrants(t, billingEmailAdminContext(t, organizationID), authz.NewGrant(authz.ScopeOrgRead, organizationID)),
		&gen.GetInferenceSpendCapsPayload{},
	)
	require.NoError(t, err)
	require.Equal(t, []*gen.InferenceSpendCap{
		{KeyType: "chat", CreditsUsed: 25.5, MonthlyCredits: 100, Disabled: false},
		{KeyType: "internal", CreditsUsed: 7.25, MonthlyCredits: 50, Disabled: false},
	}, result)
	credits.mu.Lock()
	defer credits.mu.Unlock()
	require.ElementsMatch(t, []openrouter.KeyType{openrouter.KeyTypeChat, openrouter.KeyTypeInternal}, credits.calls)
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

func TestSetSpendCapRejectsAbsentTargetKey(t *testing.T) {
	t.Parallel()

	organizationID := "org-spend-cap-absent"
	service, db, _, _ := newTUMTestService(t, organizationID)
	setTestOrganizationAccountType(t, db, organizationID, billing.TierPayg)
	scheduler := &captureSpendCapScheduler{}
	service.keyRefresher = scheduler

	_, err := service.SetSpendCap(billingEmailAdminContext(t, organizationID), &gen.SetSpendCapPayload{MonthlyCredits: 200})
	requireOopsCode(t, err, oops.CodeNotFound)
	require.Empty(t, scheduler.operationID)
}

func TestSetSpendCapRejectsDisabledTargetKey(t *testing.T) {
	t.Parallel()

	organizationID := "org-spend-cap-disabled"
	service, db, _, _ := newTUMTestService(t, organizationID)
	setTestOrganizationAccountType(t, db, organizationID, billing.TierPayg)
	createUsageInferenceKey(t, db, organizationID, openrouter.KeyTypeChat, 100)
	require.NoError(t, openrouterrepo.New(db).DisableOpenRouterAPIKey(t.Context(), openrouterrepo.DisableOpenRouterAPIKeyParams{
		OrganizationID: organizationID,
		KeyType:        string(openrouter.KeyTypeChat),
	}))
	scheduler := &captureSpendCapScheduler{}
	service.keyRefresher = scheduler

	_, err := service.SetSpendCap(billingEmailAdminContext(t, organizationID), &gen.SetSpendCapPayload{MonthlyCredits: 200})
	requireOopsCode(t, err, oops.CodeConflict)
	require.Empty(t, scheduler.operationID)
}

func TestSetSpendCapDoesNotAuditFailedScheduling(t *testing.T) {
	t.Parallel()

	organizationID := "org-spend-cap-schedule-failure"
	service, db, _, _ := newTUMTestService(t, organizationID)
	setTestOrganizationAccountType(t, db, organizationID, billing.TierPayg)
	createUsageInferenceKey(t, db, organizationID, openrouter.KeyTypeChat, 100)
	service.keyRefresher = &captureSpendCapScheduler{err: errors.New("scheduler unavailable")}

	_, err := service.SetSpendCap(billingEmailAdminContext(t, organizationID), &gen.SetSpendCapPayload{MonthlyCredits: 200})
	requireOopsCode(t, err, oops.CodeUnexpected)
}
