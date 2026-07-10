package activities_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/cache"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	usagerepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
)

type groupIdentifyCall struct {
	groupType  string
	groupKey   string
	properties map[string]any
}

type groupEventCall struct {
	event      string
	distinctID string
	groups     map[string]string
	properties map[string]any
}

// capturePosthogClient records the forwarded payloads so tests can assert on
// exactly what would reach PostHog.
type capturePosthogClient struct {
	mu         sync.Mutex
	identifies []groupIdentifyCall
	events     []groupEventCall
	// failNextCaptures makes that many CaptureGroupEvent calls fail without
	// recording, simulating transient PostHog enqueue failures.
	failNextCaptures int
}

func (c *capturePosthogClient) GroupIdentify(_ context.Context, groupType string, groupKey string, groupProperties map[string]any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.identifies = append(c.identifies, groupIdentifyCall{groupType: groupType, groupKey: groupKey, properties: groupProperties})
	return nil
}

func (c *capturePosthogClient) CaptureGroupEvent(_ context.Context, eventName string, distinctID string, groups map[string]string, eventProperties map[string]any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failNextCaptures > 0 {
		c.failNextCaptures--
		return errors.New("posthog enqueue failed")
	}
	c.events = append(c.events, groupEventCall{event: eventName, distinctID: distinctID, groups: groups, properties: eventProperties})
	return nil
}

func setupForwardTokenUsageTest(t *testing.T, dbName string) (*activities.ForwardTokenUsageToPostHog, *pgxpool.Pool, *capturePosthogClient, string) {
	t.Helper()
	ctx := t.Context()

	conn, err := infra.CloneTestDatabase(t, dbName)
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	orgID := "org-" + uuid.NewString()[:8]
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Token Usage Org",
		Slug:        orgID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	captured := &capturePosthogClient{}
	act := activities.NewForwardTokenUsageToPostHog(
		testenv.NewLogger(t),
		conn,
		captured,
		cache.NewRedisCacheAdapter(redisClient),
	)
	return act, conn, captured, orgID
}

func upsertCycleUsage(t *testing.T, conn *pgxpool.Pool, orgID string, start, end time.Time, tokens int64) {
	t.Helper()
	err := usagerepo.New(conn).UpsertBillingCycleUsage(t.Context(), usagerepo.UpsertBillingCycleUsageParams{
		OrganizationID: orgID,
		CycleStart:     pgtype.Timestamptz{Time: start, Valid: true},
		CycleEnd:       pgtype.Timestamptz{Time: end, Valid: true},
		TumTokens:      tokens,
		FinalizedAt:    pgtype.Timestamptz{},
	})
	require.NoError(t, err)
}

// TestForwardTokenUsageToPostHog_ForwardsGroupAndDailyEvent pins the payload
// shape (AGE-2289): group properties on the slug-keyed organization group
// carrying the current/previous cycle TUM tokens and allowance utilization,
// plus a once-per-UTC-day organization_token_usage event with the same
// numbers.
func TestForwardTokenUsageToPostHog_ForwardsGroupAndDailyEvent(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	act, conn, captured, orgID := setupForwardTokenUsageTest(t, "posthogtum")

	_, err := usagerepo.New(conn).UpsertBillingMetadata(ctx, usagerepo.UpsertBillingMetadataParams{
		OrganizationID:         orgID,
		TumMonthlyTokenLimit:   pgtype.Int8{Int64: 1_000, Valid: true},
		AlertEmail:             pgtype.Text{},
		BillingCycleAnchorDay:  1,
		TunneledMcpServerLimit: pgtype.Int4{},
	})
	require.NoError(t, err)

	now := time.Now().UTC()
	cycleStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	prevStart := cycleStart.AddDate(0, -1, 0)
	upsertCycleUsage(t, conn, orgID, prevStart, cycleStart, 400)
	upsertCycleUsage(t, conn, orgID, cycleStart, cycleStart.AddDate(0, 1, 0), 250)

	require.NoError(t, act.Do(ctx, []string{orgID}))

	require.Len(t, captured.identifies, 1)
	identify := captured.identifies[0]
	require.Equal(t, "organization", identify.groupType)
	require.Equal(t, orgID, identify.groupKey, "the organization group is keyed by slug (the seeded slug equals the org id)")
	require.Equal(t, int64(250), identify.properties["tum_tokens_current_cycle"])
	require.Equal(t, int64(400), identify.properties["tum_tokens_previous_cycle"])
	require.Equal(t, int64(1_000), identify.properties["tum_monthly_token_limit"])
	require.InDelta(t, 0.25, identify.properties["tum_allowance_used_ratio"], 1e-9)
	require.Equal(t, orgID, identify.properties["organization_id"])

	require.Len(t, captured.events, 1)
	event := captured.events[0]
	require.Equal(t, "organization_token_usage", event.event)
	require.Equal(t, orgID, event.distinctID)
	require.Equal(t, map[string]string{"organization": orgID}, event.groups)
	require.Equal(t, int64(250), event.properties["tum_tokens_current_cycle"])
}

