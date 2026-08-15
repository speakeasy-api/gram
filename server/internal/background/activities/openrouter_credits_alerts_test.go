package activities_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/email"
	modelkeysrepo "github.com/speakeasy-api/gram/server/internal/modelkeys/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usagerepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func setupOpenRouterCreditsAlertsTest(t *testing.T, dbName string) (*activities.MaybeSendOpenRouterCreditsAlerts, *pgxpool.Pool, *captureLoopsClient, cache.Cache) {
	t.Helper()
	return setupOpenRouterCreditsAlertsTestWithCache(t, dbName, nil)
}

func setupOpenRouterCreditsAlertsTestWithCache(
	t *testing.T,
	dbName string,
	wrap func(cache.Cache) cache.Cache,
) (*activities.MaybeSendOpenRouterCreditsAlerts, *pgxpool.Pool, *captureLoopsClient, cache.Cache) {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, dbName)
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	var cacheAdapter cache.Cache = cache.NewRedisCacheAdapter(redisClient)
	if wrap != nil {
		cacheAdapter = wrap(cacheAdapter)
	}
	captured := &captureLoopsClient{sent: nil, failNext: 0}
	act := activities.NewMaybeSendOpenRouterCreditsAlerts(
		testenv.NewLogger(t),
		conn,
		cacheAdapter,
		email.NewService(testenv.NewLogger(t), captured, email.NewTemplateIDs(map[string]string{
			"openrouter_chat_credits_threshold":     "chat-credits-test-id",
			"openrouter_internal_credits_threshold": "internal-credits-test-id",
		}), true),
		testenv.NewMeterProvider(t),
	)

	return act, conn, captured, cacheAdapter
}

type captureAlertExpireCache struct {
	cache.Cache
	mu      sync.Mutex
	expires map[string]time.Duration
}

func (c *captureAlertExpireCache) Expire(ctx context.Context, key string, ttl time.Duration) error {
	c.mu.Lock()
	c.expires[key] = ttl
	c.mu.Unlock()
	if err := c.Cache.Expire(ctx, key, ttl); err != nil {
		return fmt.Errorf("expire captured alert key: %w", err)
	}
	return nil
}

func (c *captureAlertExpireCache) ttl(key string) (time.Duration, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ttl, ok := c.expires[key]
	return ttl, ok
}

// createAlertOrg provisions an org with billing metadata. A non-empty
// alertEmail is stored as the billing alert contact; a non-empty byokSlot
// additionally attaches an enabled customer model provider key in that slot.
func createAlertOrg(t *testing.T, ctx context.Context, conn *pgxpool.Pool, alertEmail string, byokSlot string) (orgID, orgName string) {
	t.Helper()
	return createAlertOrgWithAccountType(t, ctx, conn, alertEmail, byokSlot, billing.TierEnterprise)
}

func createAlertOrgWithAccountType(t *testing.T, ctx context.Context, conn *pgxpool.Pool, alertEmail string, byokSlot string, accountType billing.Tier) (orgID, orgName string) {
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
	require.NoError(t, orgrepo.New(conn).SetAccountType(ctx, orgrepo.SetAccountTypeParams{
		GramAccountType: string(accountType),
		ID:              orgID,
	}))

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

func seedAlertAdminRecipient(t *testing.T, conn *pgxpool.Pool, organizationID, userID, adminEmail string) {
	t.Helper()

	_, err := usersrepo.New(conn).UpsertUser(t.Context(), usersrepo.UpsertUserParams{
		ID:          userID,
		Email:       adminEmail,
		DisplayName: userID,
		PhotoUrl:    pgtype.Text{},
		Admin:       false,
	})
	require.NoError(t, err)
	require.NoError(t, testrepo.New(conn).CreateOrganizationUserRelationshipFixture(t.Context(), testrepo.CreateOrganizationUserRelationshipFixtureParams{
		OrganizationID: organizationID,
		UserID:         conv.ToPGText(userID),
	}))
	selectors, err := authz.NewSelector(authz.ScopeOrgAdmin, organizationID).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(conn).UpsertPrincipalGrant(t.Context(), accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: organizationID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeUser, userID),
		Scope:          string(authz.ScopeOrgAdmin),
		Selectors:      selectors,
	})
	require.NoError(t, err)
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

