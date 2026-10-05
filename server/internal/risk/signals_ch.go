package risk

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/management/readmodel"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
)

const (
	// riskSignalRowLimit caps the number of signals (rule clusters) returned.
	// Rule cardinality is bounded by the detector catalogs, so 200 covers the
	// realistic universe; the exposure rollup is computed from these rows and
	// inherits the cap.
	riskSignalRowLimit = 200
	// riskSignalTopUsersPerRule is how many merged display users each signal
	// shows.
	riskSignalTopUsersPerRule = 5
	// riskSignalTopUsersFetchPerRule is how many raw (user_id,
	// external_user_id) groups are fetched per rule — a buffer over the
	// display count so merging raw id spellings of the same person can still
	// fill the display list instead of truncating it at the query.
	riskSignalTopUsersFetchPerRule = 3 * riskSignalTopUsersPerRule
	// riskSignalTopUsersTotalLimit bounds the per-rule top-users fetch overall.
	riskSignalTopUsersTotalLimit = riskSignalRowLimit * riskSignalTopUsersFetchPerRule
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

	if !s.riskWatchdogEnabled(ctx, authCtx) {
		return nil, oops.E(oops.CodeForbidden, nil, "risk signals are not enabled for this organization")
	}

	from, to, err := resolveRiskOverviewWindow(payload.From, payload.To)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid signals window").LogError(ctx, s.logger)
	}

	mcpServerID := ""
	if payload.McpServerID != nil {
		mcpServerID = *payload.McpServerID
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
		MCPServerID:    mcpServerID,
	}

	currentWindow := chrepo.RiskOverviewWindowParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		From:           from,
		To:             to,
		MCPServerID:    mcpServerID,
	}
	// Ceiling division: rounding the width down would let an unaligned window
	// straddle riskSignalSparkBuckets+2 buckets and silently drop findings
	// from the final one past the length cap.
	bucketSeconds := max((int64(to.Sub(from).Seconds())+riskSignalSparkBuckets-1)/riskSignalSparkBuckets, 60)

	// The four reads are independent (they only share the resolved window), so
	// fan them out: each rescans risk_findings through the dedup subquery, and
	// running them sequentially would stack those scan costs into the response
	// latency on large tenants.
	var (
		aggregates   []chrepo.RiskSignalAggregate
		userRows     []chrepo.RiskSignalUserCount
		seriesRows   []chrepo.RiskSignalSeriesPoint
		windowCounts chrepo.RiskSignalSplitCounts
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
		userRows, err = s.findingsCH.ListRiskSignalTopUsers(groupCtx, currentWindow, riskSignalTopUsersFetchPerRule, riskSignalTopUsersTotalLimit)
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
	if err := group.Wait(); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load risk signals from clickhouse").LogError(ctx, s.logger)
	}

	userIDs := make([]string, 0, len(userRows))
	for _, row := range userRows {
		if row.MCPFinding && row.UserID != "" {
			userIDs = append(userIDs, row.UserID)
		}
	}
	userEmails, err := s.riskUserEmailsByID(ctx, organizationID, userIDs)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "resolve risk signal MCP user emails").LogError(ctx, s.logger)
	}
	sparklines, sparkLen := signalSparklines(seriesRows, from, to, bucketSeconds)

	topUsersByRule := signalTopUsersByRule(userRows, userEmails)

	// Policy scores are configuration, not findings. MCP user display
	// enrichment and policy scores are the two deliberate Postgres reads.
	// The operator's configured policy score is the base severity for every
	// signal the policy matched; category defaults only cover findings with no
	// policy attribution.
	policyScores, err := s.riskPolicyScores(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load risk policy scores").LogError(ctx, s.logger)
	}

	// Previous-window scores first: the aggregates include rules active only
	// in the previous window (findings_cur = 0) purely as input to this
	// comparison.
	prevScores := make([]float64, 0, len(aggregates))
	for _, agg := range aggregates {
		if agg.FindingsPrev > 0 {
			prevScores = append(prevScores, signalScore(maxPolicyScore(agg.PolicyIDsPrev, policyScores), agg.Category))
		}
	}

	// Everything below — the signals list, current scores, and the exposure
	// rollup — describes live signals only, so drop the previous-only rows
	// here rather than guarding each consumer.
	aggregates = slices.DeleteFunc(aggregates, func(agg chrepo.RiskSignalAggregate) bool {
		return agg.FindingsCur == 0
	})

	signals := make([]*gen.RiskSignal, 0, len(aggregates))
	scores := make([]float64, 0, len(aggregates))
	for _, agg := range aggregates {
		score := signalScore(maxPolicyScore(agg.PolicyIDsCur, policyScores), agg.Category)
		scores = append(scores, score)

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
		mcpServerIDs := sortedNonNil(agg.MCPServerIDs)
		toolNames := sortedNonNil(agg.ToolNames)

		signals = append(signals, &gen.RiskSignal{
			Key:              "rule:" + agg.RuleID,
			RuleID:           agg.RuleID,
			Category:         agg.Category,
			Description:      agg.Description,
			DetectionSources: sources,
			Apps:             apps,
			McpServerIds:     mcpServerIDs,
			ToolNames:        toolNames,
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
		Findings:             safeCount(windowCounts.FindingsCur),
		PreviousFindings:     safeCount(windowCounts.FindingsPrev),
		OpenSignals:          int64(len(signals)),
		CriticalSignals:      criticalSignals,
		UsersExposed:         safeCount(windowCounts.UsersCur),
		PreviousUsersExposed: safeCount(windowCounts.UsersPrev),
		Exposure:             signalExposure(aggregates),
		Signals:              signals,
	}, nil
}

// riskWatchdogEnabled reports whether the Watchdog signals endpoint is
// enabled for the org. Same PostHog flag key the dashboard uses to show the
// Watchdog page, so one flag controls both surfaces. A failed lookup degrades
// to disabled.
func (s *Service) riskWatchdogEnabled(ctx context.Context, authCtx *contextvalues.AuthContext) bool {
	groups := feature.OrgProjectGroups(authCtx.OrganizationSlug, conv.PtrValOr(authCtx.ProjectSlug, ""))
	on, err := s.flags.IsFlagEnabled(ctx, feature.FlagRiskWatchdog, authCtx.ActiveOrganizationID, groups)
	if err != nil {
		s.logger.WarnContext(ctx, "gram-risk-watchdog flag check failed; treating as disabled",
			attr.SlogError(err),
			attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		)
		return false
	}
	return on
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

// signalTopUsersByRule turns raw per-(rule, user, attribution-kind) counts into
// display rows per rule. MCP rows prefer the current organization user email,
// then the ingest-stamped email. Chat rows retain the existing stamped email
// behavior. Both fall back to an @-containing external id, then "Unknown user".
// Raw groups that resolve to the same display email merge. The external id is
// not part of the merge key because several raw id spellings of one person
// should collapse into one row, but the first non-empty id stays as the row's
// representative id. Rows with no resolvable email keep their raw id as
// identity so distinct unknown users stay separate.
func signalTopUsersByRule(rows []chrepo.RiskSignalUserCount, userEmails map[string]string) map[string][]*gen.RiskSignalTopUser {
	// Exactly one of email/rawID is set, so an email identity can never
	// collide with a raw id spelling of another user.
	type userKey struct {
		email string
		rawID string
	}
	type userStats struct {
		findings       int64
		team           string
		externalUserID string
	}
	merged := make(map[string]map[userKey]userStats)
	for _, row := range rows {
		// Mirror the Users stat's predicate (signalUserNonEmpty): rows it
		// doesn't count must not appear here as "Unknown user" rows either.
		if row.ExternalUserID == "" && row.UserID == "" {
			continue
		}
		email := row.Email
		if row.MCPFinding {
			email = cmp.Or(userEmails[row.UserID], row.Email)
		}
		if email == "" && strings.Contains(row.ExternalUserID, "@") {
			email = row.ExternalUserID
		}
		key := userKey{email: email, rawID: ""}
		if email == "" {
			key = userKey{email: "", rawID: cmp.Or(row.ExternalUserID, row.UserID)}
		}
		if merged[row.RuleID] == nil {
			merged[row.RuleID] = make(map[userKey]userStats)
		}
		stats := merged[row.RuleID][key]
		stats.findings += safeCount(row.Findings)
		if stats.team == "" {
			stats.team = row.Team
		}
		if stats.externalUserID == "" {
			stats.externalUserID = row.ExternalUserID
		}
		merged[row.RuleID][key] = stats
	}

	out := make(map[string][]*gen.RiskSignalTopUser, len(merged))
	for ruleID, users := range merged {
		list := make([]*gen.RiskSignalTopUser, 0, len(users))
		for key, stats := range users {
			list = append(list, &gen.RiskSignalTopUser{
				Email:          cmp.Or(key.email, "Unknown user"),
				ExternalUserID: stats.externalUserID,
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

// riskPolicyScores maps every non-deleted policy id in the project to its
// configured 0.1-10 score. Includes disabled policies on purpose: findings
// written while a policy was enabled keep that policy's severity after it is
// switched off.
func (s *Service) riskPolicyScores(ctx context.Context, projectID uuid.UUID) (map[string]float64, error) {
	policies, err := s.repo.ListRiskPolicies(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list risk policies: %w", err)
	}
	scores := make(map[string]float64, len(policies))
	for _, p := range policies {
		scores[p.ID.String()] = p.Score
	}
	return scores, nil
}

// maxPolicyScore resolves a signal's base severity from the policies that
// matched its findings: the highest configured score wins when several
// policies contributed. Zero when no id resolves, which sends signalScore to
// the category fallback.
func maxPolicyScore(policyIDs []string, scores map[string]float64) float64 {
	var best float64
	for _, id := range policyIDs {
		if s, ok := scores[id]; ok && s > best {
			best = s
		}
	}
	return best
}

// sortedNonNil returns a sorted copy of values, never nil so the JSON field
// serializes as an empty array.
func sortedNonNil(values []string) []string {
	out := slices.Clone(values)
	if out == nil {
		return []string{}
	}
	slices.Sort(out)
	return out
}

// riskMCPServerCountLimit caps the per-server counts returned; a project has
// far fewer concrete servers in practice.
const riskMCPServerCountLimit = 500

// GetRiskMcpServerCounts serves the MCP server filter pickers: live finding
// counts per concrete server over the window. ClickHouse-only, gated like
// GetRiskSignals.
func (s *Service) GetRiskMcpServerCounts(ctx context.Context, payload *gen.GetRiskMcpServerCountsPayload) (*gen.RiskMcpServerCountsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	from, to, err := resolveRiskOverviewWindow(payload.From, payload.To)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid window").LogError(ctx, s.logger)
	}

	rows, err := s.findingsCH.ListRiskMCPServerCounts(ctx, chrepo.RiskOverviewWindowParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      authCtx.ProjectID.String(),
		From:           from,
		To:             to,
		MCPServerID:    "",
	}, riskMCPServerCountLimit)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load mcp server finding counts").LogError(ctx, s.logger)
	}

	rows, err = s.filterMCPServerCountsByRead(ctx, authCtx, rows)
	if err != nil {
		return nil, err
	}

	servers := make([]*gen.RiskMcpServerCount, 0, len(rows))
	for _, row := range rows {
		servers = append(servers, &gen.RiskMcpServerCount{
			McpServerID: row.MCPServerID,
			Findings:    safeCount(row.Findings),
		})
	}

	return &gen.RiskMcpServerCountsResult{Servers: servers}, nil
}

// filterMCPServerCountsByRead keeps only the servers the caller can read.
// Counts reveal a server's id and finding volume, so they follow the same
// per-server mcp:read rule as the MCP server list; org:admin alone does not
// imply it under scoped grants. A server that no longer exists is dropped.
func (s *Service) filterMCPServerCountsByRead(ctx context.Context, authCtx *contextvalues.AuthContext, rows []chrepo.RiskMCPServerCount) ([]chrepo.RiskMCPServerCount, error) {
	if len(rows) == 0 {
		return rows, nil
	}

	servers, err := readmodel.New(s.db).ListMCPServers(ctx, *authCtx.ProjectID, mcpserversrepo.ListMCPServersByProjectIDParams{
		ProjectID:            uuid.Nil,
		RemoteMcpServerID:    uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		TunneledMcpServerID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:            uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UnproxiedMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list mcp servers for finding counts").LogError(ctx, s.logger)
	}

	// Grants key on the toolset id for toolset-backed servers, else the server id.
	grantIDByServer := make(map[string]string, len(servers))
	checks := make([]authz.Check, 0, len(servers))
	for _, server := range servers {
		grantID := server.ID.String()
		if server.ToolsetID.Valid {
			grantID = server.ToolsetID.UUID.String()
		}
		grantIDByServer[server.ID.String()] = grantID
		checks = append(checks, authz.MCPCheck(authz.ScopeMCPRead, grantID, authCtx.ProjectID.String()))
	}
	allowedIDs, err := s.authz.Filter(ctx, checks)
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]struct{}, len(allowedIDs))
	for _, id := range allowedIDs {
		allowed[id] = struct{}{}
	}

	kept := make([]chrepo.RiskMCPServerCount, 0, len(rows))
	for _, row := range rows {
		grantID, known := grantIDByServer[row.MCPServerID]
		if !known {
			continue
		}
		if _, ok := allowed[grantID]; ok {
			kept = append(kept, row)
		}
	}
	return kept, nil
}