// TestForwardTokenUsageToPostHog_EventOncePerDay pins the hourly-refresh
// behavior: group properties re-forward on every run (last write wins), but
// the trend event is elected once per UTC day via the SET NX marker.
func TestForwardTokenUsageToPostHog_EventOncePerDay(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	act, conn, captured, orgID := setupForwardTokenUsageTest(t, "posthogtumdaily")

	now := time.Now().UTC()
	cycleStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	upsertCycleUsage(t, conn, orgID, cycleStart, cycleStart.AddDate(0, 1, 0), 100)

	require.NoError(t, act.Do(ctx, []string{orgID}))
	require.NoError(t, act.Do(ctx, []string{orgID}))

	require.Len(t, captured.identifies, 2, "group properties refresh on every run")
	require.Len(t, captured.events, 1, "the usage event is emitted once per day")

	// This org has no contracted limit: the limit keys must still be written
	// (as nil) so a removed contract clears stale values off the group
	// instead of leaving GTM cohorts treating the org as limited.
	props := captured.identifies[0].properties
	require.Contains(t, props, "tum_monthly_token_limit")
	require.Nil(t, props["tum_monthly_token_limit"])
	require.Contains(t, props, "tum_allowance_used_ratio")
	require.Nil(t, props["tum_allowance_used_ratio"])
}

// TestForwardTokenUsageToPostHog_CaptureFailureReleasesDailyClaim pins the
// dedupe-vs-delivery ordering: a failed capture must release the day's SET NX
// claim so a retry can still emit the point, instead of the whole day's trend
// point silently dropping.
func TestForwardTokenUsageToPostHog_CaptureFailureReleasesDailyClaim(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	act, conn, captured, orgID := setupForwardTokenUsageTest(t, "posthogtumretry")

	now := time.Now().UTC()
	cycleStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	upsertCycleUsage(t, conn, orgID, cycleStart, cycleStart.AddDate(0, 1, 0), 100)

	captured.failNextCaptures = 1
	require.Error(t, act.Do(ctx, []string{orgID}), "the failed capture surfaces so the activity retries")
	require.Empty(t, captured.events)

	require.NoError(t, act.Do(ctx, []string{orgID}))
	require.Len(t, captured.events, 1, "the retry re-claims the released marker and emits the point")
}

// TestForwardTokenUsageToPostHog_SkipsOrgsWithoutSnapshots pins that dormant
// orgs (no billed usage snapshots) mint no PostHog groups or events.
func TestForwardTokenUsageToPostHog_SkipsOrgsWithoutSnapshots(t *testing.T) {
	t.Parallel()

	act, _, captured, orgID := setupForwardTokenUsageTest(t, "posthogtumempty")

	require.NoError(t, act.Do(t.Context(), []string{orgID}))

	require.Empty(t, captured.identifies)
	require.Empty(t, captured.events)
}
