package activities_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/email"
	modelkeysrepo "github.com/speakeasy-api/gram/server/internal/modelkeys/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	usagerepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
)

func setupOpenRouterCreditsAlertsTest(t *testing.T, dbName string) (*activities.MaybeSendOpenRouterCreditsAlerts, *pgxpool.Pool, *captureLoopsClient) {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, dbName)
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	captured := &captureLoopsClient{sent: nil, failNext: 0}
	act := activities.NewMaybeSendOpenRouterCreditsAlerts(
		testenv.NewLogger(t),
		conn,
		cache.NewRedisCacheAdapter(redisClient),
		email.NewService(testenv.NewLogger(t), captured),
	)

	return act, conn, captured
}

// createAlertOrg provisions an org with billing metadata. A non-empty
// alertEmail is stored as the billing alert contact; byok additionally attaches
// an enabled customer model provider key so the org reads as BYOK.
func createAlertOrg(t *testing.T, ctx context.Context, conn *pgxpool.Pool, alertEmail string, byok bool) (orgID, orgName string) {
	t.Helper()

	orgID = "org-" + uuid.NewString()[:8]
	orgName = "Test Org " + orgID
	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        orgName,
		Slug:        orgID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	_, err = usagerepo.New(conn).UpsertBillingMetadata(ctx, usagerepo.UpsertBillingMetadataParams{
		OrganizationID:         orgID,
		TumMonthlyTokenLimit:   pgtype.Int8{},
		AlertEmail:             pgtype.Text{String: alertEmail, Valid: alertEmail != ""},
		BillingCycleAnchorDay:  1,
		TunneledMcpServerLimit: pgtype.Int4{},
	})
	require.NoError(t, err)

	if byok {
		project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
			Name:           "Test Project",
			Slug:           "proj-" + uuid.NewString()[:8],
			OrganizationID: orgID,
		})
		require.NoError(t, err)

		_, err = modelkeysrepo.New(conn).InsertKey(ctx, modelkeysrepo.InsertKeyParams{
			OrganizationID:  orgID,
			ProjectID:       project.ID,
			Slot:            "default",
			Provider:        "openrouter",
			ApiKeyEncrypted: "encrypted",
			Enabled:         true,
		})
		require.NoError(t, err)
	}

	return orgID, orgName
}

func chatCreditsMetric(orgID string, used float64, limit int64) activities.OpenRouterCreditsMetric {
	return activities.OpenRouterCreditsMetric{
		OrganizationID:   orgID,
		OrganizationSlug: orgID,
		AccountType:      "enterprise",
		KeyType:          string(openrouter.KeyTypeChat),
		CreditsUsed:      used,
		CreditLimit:      limit,
	}
}

func TestMaybeSendOpenRouterCreditsAlerts_SendsHighestCrossedThreshold(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	act, conn, captured := setupOpenRouterCreditsAlertsTest(t, "openrouter_credits_alert_threshold")
	orgID, orgName := createAlertOrg(t, ctx, conn, "billing@example.com", false)

	// 92/100 crosses 50, 75, and 90 at once; only the highest fires.
	require.NoError(t, act.Do(ctx, []activities.OpenRouterCreditsMetric{chatCreditsMetric(orgID, 92, 100)}))

	sent := captured.Sent()
	require.Len(t, sent, 1)
	require.Equal(t, "billing@example.com", sent[0].Email)
	require.Equal(t, "90", sent[0].DataVariables["threshold_percent"])
	require.Equal(t, "false", sent[0].DataVariables["exhausted"])
	require.Equal(t, orgName, sent[0].DataVariables["organization_name"])

	// Re-running the same tick must not re-alert the same threshold.
	require.NoError(t, act.Do(ctx, []activities.OpenRouterCreditsMetric{chatCreditsMetric(orgID, 92, 100)}))
	require.Len(t, captured.Sent(), 1, "threshold alerts fire once per month")
}

