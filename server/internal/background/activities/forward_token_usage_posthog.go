package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	usagerepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
)

// tokenUsagePosthog is the slice of the PostHog client this activity needs,
// kept as an interface so tests can capture the forwarded payloads.
type tokenUsagePosthog interface {
	GroupIdentify(ctx context.Context, groupType string, groupKey string, groupProperties map[string]any) error
	CaptureGroupEvent(ctx context.Context, eventName string, distinctID string, groups map[string]string, eventProperties map[string]any) error
}

// organizationTokenUsageEvent is the daily per-organization usage event GTM
// charts trends on; the group properties set alongside it are what cohorts
// and dashboards filter on.
const organizationTokenUsageEvent = "organization_token_usage"

// tokenUsageEventDedupeTTL keeps the once-a-day event marker alive well past
// the day it guards, then lets Redis reclaim it.
const tokenUsageEventDedupeTTL = 48 * time.Hour

// ForwardTokenUsageToPostHog publishes each organization's tokens-under-
// management usage to PostHog (AGE-2289) so GTM can price as a function of
// token use. It reads the durable billing_cycle_usage snapshots the
// SnapshotBillingCycleUsage activity just refreshed — never ClickHouse — and
// forwards two shapes per org: group properties on the "organization" group
// (idempotent, refreshed every run) and a once-per-UTC-day
// organization_token_usage event (the time series).
type ForwardTokenUsageToPostHog struct {
	logger  *slog.Logger
	db      *pgxpool.Pool
	posthog tokenUsagePosthog
	cache   cache.Cache
}

func NewForwardTokenUsageToPostHog(logger *slog.Logger, db *pgxpool.Pool, posthogClient tokenUsagePosthog, cacheAdapter cache.Cache) *ForwardTokenUsageToPostHog {
	return &ForwardTokenUsageToPostHog{
		logger:  logger,
		db:      db,
		posthog: posthogClient,
		cache:   cacheAdapter,
	}
}

func (f *ForwardTokenUsageToPostHog) Do(ctx context.Context, orgIDs []string) error {
	queries := usagerepo.New(f.db)
	orgs := orgRepo.New(f.db)
	now := time.Now().UTC()

	var errs []error
	for _, orgID := range orgIDs {
		if err := f.forwardOrganization(ctx, queries, orgs, orgID, now); err != nil {
			f.logger.ErrorContext(ctx, "failed to forward token usage to posthog",
				attr.SlogOrganizationID(orgID), attr.SlogError(err))
			errs = append(errs, fmt.Errorf("forward org %s: %w", orgID, err))
		}
	}

	return errors.Join(errs...)
}

func (f *ForwardTokenUsageToPostHog) forwardOrganization(ctx context.Context, queries *usagerepo.Queries, orgs *orgRepo.Queries, orgID string, now time.Time) error {
	rows, err := queries.ListBillingCycleUsage(ctx, orgID)
	if err != nil {
		return fmt.Errorf("list billing cycle usage: %w", err)
	}
	// No snapshots means no billed usage yet — don't mint empty PostHog
	// groups for every dormant org.
	if len(rows) == 0 {
		return nil
	}

	org, err := orgs.GetOrganizationMetadata(ctx, orgID)
	if err != nil {
		return fmt.Errorf("get organization metadata: %w", err)
	}

	meta, err := queries.GetBillingMetadata(ctx, orgID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("get billing metadata: %w", err)
	}
	// On pgx.ErrNoRows the zero-value row stands in for an unconfigured
	// contract, same as the snapshot activity.

	// Rows are ordered by cycle_start, so the last row is the active cycle.
	current := rows[len(rows)-1]
	properties := map[string]any{
		"organization_id":          orgID,
		"organization_slug":        org.Slug,
		"gram_account_type":        org.GramAccountType,
		"tum_tokens_current_cycle": current.TumTokens,
		"tum_cycle_start":          current.CycleStart.Time.UTC().Format(time.RFC3339),
		"tum_cycle_end":            current.CycleEnd.Time.UTC().Format(time.RFC3339),
	}
	if len(rows) >= 2 {
		properties["tum_tokens_previous_cycle"] = rows[len(rows)-2].TumTokens
	}
	if meta.TumMonthlyTokenLimit.Valid && meta.TumMonthlyTokenLimit.Int64 > 0 {
		limit := meta.TumMonthlyTokenLimit.Int64
		properties["tum_monthly_token_limit"] = limit
		properties["tum_allowance_used_ratio"] = math.Round(float64(current.TumTokens)/float64(limit)*10_000) / 10_000
	}

	// The organization group is keyed by SLUG across every PostHog capture
	// path (dashboard posthog-js and the server's CaptureEvent alike); an id
	// key here would fork the group into a second identity.
	if err := f.posthog.GroupIdentify(ctx, "organization", org.Slug, properties); err != nil {
		return fmt.Errorf("group identify: %w", err)
	}

	// The workflow runs hourly but the trend series wants one point per day:
	// a SET NX marker elects exactly one run per UTC day to emit the event. A
	// lost marker (Redis flush) costs at most one duplicate point.
	dedupeKey := fmt.Sprintf("posthog:org-token-usage:%s:%s", orgID, now.Format("2006-01-02"))
	won, err := f.cache.Add(ctx, dedupeKey, tokenUsageEventDedupeTTL)
	if err != nil {
		return fmt.Errorf("token usage event dedupe: %w", err)
	}
	if !won {
		return nil
	}

	if err := f.posthog.CaptureGroupEvent(ctx, organizationTokenUsageEvent, orgID,
		map[string]string{"organization": org.Slug}, properties); err != nil {
		return fmt.Errorf("capture token usage event: %w", err)
	}

	return nil
}
