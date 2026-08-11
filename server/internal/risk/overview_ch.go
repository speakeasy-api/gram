package risk

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// riskOverviewRowLimit caps the top-users and top-rules lists. Enough rows for
// the "view all" pages to show the full long-tail without pagination; the main
// /risk-overview widget only renders the top 10.
const riskOverviewRowLimit = 200

// overviewFromClickHouse reports whether GetRiskOverview should serve from
// ClickHouse for this org. Per-org PostHog rollout flag, targeted by org and
// slug groups the same way the dashboard evaluates flags. A nil provider,
// missing ClickHouse connection, or a failed lookup degrades to the Postgres
// path.
func (s *Service) overviewFromClickHouse(ctx context.Context, authCtx *contextvalues.AuthContext) bool {
	if s.findingsCH == nil || s.flags == nil {
		return false
	}
	groups := feature.OrgProjectGroups(authCtx.OrganizationSlug, conv.PtrValOr(authCtx.ProjectSlug, ""))
	on, err := s.flags.IsFlagEnabled(ctx, feature.FlagRiskOverviewFromClickHouse, authCtx.ActiveOrganizationID, groups)
	if err != nil {
		s.logger.WarnContext(ctx, "risk-overview-from-clickhouse flag check failed; serving from postgres",
			attr.SlogError(err),
			attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		)
		return false
	}
	return on
}

// getRiskOverviewFromClickHouse serves GetRiskOverview from the ClickHouse
// risk_findings table instead of Postgres risk_results, relieving the
// autovacuum pressure the PG queries create. Events Scanned and Active
// Policies stay on Postgres: ClickHouse stores only true positives (clean
// scans are never written) and policies are current-state config.
func (s *Service) getRiskOverviewFromClickHouse(ctx context.Context, projectID uuid.UUID, organizationID string, from, to time.Time) (*gen.RiskOverviewResult, error) {
	window := riskOverviewWindowParams(from, to)
	scanCounts, err := s.repo.GetRiskOverviewScanCounts(ctx, repo.GetRiskOverviewScanCountsParams{
		ProjectID: projectID,
		FromTime:  window.from,
		ToTime:    window.to,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get risk overview scan counts").LogError(ctx, s.logger)
	}

	chWindow := chrepo.RiskOverviewWindowParams{
		OrganizationID: organizationID,
		ProjectID:      projectID.String(),
		From:           from,
		To:             to,
	}

	findingCounts, err := s.findingsCH.GetRiskOverviewFindingCounts(ctx, chWindow)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get risk overview finding counts from clickhouse").LogError(ctx, s.logger)
	}

	ruleRows, err := s.findingsCH.ListRiskOverviewTopRules(ctx, chWindow, riskOverviewRowLimit)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list risk overview top rules from clickhouse").LogError(ctx, s.logger)
	}

	buckets, err := s.findingsCH.ListRiskOverviewCategoryTimeSeries(ctx, chWindow)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list risk overview time series from clickhouse").LogError(ctx, s.logger)
	}

	// Fetch raw (user_id, external_user_id) groups with headroom: email
	// resolution can merge several raw groups into one display identity, so
	// limiting at the display cap here could undercount a merged user near the
	// boundary. The display-level cap applies after merging.
	userRows, err := s.findingsCH.ListRiskOverviewTopUsers(ctx, chWindow, riskOverviewRowLimit*5)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list risk overview top users from clickhouse").LogError(ctx, s.logger)
	}

	topUsers, err := s.resolveOverviewUserEmails(ctx, organizationID, userRows)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "resolve risk overview user emails").LogError(ctx, s.logger)
	}

	topRules := make([]*gen.RiskRuleBreakdownEntry, 0, len(ruleRows))
	for _, row := range ruleRows {
		topRules = append(topRules, &gen.RiskRuleBreakdownEntry{
			RuleID:   row.RuleID,
			Source:   row.Source,
			Findings: safeCount(row.Findings),
		})
	}

	categoryCounts := make(map[string]int64, len(buckets))
	for _, bucket := range buckets {
		categoryCounts[bucket.Category] += safeCount(bucket.Findings)
	}

	return &gen.RiskOverviewResult{
		From:               from.UTC().Format(time.RFC3339),
		To:                 to.UTC().Format(time.RFC3339),
		MessagesScanned:    scanCounts.MessagesScanned,
		Findings:           safeCount(findingCounts.Findings),
		FlaggedSessions:    safeCount(findingCounts.FlaggedSessions),
		ActivePolicies:     scanCounts.ActivePolicies,
		TopCategories:      topCategoriesFromCounts(categoryCounts, 10),
		TopUsers:           topUsers,
		TopRules:           topRules,
		TimeSeriesFindings: fillCategoryTimeSeries(from, to, buckets),
	}, nil
}