func setAlertGeneration(t *testing.T, ctx context.Context, cacheAdapter cache.Cache, orgID string, generation string) {
	t.Helper()
	key := fmt.Sprintf("openrouter-credits-alert-generation:%s:%s", orgID, openrouter.KeyTypeChat)
	require.NoError(t, cacheAdapter.Set(ctx, key, generation, 24*time.Hour))
}

func internalCreditsMetric(orgID string, used float64, limit int64) activities.OpenRouterCreditsMetric {
	m := chatCreditsMetric(orgID, used, limit)
	m.KeyType = string(openrouter.KeyTypeInternal)
	return m
}

func paygCreditsMetric(orgID string, used float64, limit int64) activities.OpenRouterCreditsMetric {
	m := chatCreditsMetric(orgID, used, limit)
	m.AccountType = string(billing.TierPayg)
	return m
}

// Template IDs distinguish which email family a captured send used.
var (
	chatCreditsTemplateID     = "chat-credits-test-id"
	internalCreditsTemplateID = "internal-credits-test-id"
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

func TestMaybeSendOpenRouterCreditsAlerts_ExtendsGenerationWithReservation(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	var capturedCache *captureAlertExpireCache
	act, conn, _, cacheAdapter := setupOpenRouterCreditsAlertsTestWithCache(t, "openrouter_credits_alert_generation_ttl", func(base cache.Cache) cache.Cache {
		capturedCache = &captureAlertExpireCache{Cache: base, expires: map[string]time.Duration{}}
		return capturedCache
	})
	orgID, _ := createAlertOrg(t, ctx, conn, "billing@example.com", "")
	setAlertGeneration(t, ctx, cacheAdapter, orgID, "operation_placeholder")

	require.NoError(t, act.Do(ctx, []activities.OpenRouterCreditsMetric{chatCreditsMetric(orgID, 60, 100)}))

	reservationKey := fmt.Sprintf("openrouter-credits-alert:%s:chat:50:operation_placeholder", orgID)
	reservationTTL, ok := capturedCache.ttl(reservationKey)
	require.True(t, ok)
	generationTTL, ok := capturedCache.ttl(spendCapGenerationKey(orgID))
	require.True(t, ok)
	require.Equal(t, reservationTTL, generationTTL)
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

func TestMaybeSendOpenRouterCreditsAlerts_CapChangeRearmsAdjustedLadder(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	act, conn, captured, cacheAdapter := setupOpenRouterCreditsAlertsTest(t, "openrouter_credits_alert_cap_rearm")
	orgID, _ := createAlertOrgWithAccountType(t, ctx, conn, "billing@example.test", "", billing.TierPayg)

	require.NoError(t, act.Do(ctx, []activities.OpenRouterCreditsMetric{paygCreditsMetric(orgID, 95, 100)}))
	setAlertGeneration(t, ctx, cacheAdapter, orgID, "operation_raised_cap_placeholder")

	// The raised 200-credit cap drops current usage below the first threshold.
	// Each threshold then fires once as usage crosses the adjusted ladder.
	for _, used := range []float64{99, 100, 150, 180, 200, 200} {
		require.NoError(t, act.Do(ctx, []activities.OpenRouterCreditsMetric{paygCreditsMetric(orgID, used, 200)}))
	}

	sent := captured.Sent()
	require.Len(t, sent, 5)
	thresholds := make([]string, 0, len(sent))
	for _, message := range sent {
		thresholds = append(thresholds, message.DataVariables["threshold_percent"])
	}
	require.Equal(t, []string{"90", "50", "75", "90", "100"}, thresholds)
	require.NotEqual(t, sent[0].IdempotencyKey, sent[3].IdempotencyKey,
		"the provider must accept the re-armed 90%% alert within its idempotency window")
}

func TestMaybeSendOpenRouterCreditsAlerts_ReconcileLimitFlapDoesNotRearm(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	act, conn, captured, cacheAdapter := setupOpenRouterCreditsAlertsTest(t, "openrouter_credits_alert_limit_flap")
	orgID, _ := createAlertOrgWithAccountType(t, ctx, conn, "billing@example.test", "", billing.TierPayg)
	setAlertGeneration(t, ctx, cacheAdapter, orgID, "operation_stable_cap_placeholder")

	require.NoError(t, act.Do(ctx, []activities.OpenRouterCreditsMetric{paygCreditsMetric(orgID, 95, 100)}))
	require.NoError(t, act.Do(ctx, []activities.OpenRouterCreditsMetric{paygCreditsMetric(orgID, 95, 200)}))
	require.NoError(t, act.Do(ctx, []activities.OpenRouterCreditsMetric{paygCreditsMetric(orgID, 95, 100)}))

	require.Len(t, captured.Sent(), 1, "the same generation must survive an upstream/local limit flap")
}

func TestMaybeSendOpenRouterCreditsAlerts_SkipsWithoutAlertEmail(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	act, conn, captured, _ := setupOpenRouterCreditsAlertsTest(t, "openrouter_credits_alert_no_email")
	orgID, _ := createAlertOrg(t, ctx, conn, "", "")

	require.NoError(t, act.Do(ctx, []activities.OpenRouterCreditsMetric{chatCreditsMetric(orgID, 95, 100)}))
	require.Empty(t, captured.Sent(), "no billing alert contact means no email")
}

func TestMaybeSendOpenRouterCreditsAlerts_PAYGWithoutExplicitEmailSendsAllEffectiveAdmins(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	act, conn, captured, _ := setupOpenRouterCreditsAlertsTest(t, "openrouter_credits_alert_payg_admins")
	orgID, _ := createAlertOrgWithAccountType(t, ctx, conn, "", "", billing.TierPayg)
	seedAlertAdminRecipient(t, conn, orgID, "admin-beta", "beta@example.test")
	seedAlertAdminRecipient(t, conn, orgID, "admin-alpha", "alpha@example.test")

	require.NoError(t, act.Do(ctx, []activities.OpenRouterCreditsMetric{paygCreditsMetric(orgID, 95, 100)}))

	sent := captured.Sent()
	require.Len(t, sent, 2)
	require.Equal(t, "alpha@example.test", sent[0].Email)
	require.Equal(t, "beta@example.test", sent[1].Email)
	require.NotEqual(t, sent[0].IdempotencyKey, sent[1].IdempotencyKey)
}

func TestMaybeSendOpenRouterCreditsAlerts_PAYGExplicitEmailOverridesAdmins(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	act, conn, captured, _ := setupOpenRouterCreditsAlertsTest(t, "openrouter_credits_alert_payg_explicit")
	orgID, _ := createAlertOrgWithAccountType(t, ctx, conn, "billing@example.test", "", billing.TierPayg)
	seedAlertAdminRecipient(t, conn, orgID, "admin-unused", "admin@example.test")

	require.NoError(t, act.Do(ctx, []activities.OpenRouterCreditsMetric{paygCreditsMetric(orgID, 95, 100)}))

	sent := captured.Sent()
	require.Len(t, sent, 1)
	require.Equal(t, "billing@example.test", sent[0].Email)
}

func TestMaybeSendOpenRouterCreditsAlerts_PartialPAYGAudienceRetriesOnlyFailedRecipients(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	act, conn, captured, cacheAdapter := setupOpenRouterCreditsAlertsTest(t, "openrouter_credits_alert_payg_partial_retry")
	orgID, _ := createAlertOrgWithAccountType(t, ctx, conn, "", "", billing.TierPayg)
	seedAlertAdminRecipient(t, conn, orgID, "admin-alpha", "alpha@example.test")
	seedAlertAdminRecipient(t, conn, orgID, "admin-beta", "beta@example.test")
	captured.FailNext(1)

	require.NoError(t, act.Do(ctx, []activities.OpenRouterCreditsMetric{paygCreditsMetric(orgID, 95, 100)}))
	firstAttempt := captured.Sent()
	require.Len(t, firstAttempt, 1)
	require.Equal(t, "beta@example.test", firstAttempt[0].Email)

	// The org reservation remains at its short retry TTL because one recipient
	// failed; an immediate workflow tick does not hammer Loops.
	require.NoError(t, act.Do(ctx, []activities.OpenRouterCreditsMetric{paygCreditsMetric(orgID, 95, 100)}))
	require.Len(t, captured.Sent(), 1)

	deleteAlertReservation(t, ctx, cacheAdapter, orgID, openrouter.KeyTypeChat, 90)
	require.NoError(t, act.Do(ctx, []activities.OpenRouterCreditsMetric{paygCreditsMetric(orgID, 95, 100)}))
	retry := captured.Sent()
	require.Len(t, retry, 2)
	require.Equal(t, "alpha@example.test", retry[1].Email)
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
