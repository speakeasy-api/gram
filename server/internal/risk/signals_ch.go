package risk

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
)

const (
	// riskSignalRowLimit caps the number of signals (rule clusters) returned.
	// Rule cardinality is bounded by the detector catalogs, so 200 covers the
	// realistic universe; the exposure rollup is computed from these rows and
	// inherits the cap.
	riskSignalRowLimit = 200
	// riskSignalTopUsersPerRule is how many raw (user_id, external_user_id)
	// groups are fetched per rule. Display shows up to this many after email
	// merging.
	riskSignalTopUsersPerRule = 5
	// riskSignalTopUsersTotalLimit bounds the per-rule top-users fetch overall.
	riskSignalTopUsersTotalLimit = riskSignalRowLimit * riskSignalTopUsersPerRule
	// riskSignalSparkBuckets is how many equal-width buckets the window is
	// split into for the per-signal sparkline. Buckets are epoch-aligned, so
	// an arbitrary window can straddle one extra partial bucket.
	riskSignalSparkBuckets = 24
)

// GetRiskSignals serves the Watchdog page: findings clustered by rule into
// ranked signals, window-level KPI counts with previous-window comparisons,
// and the exposure-by-category rollup. ClickHouse-only — there is no Postgres
// fallback, so orgs must be on the ClickHouse findings ingest before the
// Watchdog UI is enabled for them.
func (s *Service) GetRiskSignals(ctx context.Context, payload *gen.GetRiskSignalsPayload) (*gen.RiskSignalsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	from, to, err := resolveRiskOverviewWindow(payload.From, payload.To)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid signals window").LogError(ctx, s.logger)
	}

	if s.findingsCH == nil {
		return nil, oops.E(oops.CodeNotImplemented, nil, "risk signals require the ClickHouse findings store").LogError(ctx, s.logger)
	}

	organizationID := authCtx.ActiveOrganizationID
	projectID := authCtx.ProjectID.String()
	wideFrom := from.Add(-to.Sub(from))

	doubled := chrepo.RiskSignalWindowParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		WideFrom:       wideFrom,
		From:           from,
		To:             to,
	}

	currentWindow := chrepo.RiskOverviewWindowParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		From:           from,
		To:             to,
	}
	bucketSeconds := max(int64(to.Sub(from).Seconds())/riskSignalSparkBuckets, 60)

	// The five reads are independent (they only share the resolved window), so
	// fan them out: each rescans risk_findings through the dedup subquery, and
	// running them sequentially would stack those scan costs into the response
	// latency on large tenants.
	var (
		aggregates   []chrepo.RiskSignalAggregate
		userRows     []chrepo.RiskSignalUserCount
		seriesRows   []chrepo.RiskSignalSeriesPoint
		windowCounts chrepo.RiskSignalSplitCounts
		dayCounts    chrepo.RiskSignalSplitCounts
	)
	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		var err error
		aggregates, err = s.findingsCH.ListRiskSignalAggregates(groupCtx, doubled, riskSignalRowLimit)
		if err != nil {
			return fmt.Errorf("list risk signal aggregates: %w", err)
		}
		return nil
	})
	group.Go(func() error {
		var err error
		userRows, err = s.findingsCH.ListRiskSignalTopUsers(groupCtx, currentWindow, riskSignalTopUsersPerRule, riskSignalTopUsersTotalLimit)
		if err != nil {
			return fmt.Errorf("list risk signal top users: %w", err)
		}
		return nil
	})
	group.Go(func() error {
		var err error
		seriesRows, err = s.findingsCH.ListRiskSignalSeries(groupCtx, currentWindow, bucketSeconds)
		if err != nil {
			return fmt.Errorf("list risk signal series: %w", err)
		}
		return nil
	})
	group.Go(func() error {
		var err error
		windowCounts, err = s.findingsCH.GetRiskSignalSplitCounts(groupCtx, doubled)
		if err != nil {
			return fmt.Errorf("get risk signal window counts: %w", err)
		}
		return nil
	})
	group.Go(func() error {
		var err error
		dayCounts, err = s.findingsCH.GetRiskSignalSplitCounts(groupCtx, chrepo.RiskSignalWindowParams{
			OrganizationID: organizationID,
			ProjectID:      projectID,
			WideFrom:       to.Add(-48 * time.Hour),
			From:           to.Add(-24 * time.Hour),
			To:             to,
		})
		if err != nil {
			return fmt.Errorf("get risk signal 24h counts: %w", err)
		}
		return nil
	})
	if err := group.Wait(); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load risk signals from clickhouse").LogError(ctx, s.logger)
	}

	sparklines, sparkLen := signalSparklines(seriesRows, from, to, bucketSeconds)

	topUsersByRule := signalTopUsersByRule(userRows)

	signals := make([]*gen.RiskSignal, 0, len(aggregates))
	scores := make([]float64, 0, len(aggregates))
	prevScores := make([]float64, 0, len(aggregates))
	for _, agg := range aggregates {
		score := signalScore(signalScoreInputs{
			category:         agg.Category,
			avgConfidence:    agg.AvgConfidence,
			findings:         agg.FindingsCur,
			users:            agg.UsersCur,
			lastSeen:         agg.LastSeen,
			windowEnd:        to,
			previousFindings: agg.FindingsPrev,
		})
		scores = append(scores, score)

		if agg.FindingsPrev > 0 {
			// Previous-window score for the org-score comparison: same formula
			// minus the recency and growth bonuses, which describe the current
			// window only.
			prevScores = append(prevScores, signalScore(signalScoreInputs{
				category:         agg.Category,
				avgConfidence:    agg.AvgConfidence,
				findings:         agg.FindingsPrev,
				users:            agg.UsersPrev,
				lastSeen:         time.Time{},
				windowEnd:        time.Time{},
				previousFindings: 0,
			}))
		}

		topUsers := topUsersByRule[agg.RuleID]
		if topUsers == nil {
			topUsers = []*gen.RiskSignalTopUser{}
		}

		spark := sparklines[agg.RuleID]
		if spark == nil {
			spark = make([]int64, sparkLen)
		}

		sources := slices.Clone(agg.Sources)
		slices.Sort(sources)
		apps := slices.Clone(agg.Apps)
		if apps == nil {
			apps = []string{}
		}
		slices.Sort(apps)

		signals = append(signals, &gen.RiskSignal{
			Key:              "rule:" + agg.RuleID,
			RuleID:           agg.RuleID,
			Category:         agg.Category,
			Description:      agg.Description,
			DetectionSources: sources,
			Apps:             apps,
			Severity:         severityForScore(score),
			RiskScore:        score,
			Findings:         safeCount(agg.FindingsCur),
			PreviousFindings: safeCount(agg.FindingsPrev),
			Users:            safeCount(agg.UsersCur),
			Teams:            safeCount(agg.TeamsCur),
			FirstSeen:        agg.FirstSeen.UTC().Format(time.RFC3339),
			LastSeen:         agg.LastSeen.UTC().Format(time.RFC3339),
			TopUsers:         topUsers,
			Sparkline:        spark,
		})
	}

	slices.SortFunc(signals, func(a, b *gen.RiskSignal) int {
		if a.RiskScore != b.RiskScore {
			return cmp.Compare(b.RiskScore, a.RiskScore)
		}
		if a.Findings != b.Findings {
			return cmp.Compare(b.Findings, a.Findings)
		}
		return cmp.Compare(a.RuleID, b.RuleID)
	})

	var criticalSignals int64
	for _, signal := range signals {
		if signal.Severity == "critical" {
			criticalSignals++
		}
	}

	return &gen.RiskSignalsResult{
		From:                 from.UTC().Format(time.RFC3339),
		To:                   to.UTC().Format(time.RFC3339),
		OrgRiskScore:         orgRiskScore(scores, windowCounts.FindingsCur),
		PreviousOrgRiskScore: orgRiskScore(prevScores, windowCounts.FindingsPrev),
		Findings24h:          safeCount(dayCounts.FindingsCur),
		PreviousFindings24h:  safeCount(dayCounts.FindingsPrev),
		OpenSignals:          int64(len(signals)),
		CriticalSignals:      criticalSignals,
		UsersExposed:         safeCount(windowCounts.UsersCur),
		PreviousUsersExposed: safeCount(windowCounts.UsersPrev),
		Exposure:             signalExposure(aggregates),
		Signals:              signals,
	}, nil
}