func TestMaybeSendOpenRouterCreditsAlerts_ExhaustedFlagsExhaustion(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	act, conn, captured := setupOpenRouterCreditsAlertsTest(t, "openrouter_credits_alert_exhausted")
	orgID, _ := createAlertOrg(t, ctx, conn, "billing@example.com", false)

	require.NoError(t, act.Do(ctx, []activities.OpenRouterCreditsMetric{chatCreditsMetric(orgID, 100, 100)}))

	sent := captured.Sent()
	require.Len(t, sent, 1)
	require.Equal(t, "100", sent[0].DataVariables["threshold_percent"])
	require.Equal(t, "true", sent[0].DataVariables["exhausted"])
}

func TestMaybeSendOpenRouterCreditsAlerts_IgnoresInternalKey(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	act, conn, captured := setupOpenRouterCreditsAlertsTest(t, "openrouter_credits_alert_internal")
	orgID, _ := createAlertOrg(t, ctx, conn, "billing@example.com", false)

	internal := chatCreditsMetric(orgID, 100, 100)
	internal.KeyType = string(openrouter.KeyTypeInternal)
	require.NoError(t, act.Do(ctx, []activities.OpenRouterCreditsMetric{internal}))

	require.Empty(t, captured.Sent(), "only the chat key drives customer-facing alerts")
}

func TestMaybeSendOpenRouterCreditsAlerts_SkipsWithoutAlertEmail(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	act, conn, captured := setupOpenRouterCreditsAlertsTest(t, "openrouter_credits_alert_no_email")
	orgID, _ := createAlertOrg(t, ctx, conn, "", false)

	require.NoError(t, act.Do(ctx, []activities.OpenRouterCreditsMetric{chatCreditsMetric(orgID, 95, 100)}))
	require.Empty(t, captured.Sent(), "no billing alert contact means no email")
}

func TestMaybeSendOpenRouterCreditsAlerts_SkipsBYOK(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	act, conn, captured := setupOpenRouterCreditsAlertsTest(t, "openrouter_credits_alert_byok")
	orgID, _ := createAlertOrg(t, ctx, conn, "billing@example.com", true)

	require.NoError(t, act.Do(ctx, []activities.OpenRouterCreditsMetric{chatCreditsMetric(orgID, 95, 100)}))
	require.Empty(t, captured.Sent(), "BYOK orgs do not depend on the platform chat key")
}

func TestMaybeSendOpenRouterCreditsAlerts_SkipsBelowLowestThreshold(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	act, conn, captured := setupOpenRouterCreditsAlertsTest(t, "openrouter_credits_alert_below")
	orgID, _ := createAlertOrg(t, ctx, conn, "billing@example.com", false)

	require.NoError(t, act.Do(ctx, []activities.OpenRouterCreditsMetric{chatCreditsMetric(orgID, 40, 100)}))
	require.Empty(t, captured.Sent(), "usage below 50%% crosses no threshold")
}

func TestMaybeSendOpenRouterCreditsAlerts_RetriesAfterSendFailure(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	act, conn, captured := setupOpenRouterCreditsAlertsTest(t, "openrouter_credits_alert_retry")
	orgID, _ := createAlertOrg(t, ctx, conn, "billing@example.com", false)

	// The first send fails; the reservation must be released so the next run
	// retries instead of silently dropping the alert.
	captured.FailNext(1)
	require.NoError(t, act.Do(ctx, []activities.OpenRouterCreditsMetric{chatCreditsMetric(orgID, 95, 100)}))
	require.Empty(t, captured.Sent())

	require.NoError(t, act.Do(ctx, []activities.OpenRouterCreditsMetric{chatCreditsMetric(orgID, 95, 100)}))
	sent := captured.Sent()
	require.Len(t, sent, 1, "the alert is retried on the next run")
	require.Equal(t, "90", sent[0].DataVariables["threshold_percent"])
}
