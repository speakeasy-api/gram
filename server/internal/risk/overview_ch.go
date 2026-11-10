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
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// riskOverviewRowLimit caps the top-users and top-rules lists.
const riskOverviewRowLimit = 200

// getRiskOverviewFromClickHouse serves GetRiskOverview from the ClickHouse
// risk_findings table.
func (s *Service) getRiskOverviewFromClickHouse(ctx context.Context, projectID uuid.UUID, organizationID string, from, to time.Time) (*gen.RiskOverviewResult, error) {
	chWindow := chrepo.RiskOverviewWindowParams{
		OrganizationID: organizationID,
		ProjectID:      projectID.String(),
		From:           from,
		To:             to,
		MCPServerID:    "",
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
		Findings:           safeCount(findingCounts.Findings),
		FlaggedSessions:    safeCount(findingCounts.FlaggedSessions),
		TopCategories:      topCategoriesFromCounts(categoryCounts, 10),
		TopUsers:           topUsers,
		TopRules:           topRules,
		TimeSeriesFindings: fillCategoryTimeSeries(from, to, buckets),
	}, nil
}

// ruleBreakdownRowLimit bounds the rules one breakdown returns.
const ruleBreakdownRowLimit = 1000

// RuleBreakdown is one category's per-rule finding counts over a window.
// Truncated reports that more rules exist than were returned; Total covers
// only the returned rules.
type RuleBreakdown struct {
	From      string
	To        string
	Category  string
	Rules     []*gen.RiskRuleBreakdownEntry
	Total     int64
	Truncated bool
}

// GetRuleBreakdown counts one category's live findings per rule for the
// managed assistant. Findings of deleted policies are excluded, as in the Risk
// Events listing.
func (s *Service) GetRuleBreakdown(ctx context.Context, category string, rawFrom, rawTo *string) (*RuleBreakdown, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	from, to, err := resolveRiskOverviewWindow(rawFrom, rawTo)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid window").LogError(ctx, s.logger)
	}

	if s.findingsCH == nil {
		return nil, oops.E(oops.CodeNotImplemented, nil, "risk rule breakdown requires the ClickHouse findings store").LogError(ctx, s.logger)
	}

	out := &RuleBreakdown{
		From:      from.UTC().Format(time.RFC3339),
		To:        to.UTC().Format(time.RFC3339),
		Category:  category,
		Rules:     []*gen.RiskRuleBreakdownEntry{},
		Total:     0,
		Truncated: false,
	}

	policyIDs, err := s.visiblePolicyIDs(ctx, *authCtx.ProjectID, uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "resolve visible risk policies").LogError(ctx, s.logger)
	}
	if len(policyIDs) == 0 {
		return out, nil
	}

	rows, err := s.findingsCH.ListRiskRuleCountsByCategory(ctx, chrepo.RiskOverviewWindowParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      authCtx.ProjectID.String(),
		From:           from,
		To:             to,
		MCPServerID:    "",
	}, category, policyIDs, ruleBreakdownRowLimit+1)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list risk rule breakdown from clickhouse").LogError(ctx, s.logger)
	}

	// One row past the bound detects truncation without an unbounded read.
	if len(rows) > ruleBreakdownRowLimit {
		out.Truncated = true
		rows = rows[:ruleBreakdownRowLimit]
	}
	for _, row := range rows {
		findings := safeCount(row.Findings)
		out.Rules = append(out.Rules, &gen.RiskRuleBreakdownEntry{
			RuleID:   row.RuleID,
			Source:   row.Source,
			Findings: findings,
		})
		out.Total += findings
	}

	return out, nil
}

// resolveOverviewUserEmails turns ClickHouse (user_id, external_user_id)
// groups into display rows, replicating the Postgres query's email precedence
// in Go: users.email, else an @-containing external id, else "Unknown user".
// Groups that resolve to the same (external_user_id, email) merge, mirroring
// the Postgres GROUP BY. The email lookup is tenant-bound to organizationID.
func (s *Service) resolveOverviewUserEmails(ctx context.Context, organizationID string, rows []chrepo.RiskOverviewUserCount) ([]*gen.RiskOverviewUser, error) {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.UserID)
	}
	emails, err := s.riskUserEmailsByID(ctx, organizationID, ids)
	if err != nil {
		return nil, err
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

// riskUserEmailsByID batch-resolves current display emails from internal user
// ids. The query is organization-bound, so an id from another tenant cannot
// disclose its email. Callers choose whether lookup failure is fatal or a
// best-effort display fallback.
func (s *Service) riskUserEmailsByID(ctx context.Context, organizationID string, rawIDs []string) (map[string]string, error) {
	ids := make([]string, 0, len(rawIDs))
	seen := make(map[string]struct{}, len(rawIDs))
	for _, id := range rawIDs {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	emails := make(map[string]string, len(ids))
	if len(ids) == 0 {
		return emails, nil
	}
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
	return emails, nil
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