// signalSparklines gap-fills the sparse per-(rule, bucket) series into dense
// per-rule arrays covering the window, oldest bucket first. Buckets are
// epoch-aligned to bucketSeconds, so the array length is derived from the
// aligned bucket that contains `from` through the one containing `to-1` — an
// arbitrary window can straddle one extra partial bucket.
func signalSparklines(rows []chrepo.RiskSignalSeriesPoint, from, to time.Time, bucketSeconds int64) (map[string][]int64, int) {
	alignedFrom := from.Unix() / bucketSeconds * bucketSeconds
	length := max(int((to.Unix()-1-alignedFrom)/bucketSeconds)+1, 1)
	length = min(length, riskSignalSparkBuckets+1)

	out := make(map[string][]int64)
	for _, row := range rows {
		idx := int((row.BucketStart.Unix() - alignedFrom) / bucketSeconds)
		if idx < 0 || idx >= length {
			continue
		}
		series := out[row.RuleID]
		if series == nil {
			series = make([]int64, length)
			out[row.RuleID] = series
		}
		series[idx] += safeCount(row.Findings)
	}
	return out, length
}

// signalTopUsersByRule turns raw per-(rule, user) counts into display rows per
// rule. Email precedence mirrors the overview's, but resolves entirely from
// the denormalized ClickHouse columns: the ingest-stamped user_email, else an
// @-containing external id, else "Unknown user". Raw groups that resolve to
// the same display identity merge.
func signalTopUsersByRule(rows []chrepo.RiskSignalUserCount) map[string][]*gen.RiskSignalTopUser {
	type userKey struct {
		externalUserID string
		email          string
	}
	type userStats struct {
		findings int64
		team     string
	}
	merged := make(map[string]map[userKey]userStats)
	for _, row := range rows {
		email := row.Email
		if email == "" && strings.Contains(row.ExternalUserID, "@") {
			email = row.ExternalUserID
		}
		if email == "" {
			email = "Unknown user"
		}
		if merged[row.RuleID] == nil {
			merged[row.RuleID] = make(map[userKey]userStats)
		}
		key := userKey{externalUserID: row.ExternalUserID, email: email}
		stats := merged[row.RuleID][key]
		stats.findings += safeCount(row.Findings)
		if stats.team == "" {
			stats.team = row.Team
		}
		merged[row.RuleID][key] = stats
	}

	out := make(map[string][]*gen.RiskSignalTopUser, len(merged))
	for ruleID, users := range merged {
		list := make([]*gen.RiskSignalTopUser, 0, len(users))
		for key, stats := range users {
			list = append(list, &gen.RiskSignalTopUser{
				Email:          key.email,
				ExternalUserID: key.externalUserID,
				Team:           stats.team,
				Findings:       stats.findings,
			})
		}
		slices.SortFunc(list, func(a, b *gen.RiskSignalTopUser) int {
			if a.Findings != b.Findings {
				return cmp.Compare(b.Findings, a.Findings)
			}
			return cmp.Compare(a.Email, b.Email)
		})
		if len(list) > riskSignalTopUsersPerRule {
			list = list[:riskSignalTopUsersPerRule]
		}
		out[ruleID] = list
	}
	return out
}

// signalExposure rolls the per-rule aggregates up by category, largest slice
// first. Signals partition the window's findings by rule, so this needs no
// extra query; the share denominator is the same capped set the signals list
// shows.
func signalExposure(aggregates []chrepo.RiskSignalAggregate) []*gen.RiskExposureSlice {
	counts := make(map[string]int64)
	var total int64
	for _, agg := range aggregates {
		count := safeCount(agg.FindingsCur)
		counts[agg.Category] += count
		total += count
	}

	out := make([]*gen.RiskExposureSlice, 0, len(counts))
	for category, findings := range counts {
		share := 0.0
		if total > 0 {
			share = float64(findings) / float64(total)
		}
		out = append(out, &gen.RiskExposureSlice{
			Category: category,
			Findings: findings,
			Share:    share,
		})
	}
	slices.SortFunc(out, func(a, b *gen.RiskExposureSlice) int {
		if a.Findings != b.Findings {
			return cmp.Compare(b.Findings, a.Findings)
		}
		return cmp.Compare(a.Category, b.Category)
	})
	return out
}