// resolveOverviewUserEmails turns ClickHouse (user_id, external_user_id)
// groups into display rows, replicating the Postgres query's email precedence
// in Go: users.email, else an @-containing external id, else "Unknown user".
// Groups that resolve to the same (external_user_id, email) merge, mirroring
// the Postgres GROUP BY. The email lookup is tenant-bound to organizationID.
func (s *Service) resolveOverviewUserEmails(ctx context.Context, organizationID string, rows []chrepo.RiskOverviewUserCount) ([]*gen.RiskOverviewUser, error) {
	ids := make([]string, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if row.UserID == "" {
			continue
		}
		if _, ok := seen[row.UserID]; ok {
			continue
		}
		seen[row.UserID] = struct{}{}
		ids = append(ids, row.UserID)
	}

	emails := make(map[string]string, len(ids))
	if len(ids) > 0 {
		userRows, err := s.repo.ListUserEmailsByIDs(ctx, repo.ListUserEmailsByIDsParams{
			OrganizationID: organizationID,
			Ids:            ids,
		})
		if err != nil {
			return nil, fmt.Errorf("list user emails by ids: %w", err)
		}
		for _, user := range userRows {
			if user.Email != "" {
				emails[user.ID] = user.Email
			}
		}
	}

	type userKey struct {
		externalUserID string
		email          string
	}
	merged := make(map[userKey]int64, len(rows))
	for _, row := range rows {
		email := emails[row.UserID]
		if email == "" && strings.Contains(row.ExternalUserID, "@") {
			email = row.ExternalUserID
		}
		if email == "" {
			email = "Unknown user"
		}
		merged[userKey{externalUserID: row.ExternalUserID, email: email}] += safeCount(row.Findings)
	}

	out := make([]*gen.RiskOverviewUser, 0, len(merged))
	for key, findings := range merged {
		out = append(out, &gen.RiskOverviewUser{
			Email:          key.email,
			ExternalUserID: key.externalUserID,
			Findings:       findings,
		})
	}
	slices.SortFunc(out, func(a, b *gen.RiskOverviewUser) int {
		if a.Findings != b.Findings {
			return cmp.Compare(b.Findings, a.Findings)
		}
		return cmp.Compare(a.Email, b.Email)
	})
	if len(out) > riskOverviewRowLimit {
		out = out[:riskOverviewRowLimit]
	}

	return out, nil
}

// fillCategoryTimeSeries expands the sparse (hour, category) cells ClickHouse
// returns into the dense hour x category grid the dashboard chart expects,
// replicating the Postgres generate_series cross-join: one row per window hour
// per category observed in the window, zero-filled, ordered by bucket then
// category. No categories in the window yields an empty series.
func fillCategoryTimeSeries(from, to time.Time, buckets []chrepo.RiskOverviewCategoryBucket) []*gen.RiskOverviewTimeSeriesFinding {
	out := make([]*gen.RiskOverviewTimeSeriesFinding, 0, len(buckets))
	if len(buckets) == 0 {
		return out
	}

	type cell struct {
		hourUnix int64
		category string
	}
	counts := make(map[cell]int64, len(buckets))
	categories := make([]string, 0)
	seen := make(map[string]struct{})
	for _, bucket := range buckets {
		hour := bucket.BucketStart.UTC().Truncate(time.Hour)
		counts[cell{hourUnix: hour.Unix(), category: bucket.Category}] += safeCount(bucket.Findings)
		if _, ok := seen[bucket.Category]; !ok {
			seen[bucket.Category] = struct{}{}
			categories = append(categories, bucket.Category)
		}
	}
	slices.Sort(categories)

	start := from.UTC().Truncate(time.Hour)
	// Mirror the Postgres bucket series: the last bucket is the hour containing
	// to minus one microsecond, so an exact-hour to does not add an extra
	// empty trailing bucket.
	end := to.UTC().Add(-time.Microsecond).Truncate(time.Hour)
	for hour := start; !hour.After(end); hour = hour.Add(time.Hour) {
		for _, category := range categories {
			out = append(out, &gen.RiskOverviewTimeSeriesFinding{
				BucketStart: hour.Format(time.RFC3339),
				Category:    category,
				Findings:    counts[cell{hourUnix: hour.Unix(), category: category}],
			})
		}
	}

	return out
}

// safeCount narrows a ClickHouse UInt64 aggregate to the int64 the API types
// use, clamping instead of overflowing.
func safeCount(v uint64) int64 {
	if v > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(v)
}
