package activities_test

import (
	"context"
	"fmt"
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

func setupOpenRouterCreditsAlertsTest(t *testing.T, dbName string) (*activities.MaybeSendOpenRouterCreditsAlerts, *pgxpool.Pool, *captureLoopsClient, cache.Cache) {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, dbName)
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	cacheAdapter := cache.NewRedisCacheAdapter(redisClient)
	captured := &captureLoopsClient{sent: nil, failNext: 0}
	act := activities.NewMaybeSendOpenRouterCreditsAlerts(
		testenv.NewLogger(t),
		conn,
		cacheAdapter,
		email.NewService(testenv.NewLogger(t), captured),
		testenv.NewMeterProvider(t),
	)

	return act, conn, captured, cacheAdapter
}

// createAlertOrg provisions an org with billing metadata. A non-empty
// alertEmail is stored as the billing alert contact; a non-empty byokSlot
// additionally attaches an enabled customer model provider key in that slot.
func createAlertOrg(t *testing.T, ctx context.Context, conn *pgxpool.Pool, alertEmail string, byokSlot string) (orgID, orgName string) {
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

	if byokSlot != "" {
		project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
			Name:           "Test Project",
			Slug:           "proj-" + uuid.NewString()[:8],
			OrganizationID: orgID,
		})
		require.NoError(t, err)

		_, err = modelkeysrepo.New(conn).InsertKey(ctx, modelkeysrepo.InsertKeyParams{
			OrganizationID:  orgID,
			ProjectID:       project.ID,
			Slot:            byokSlot,
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

// deleteAlertReservation simulates the dedup reservation's TTL expiring by
// removing the key directly. The key format mirrors the activity's; orgIDs are
// unique per test so this cannot clobber a sibling test's reservations.
func deleteAlertReservation(t *testing.T, ctx context.Context, cacheAdapter cache.Cache, orgID string, keyType openrouter.KeyType, threshold int) {
	t.Helper()
	key := fmt.Sprintf("openrouter-credits-alert:%s:%s:%d", orgID, keyType, threshold)
	require.NoError(t, cacheAdapter.Delete(ctx, key))
}

func internalCreditsMetric(orgID string, used float64, limit int64) activities.OpenRouterCreditsMetric {
	m := chatCreditsMetric(orgID, used, limit)
	m.KeyType = string(openrouter.KeyTypeInternal)
	return m
}

// Template IDs distinguish which email family a captured send used.
var (
	chatCreditsTemplateID     = string(email.OpenRouterChatCreditsThreshold{}.TransactionalID())
	internalCreditsTemplateID = string(email.OpenRouterInternalCreditsThreshold{}.TransactionalID())
)

func TestMaybeSendOpenRouterCreditsAlerts_SendsHighestCrossedThreshold(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	act, conn, captured, _ := setupOpenRouterCreditsAlertsTest(t, "openrouter_credits_alert_threshold")
	orgID, orgName := createAlertOrg(t, ctx, conn, "billing@example.com", "")

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

	act, conn, captured, _ := setupOpenRouterCreditsAlertsTest(t, "openrouter_credits_alert_exhausted")
	orgID, _ := createAlertOrg(t, ctx, conn, "billing@example.com", "")

	require.NoError(t, act.Do(ctx, []activities.OpenRouterCreditsMetric{chatCreditsMetric(orgID, 100, 100)}))

	sent := captured.Sent()
	require.Len(t, sent, 1)
	require.Equal(t, "100", sent[0].DataVariables["threshold_percent"])
	require.Equal(t, "true", sent[0].DataVariables["exhausted"])
}

func TestMaybeSendOpenRouterCreditsAlerts_InternalKeyUsesInternalTemplate(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	act, conn, captured, _ := setupOpenRouterCreditsAlertsTest(t, "openrouter_credits_alert_internal")
	orgID, _ := createAlertOrg(t, ctx, conn, "billing@example.com", "")

	require.NoError(t, act.Do(ctx, []activities.OpenRouterCreditsMetric{internalCreditsMetric(orgID, 100, 100)}))

	sent := captured.Sent()
	require.Len(t, sent, 1, "the internal key alerts with its own email family")
	require.Equal(t, internalCreditsTemplateID, sent[0].TransactionalID)
	require.Equal(t, "100", sent[0].DataVariables["threshold_percent"])
	require.Equal(t, "true", sent[0].DataVariables["exhausted"])
}

func TestMaybeSendOpenRouterCreditsAlerts_KeyTypesAlertIndependently(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	act, conn, captured, _ := setupOpenRouterCreditsAlertsTest(t, "openrouter_credits_alert_both_keys")
	orgID, _ := createAlertOrg(t, ctx, conn, "billing@example.com", "")

	// Both keys cross a threshold in the same tick: one email per key, each
	// from its own template, deduped independently.
	require.NoError(t, act.Do(ctx, []activities.OpenRouterCreditsMetric{
		chatCreditsMetric(orgID, 92, 100),
		internalCreditsMetric(orgID, 60, 100),
	}))

	sent := captured.Sent()
	require.Len(t, sent, 2, "each key type alerts separately")
	byTemplate := map[string]string{}
	for _, s := range sent {
		byTemplate[s.TransactionalID] = s.DataVariables["threshold_percent"]
	}
	require.Equal(t, map[string]string{
		chatCreditsTemplateID:     "90",
		internalCreditsTemplateID: "50",
	}, byTemplate)

	// Re-running changes nothing; the internal key climbing to a new threshold
	// alerts again without re-alerting the chat key.
	require.NoError(t, act.Do(ctx, []activities.OpenRouterCreditsMetric{
		chatCreditsMetric(orgID, 92, 100),
		internalCreditsMetric(orgID, 60, 100),
	}))
	require.Len(t, captured.Sent(), 2)

	require.NoError(t, act.Do(ctx, []activities.OpenRouterCreditsMetric{
		chatCreditsMetric(orgID, 92, 100),
		internalCreditsMetric(orgID, 80, 100),
	}))
	sent = captured.Sent()
	require.Len(t, sent, 3, "the internal key's next threshold fires independently")
	require.Equal(t, internalCreditsTemplateID, sent[2].TransactionalID)
	require.Equal(t, "75", sent[2].DataVariables["threshold_percent"])
}

func TestMaybeSendOpenRouterCreditsAlerts_SkipsWithoutAlertEmail(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	act, conn, captured, _ := setupOpenRouterCreditsAlertsTest(t, "openrouter_credits_alert_no_email")
	orgID, _ := createAlertOrg(t, ctx, conn, "", "")

	require.NoError(t, act.Do(ctx, []activities.OpenRouterCreditsMetric{chatCreditsMetric(orgID, 95, 100)}))
	require.Empty(t, captured.Sent(), "no billing alert contact means no email")
}

func TestMaybeSendOpenRouterCreditsAlerts_ChatBYOKSuppressesOnlyChatAlerts(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	act, conn, captured, _ := setupOpenRouterCreditsAlertsTest(t, "openrouter_credits_alert_byok")
	orgID, _ := createAlertOrg(t, ctx, conn, "billing@example.com", "default")

	// A chat-serving customer key suppresses chat-key warnings, but internal
	// platform-key usage is platform-billed regardless, so its alert still
	// goes out.
	require.NoError(t, act.Do(ctx, []activities.OpenRouterCreditsMetric{
		chatCreditsMetric(orgID, 95, 100),
		internalCreditsMetric(orgID, 55, 100),
	}))

	sent := captured.Sent()
	require.Len(t, sent, 1, "chat alert suppressed, internal alert delivered")
	require.Equal(t, internalCreditsTemplateID, sent[0].TransactionalID)
	require.Equal(t, "50", sent[0].DataVariables["threshold_percent"])
}

func TestMaybeSendOpenRouterCreditsAlerts_InternalOnlyBYOKSlotStillAlerts(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	act, conn, captured, _ := setupOpenRouterCreditsAlertsTest(t, "openrouter_credits_alert_internal_slot")
	// A customer key on an internal-only judge slot never pays for chat
	// completions, so the org still depends on the platform chat key and must
	// keep receiving warnings.
	orgID, _ := createAlertOrg(t, ctx, conn, "billing@example.com", "risk-policy")

	require.NoError(t, act.Do(ctx, []activities.OpenRouterCreditsMetric{chatCreditsMetric(orgID, 95, 100)}))

	sent := captured.Sent()
	require.Len(t, sent, 1, "internal-only BYOK slots must not suppress chat-key alerts")
	require.Equal(t, "90", sent[0].DataVariables["threshold_percent"])
}

func TestMaybeSendOpenRouterCreditsAlerts_SkipsBelowLowestThreshold(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	act, conn, captured, _ := setupOpenRouterCreditsAlertsTest(t, "openrouter_credits_alert_below")
	orgID, _ := createAlertOrg(t, ctx, conn, "billing@example.com", "")

	require.NoError(t, act.Do(ctx, []activities.OpenRouterCreditsMetric{chatCreditsMetric(orgID, 40, 100)}))
	require.Empty(t, captured.Sent(), "usage below 50%% crosses no threshold")
}

func TestMaybeSendOpenRouterCreditsAlerts_RetriesAfterSendFailureWithBackoff(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	act, conn, captured, cacheAdapter := setupOpenRouterCreditsAlertsTest(t, "openrouter_credits_alert_retry")
	orgID, _ := createAlertOrg(t, ctx, conn, "billing@example.com", "")

	// The first send fails. The short reservation is kept as a backoff, so an
	// immediate re-run must NOT hammer the provider again.
	captured.FailNext(1)
	require.NoError(t, act.Do(ctx, []activities.OpenRouterCreditsMetric{chatCreditsMetric(orgID, 95, 100)}))
	require.Empty(t, captured.Sent(), "failed send records nothing")

	require.NoError(t, act.Do(ctx, []activities.OpenRouterCreditsMetric{chatCreditsMetric(orgID, 95, 100)}))
	require.Empty(t, captured.Sent(), "the retry waits for the backoff reservation to expire")

	// Once the reservation lapses (simulated), the next tick retries the send.
	deleteAlertReservation(t, ctx, cacheAdapter, orgID, openrouter.KeyTypeChat, 90)
	require.NoError(t, act.Do(ctx, []activities.OpenRouterCreditsMetric{chatCreditsMetric(orgID, 95, 100)}))
	sent := captured.Sent()
	require.Len(t, sent, 1, "the alert is retried after the backoff expires")
	require.Equal(t, "90", sent[0].DataVariables["threshold_percent"])
}

func TestMaybeSendOpenRouterCreditsAlerts_IneligibleOrgRecheckedAfterReservationExpiry(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	act, conn, captured, cacheAdapter := setupOpenRouterCreditsAlertsTest(t, "openrouter_credits_alert_late_email")
	orgID, _ := createAlertOrg(t, ctx, conn, "", "")

	// No alert email configured: the tick reserves, finds no recipient, and
	// keeps the reservation as a negative marker.
	require.NoError(t, act.Do(ctx, []activities.OpenRouterCreditsMetric{chatCreditsMetric(orgID, 95, 100)}))
	require.Empty(t, captured.Sent())

	// The admin configures an alert email. The held reservation defers the
	// re-check until it expires; after that the alert goes out.
	_, err := usagerepo.New(conn).UpsertBillingMetadata(ctx, usagerepo.UpsertBillingMetadataParams{
		OrganizationID:         orgID,
		TumMonthlyTokenLimit:   pgtype.Int8{},
		AlertEmail:             pgtype.Text{String: "billing@example.com", Valid: true},
		BillingCycleAnchorDay:  1,
		TunneledMcpServerLimit: pgtype.Int4{},
	})
	require.NoError(t, err)

	require.NoError(t, act.Do(ctx, []activities.OpenRouterCreditsMetric{chatCreditsMetric(orgID, 95, 100)}))
	require.Empty(t, captured.Sent(), "held reservation defers the re-check")

	deleteAlertReservation(t, ctx, cacheAdapter, orgID, openrouter.KeyTypeChat, 90)
	require.NoError(t, act.Do(ctx, []activities.OpenRouterCreditsMetric{chatCreditsMetric(orgID, 95, 100)}))
	require.Len(t, captured.Sent(), 1, "alert sent once the reservation lapses")
}
