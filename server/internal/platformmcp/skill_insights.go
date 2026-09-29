//nolint:exhaustruct // Insight projections leave documented optional fields at their zero values.
package platformmcp

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/google/uuid"

	genskills "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/gen/types"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// SkillInsightsReader is the ClickHouse read behind the two insight tools:
// exact activation mappings joined to session-grained usage cost and sampled
// efficacy scores, plus the observation watermark the freshness envelope is
// classified against. It is the same repository read the managed assistant's
// platform_skill_insights tool answers from, so both surfaces report one number.
type SkillInsightsReader interface {
	QuerySkillInsights(context.Context, telemetryrepo.QuerySkillInsightsParams) ([]telemetryrepo.SkillInsightBucket, error)
	GetTelemetryWatermark(context.Context, telemetryrepo.GetTelemetryWatermarkParams) (int64, error)
}

const (
	// skillInsightsDefaultLimit and skillInsightsMaxLimit bound how many ranked
	// skills one answer carries.
	skillInsightsDefaultLimit = 10
	skillInsightsMaxLimit     = 20

	// skillInsightsRegistryPage is how many skills the ranking read takes from
	// the registry before ranking. A project with more skills than this is
	// reported as truncated rather than silently ranked over a sample.
	skillInsightsRegistryPage = 200

	// skillInsightsVersionPage is how many versions of one skill are compared.
	skillInsightsVersionPage = 50

	SkillInsightsSortEstimatedMinutesSaved = "estimated_minutes_saved"
	SkillInsightsSortEfficacy              = "efficacy"
	SkillInsightsSortActivations           = "activations"
	SkillInsightsSortSessionCost           = "session_cost"

	// skillInsightsCostAttribution and skillInsightsScoreCoverage travel on every
	// answer. A cost that fans out to every skill used in a session is not
	// additive across skills, and a missing score is not a zero score; saying
	// both beside the numbers is what keeps a caller from adding or averaging
	// them into a claim the data does not support.
	skillInsightsCostAttribution = "Each session's whole model cost is counted against every skill used in that session, so costs do not add up across skills or versions."
	skillInsightsScoreCoverage   = "Scores and saved-time estimates come from sessions that were sampled and scored, so a skill with no scores is unmeasured rather than bad. The saved-turns and saved-minutes averages divide by the scored sessions that carried an estimate — estimated_turns_saved_samples and estimated_minutes_saved_samples — which can be fewer than scored_sessions."
)

// skillInsightsWindowSpec looks back a month by default and at most. Scoring is
// sampled, so a shorter default would report most skills as unscored; a longer
// maximum would turn the aggregate into a full-history scan.
var skillInsightsWindowSpec = windowSpec{Fallback: DiagnosticWindowLastMonth, Max: DiagnosticWindowLastMonth}

var skillInsightsSortKeys = []string{
	SkillInsightsSortEstimatedMinutesSaved,
	SkillInsightsSortEfficacy,
	SkillInsightsSortActivations,
	SkillInsightsSortSessionCost,
}

// ListSkillInsightsInput ranks the skills of one project.
type ListSkillInsightsInput struct {
	ProjectSlug string
	Window      string
	SortBy      string
	Limit       int
}

// ListSkillInsightsOutput is one project's skills ranked by what they returned.
// It names skills by registry ID; it never names a person or a session.
type ListSkillInsightsOutput struct {
	ProjectSlug string       `json:"project_slug"`
	Envelope    DataEnvelope `json:"data"`

	// SortBy echoes the ranking key applied, including the default.
	SortBy string `json:"sort_by"`

	// ScoresAvailable is false when no session in the window was scored, in
	// which case every efficacy block is absent and sorting by an efficacy key
	// falls back to name order.
	ScoresAvailable bool `json:"scores_available"`

	// CostAttribution says how full_session_cost_usd was counted.
	CostAttribution string `json:"cost_attribution"`

	// ScoreCoverage says which sessions the score and saved-time figures cover.
	ScoreCoverage string `json:"score_coverage"`

	Skills []SkillInsight `json:"skills"`

	// Truncated is true when the project holds more skills than were ranked,
	// either because the registry page filled or because the ranking was cut at
	// the requested limit.
	Truncated bool `json:"truncated"`
}

// SkillInsight is one skill's totals over the window. Per-version figures come
// from compare_skill_versions rather than from here, so ranking a project and
// comparing one skill stay two separate answers.
type SkillInsight struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	DisplayName string              `json:"display_name"`
	Metrics     SkillInsightMetrics `json:"metrics"`
}

// CompareSkillVersionsInput compares the versions of one named skill.
type CompareSkillVersionsInput struct {
	ProjectSlug string
	SkillID     string
	Window      string
}

// CompareSkillVersionsOutput is one skill's versions measured against each
// other, newest first.
type CompareSkillVersionsOutput struct {
	ProjectSlug string       `json:"project_slug"`
	Envelope    DataEnvelope `json:"data"`

	SkillID          string `json:"skill_id"`
	SkillName        string `json:"skill_name"`
	SkillDisplayName string `json:"skill_display_name"`

	// Metrics totals every version below, so the skill's own figures match what
	// list_skill_insights reports for it.
	Metrics SkillInsightMetrics `json:"metrics"`

	Versions []SkillVersionInsight `json:"versions"`

	// ScoresAvailable is false when no session in the window was scored.
	ScoresAvailable bool `json:"scores_available"`

	// CostAttribution says how full_session_cost_usd was counted.
	CostAttribution string `json:"cost_attribution"`

	// ScoreCoverage says which sessions the score and saved-time figures cover.
	ScoreCoverage string `json:"score_coverage"`

	// Truncated is true when the skill holds more versions than were compared.
	Truncated bool `json:"truncated"`
}

// SkillVersionInsight is one version's share of the skill's totals. CreatedAt
// is empty for a version beyond the registry page that was nonetheless active.
type SkillVersionInsight struct {
	ID        string              `json:"id"`
	CreatedAt string              `json:"created_at,omitempty"`
	Metrics   SkillInsightMetrics `json:"metrics"`
}

// SkillInsightMetrics carries the exact activation counts, the session cost
// counted against this skill, and, when any session was scored, the sampled
// score and saved-time figures.
type SkillInsightMetrics struct {
	Activations       uint64 `json:"activations"`
	ActivatedSessions uint64 `json:"activated_sessions"`

	// FullSessionCostUSD is the model cost of every session that used this
	// skill, counted in full. The same session's cost also lands on every other
	// skill it used, so these figures are not additive across rows.
	FullSessionCostUSD float64 `json:"full_session_cost_usd"`

	// AverageFullSessionCostUSD is FullSessionCostUSD per session, absent when
	// nothing used the skill.
	AverageFullSessionCostUSD *float64 `json:"average_full_session_cost_usd,omitempty"`

	// Efficacy is absent when no session using this skill was scored.
	Efficacy *SkillEfficacyMetrics `json:"efficacy,omitempty"`
}

// SkillEfficacyMetrics summarizes sampled judge scores. Every figure covers
// scored sessions only, and a judge may score a session without estimating
// what it saved, so each savings average names the sample count it divides by.
type SkillEfficacyMetrics struct {
	ScoredSessions uint64  `json:"scored_sessions"`
	AverageScore   float64 `json:"average_score"`

	// EstimatedTurnsSavedTotal sums the turns-saved estimates that were made.
	EstimatedTurnsSavedTotal float64 `json:"estimated_turns_saved_total"`

	// EstimatedTurnsSavedAverage is the total over EstimatedTurnsSavedSamples,
	// absent when no scored session carried an estimate.
	EstimatedTurnsSavedAverage *float64 `json:"estimated_turns_saved_average,omitempty"`

	// EstimatedTurnsSavedSamples is how many scored sessions carried a
	// turns-saved estimate. It is at most ScoredSessions.
	EstimatedTurnsSavedSamples uint64 `json:"estimated_turns_saved_samples"`

	// EstimatedMinutesSavedTotal sums the minutes-saved estimates that were made.
	EstimatedMinutesSavedTotal float64 `json:"estimated_minutes_saved_total"`

	// EstimatedMinutesSavedAverage is the total over
	// EstimatedMinutesSavedSamples, absent when no scored session carried an
	// estimate.
	EstimatedMinutesSavedAverage *float64 `json:"estimated_minutes_saved_average,omitempty"`

	// EstimatedMinutesSavedSamples is how many scored sessions carried a
	// minutes-saved estimate. It is at most ScoredSessions.
	EstimatedMinutesSavedSamples uint64 `json:"estimated_minutes_saved_samples"`

	ROIConfidenceCounts map[string]uint64 `json:"roi_confidence_counts"`
	FlagCounts          map[string]uint64 `json:"flag_counts"`
}

// WithInsights attaches the ClickHouse read behind the insight tools and the
// rate limit they answer under.
//
// They are limited with the other telemetry reads — 60 calls a minute per
// connection — rather than with skill authoring and distribution, which get 5,
// because the work being limited here is a ClickHouse scan and an administrator
// reading through a project's skills would otherwise exhaust the allowance
// writing a skill needs.
//
// A nil reader leaves both tools registered as stubs.
func (s *SkillsService) WithInsights(insights SkillInsightsReader, budget OperationBudget) *SkillsService {
	if s == nil {
		return nil
	}
	s.insights = insights
	s.insightsBudget = budget
	return s
}

func (s *SkillsService) insightsValid() bool {
	return s.valid() && s.insights != nil && s.insightsBudget.valid() && s.now != nil
}

// ListSkillInsights ranks a project's skills by activations, sampled efficacy,
// session cost, and estimated time saved.
//
// Which skills exist comes from the registry under the caller's own grants,
// exactly as list_skills resolves them, so a caller sees insights for precisely
// the skills it may read. The numbers come from ClickHouse keyed by those
// registry IDs.
func (s *SkillsService) ListSkillInsights(ctx context.Context, principal Principal, input ListSkillInsightsInput) (ListSkillInsightsOutput, error) {
	var zero ListSkillInsightsOutput
	if !s.insightsValid() {
		return zero, ErrSkillsUnavailable
	}
	sortBy := strings.ToLower(strings.TrimSpace(input.SortBy))
	if sortBy == "" {
		sortBy = SkillInsightsSortEstimatedMinutesSaved
	}
	if !slices.Contains(skillInsightsSortKeys, sortBy) {
		return zero, fmt.Errorf("%w: sort_by must be one of %s", ErrRegistrationInvalid, strings.Join(skillInsightsSortKeys, ", "))
	}
	limit := input.Limit
	switch {
	case limit <= 0:
		limit = skillInsightsDefaultLimit
	case limit > skillInsightsMaxLimit:
		limit = skillInsightsMaxLimit
	}
	now := s.now()
	window, err := resolveWindow(input.Window, now, skillInsightsWindowSpec)
	if err != nil {
		return zero, fmt.Errorf("%w: %w", ErrRegistrationInvalid, err)
	}

	ctx, project, err := s.beginWith(ctx, principal, input.ProjectSlug, s.insightsBudget)
	if err != nil {
		return zero, err
	}

	listed, err := s.skills.List(ctx, &genskills.ListPayload{
		Cursor: nil, Limit: skillInsightsRegistryPage, Search: nil, SourceKinds: nil, Classifications: nil, Tags: nil, AccessibleBy: nil, Sort: "name",
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	if err != nil {
		return zero, fmt.Errorf("list project skills: %w", err)
	}
	truncated := listed.NextCursor != nil
	skillByID := make(map[string]*types.Skill, len(listed.Skills))
	skillIDs := make([]string, 0, len(listed.Skills))
	for _, skill := range listed.Skills {
		skillByID[skill.ID] = skill
		skillIDs = append(skillIDs, skill.ID)
	}

	rows, watermark, err := s.readSkillInsights(ctx, principal, project, window, skillIDs)
	if err != nil {
		return zero, err
	}

	skills, scoresAvailable := rankSkillInsights(rows, skillByID, sortBy)
	if len(skills) > limit {
		skills = skills[:limit]
		truncated = true
	}
	return ListSkillInsightsOutput{
		ProjectSlug:     project.Slug,
		Envelope:        newDataEnvelope(now, watermarkTime(watermark), window, len(rows) > 0),
		SortBy:          sortBy,
		ScoresAvailable: scoresAvailable,
		CostAttribution: skillInsightsCostAttribution,
		ScoreCoverage:   skillInsightsScoreCoverage,
		Skills:          skills,
		Truncated:       truncated,
	}, nil
}

// CompareSkillVersions measures one skill's versions against each other over
// the same window and with the same numbers ListSkillInsights reports.
func (s *SkillsService) CompareSkillVersions(ctx context.Context, principal Principal, input CompareSkillVersionsInput) (CompareSkillVersionsOutput, error) {
	var zero CompareSkillVersionsOutput
	if !s.insightsValid() {
		return zero, ErrSkillsUnavailable
	}
	skillID := strings.TrimSpace(input.SkillID)
	if _, err := uuid.Parse(skillID); err != nil {
		return zero, fmt.Errorf("%w: skill_id must be a skill ID returned by list_skills", ErrRegistrationInvalid)
	}
	now := s.now()
	window, err := resolveWindow(input.Window, now, skillInsightsWindowSpec)
	if err != nil {
		return zero, fmt.Errorf("%w: %w", ErrRegistrationInvalid, err)
	}

	ctx, project, err := s.beginWith(ctx, principal, input.ProjectSlug, s.insightsBudget)
	if err != nil {
		return zero, err
	}

	got, err := s.skills.Get(ctx, &genskills.GetPayload{ID: skillID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	if err != nil {
		return zero, fmt.Errorf("read skill %s from the registry: %w", skillID, err)
	}
	if got.Skill == nil {
		return zero, fmt.Errorf("get skill %s: registry returned no skill", skillID)
	}
	versions, err := s.skills.ListVersions(ctx, &genskills.ListVersionsPayload{ID: got.Skill.ID, Cursor: nil, Limit: skillInsightsVersionPage, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	if err != nil {
		return zero, fmt.Errorf("list versions of skill %s: %w", got.Skill.ID, err)
	}
	versionCreatedAt := make(map[string]string, len(versions.Versions))
	for _, version := range versions.Versions {
		versionCreatedAt[version.ID] = version.CreatedAt
	}

	rows, watermark, err := s.readSkillInsights(ctx, principal, project, window, []string{got.Skill.ID})
	if err != nil {
		return zero, err
	}

	total, compared, scoresAvailable := compareSkillVersionInsights(rows, got.Skill.ID, versionCreatedAt)
	return CompareSkillVersionsOutput{
		ProjectSlug:      project.Slug,
		Envelope:         newDataEnvelope(now, watermarkTime(watermark), window, len(rows) > 0),
		SkillID:          got.Skill.ID,
		SkillName:        got.Skill.Name,
		SkillDisplayName: got.Skill.DisplayName,
		Metrics:          skillInsightMetrics(total),
		Versions:         compared,
		ScoresAvailable:  scoresAvailable,
		CostAttribution:  skillInsightsCostAttribution,
		ScoreCoverage:    skillInsightsScoreCoverage,
		Truncated:        versions.NextCursor != nil,
	}, nil
}

// readSkillInsights is the ClickHouse half both tools share, so a skill's
// figures are the same whichever tool reported them. The whole window is one
// bucket: neither tool draws a trend line, and asking for finer buckets would
// only make the caller re-add them.
func (s *SkillsService) readSkillInsights(ctx context.Context, principal Principal, project ResolvedProject, window ResolvedWindow, skillIDs []string) ([]telemetryrepo.SkillInsightBucket, int64, error) {
	var rows []telemetryrepo.SkillInsightBucket
	if len(skillIDs) > 0 {
		queried, err := s.insights.QuerySkillInsights(ctx, telemetryrepo.QuerySkillInsightsParams{
			OrganizationID:      principal.OrganizationID,
			ProjectID:           project.ID.String(),
			SkillIDs:            skillIDs,
			SkillVersionIDs:     nil,
			From:                window.start,
			To:                  window.end,
			IntervalSeconds:     int64(window.end.Sub(window.start).Seconds()),
			IncludeSessionUsage: true,
		})
		if err != nil {
			return nil, 0, fmt.Errorf("read skill insights: %w", err)
		}
		rows = queried
	}
	watermark, err := s.insights.GetTelemetryWatermark(ctx, telemetryrepo.GetTelemetryWatermarkParams{GramProjectIDs: []string{project.ID.String()}})
	if err != nil {
		return nil, 0, fmt.Errorf("read skill insights watermark: %w", err)
	}
	return rows, watermark, nil
}

// rankSkillInsights totals each skill's rows and orders the skills by sortBy,
// highest first, then by name so equal values keep a stable order. Rows for a
// skill the registry did not return are dropped: the registry read ran under
// the caller's grants and is the authority on what the caller may see.
func rankSkillInsights(rows []telemetryrepo.SkillInsightBucket, skills map[string]*types.Skill, sortBy string) ([]SkillInsight, bool) {
	totals := map[string]*telemetryrepo.SkillInsightBucket{}
	scoresAvailable := false
	for _, row := range rows {
		if skills[row.SkillID] == nil {
			continue
		}
		total := totals[row.SkillID]
		if total == nil {
			total = &telemetryrepo.SkillInsightBucket{}
			totals[row.SkillID] = total
		}
		addSkillInsightBucket(total, row)
		scoresAvailable = scoresAvailable || row.ScoredSessions > 0
	}

	items := make([]SkillInsight, 0, len(totals))
	for skillID, total := range totals {
		skill := skills[skillID]
		items = append(items, SkillInsight{
			ID:          skill.ID,
			Name:        skill.Name,
			DisplayName: skill.DisplayName,
			Metrics:     skillInsightMetrics(*total),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		left, right := skillInsightSortValue(items[i].Metrics, sortBy), skillInsightSortValue(items[j].Metrics, sortBy)
		if left == right {
			return items[i].Name < items[j].Name
		}
		return left > right
	})
	return items, scoresAvailable
}

// compareSkillVersionInsights totals one skill's rows per version and returns
// them newest first, alongside the skill's total across them.
//
// Every version the registry named is present even with no activity, so a
// version nobody used reads as zero rather than dropping out of the comparison.
// A version that was used but sits beyond the registry page is still reported,
// without a creation time, rather than having its activity disappear.
func compareSkillVersionInsights(rows []telemetryrepo.SkillInsightBucket, skillID string, versionCreatedAt map[string]string) (telemetryrepo.SkillInsightBucket, []SkillVersionInsight, bool) {
	totals := make(map[string]*telemetryrepo.SkillInsightBucket, len(versionCreatedAt))
	for versionID := range versionCreatedAt {
		totals[versionID] = &telemetryrepo.SkillInsightBucket{}
	}
	var skillTotal telemetryrepo.SkillInsightBucket
	scoresAvailable := false
	for _, row := range rows {
		if row.SkillID != skillID {
			continue
		}
		total := totals[row.SkillVersionID]
		if total == nil {
			total = &telemetryrepo.SkillInsightBucket{}
			totals[row.SkillVersionID] = total
		}
		addSkillInsightBucket(total, row)
		addSkillInsightBucket(&skillTotal, row)
		scoresAvailable = scoresAvailable || row.ScoredSessions > 0
	}

	compared := make([]SkillVersionInsight, 0, len(totals))
	for versionID, total := range totals {
		compared = append(compared, SkillVersionInsight{
			ID:        versionID,
			CreatedAt: versionCreatedAt[versionID],
			Metrics:   skillInsightMetrics(*total),
		})
	}
	sortSkillVersionInsights(compared)
	return skillTotal, compared, scoresAvailable
}

// sortSkillVersionInsights orders newest first; versions without a known
// creation time sort last, by ID.
func sortSkillVersionInsights(versions []SkillVersionInsight) {
	sort.Slice(versions, func(i, j int) bool {
		if versions[i].CreatedAt == versions[j].CreatedAt {
			return versions[i].ID < versions[j].ID
		}
		return versions[i].CreatedAt > versions[j].CreatedAt
	})
}

func addSkillInsightBucket(dst *telemetryrepo.SkillInsightBucket, src telemetryrepo.SkillInsightBucket) {
	dst.ActivationCount += src.ActivationCount
	dst.ActivatedSessions += src.ActivatedSessions
	dst.TotalSessionCost += src.TotalSessionCost
	dst.ScoredSessions += src.ScoredSessions
	dst.ScoreSum += src.ScoreSum
	dst.EstimatedTurnsSavedSum += src.EstimatedTurnsSavedSum
	dst.EstimatedTurnsSamples += src.EstimatedTurnsSamples
	dst.EstimatedMinutesSavedSum += src.EstimatedMinutesSavedSum
	dst.EstimatedMinutesSamples += src.EstimatedMinutesSamples
	dst.ROIConfidenceLow += src.ROIConfidenceLow
	dst.ROIConfidenceMed += src.ROIConfidenceMed
	dst.ROIConfidenceHigh += src.ROIConfidenceHigh
	dst.IgnoredCount += src.IgnoredCount
	dst.MisappliedCount += src.MisappliedCount
	dst.PartiallyFollowedCount += src.PartiallyFollowedCount
	dst.HarmfulCount += src.HarmfulCount
}

func skillInsightMetrics(total telemetryrepo.SkillInsightBucket) SkillInsightMetrics {
	metrics := SkillInsightMetrics{
		Activations:               total.ActivationCount,
		ActivatedSessions:         total.ActivatedSessions,
		FullSessionCostUSD:        total.TotalSessionCost,
		AverageFullSessionCostUSD: skillInsightRatio(total.TotalSessionCost, total.ActivatedSessions),
		Efficacy:                  nil,
	}
	if total.ScoredSessions == 0 {
		return metrics
	}
	metrics.Efficacy = &SkillEfficacyMetrics{
		ScoredSessions:               total.ScoredSessions,
		AverageScore:                 total.ScoreSum / float64(total.ScoredSessions),
		EstimatedTurnsSavedTotal:     total.EstimatedTurnsSavedSum,
		EstimatedTurnsSavedAverage:   skillInsightRatio(total.EstimatedTurnsSavedSum, total.EstimatedTurnsSamples),
		EstimatedTurnsSavedSamples:   total.EstimatedTurnsSamples,
		EstimatedMinutesSavedTotal:   total.EstimatedMinutesSavedSum,
		EstimatedMinutesSavedAverage: skillInsightRatio(total.EstimatedMinutesSavedSum, total.EstimatedMinutesSamples),
		EstimatedMinutesSavedSamples: total.EstimatedMinutesSamples,
		ROIConfidenceCounts:          map[string]uint64{"low": total.ROIConfidenceLow, "med": total.ROIConfidenceMed, "high": total.ROIConfidenceHigh},
		FlagCounts:                   map[string]uint64{"ignored": total.IgnoredCount, "misapplied": total.MisappliedCount, "partially_followed": total.PartiallyFollowedCount, "harmful": total.HarmfulCount},
	}
	return metrics
}

func skillInsightRatio(sum float64, count uint64) *float64 {
	if count == 0 {
		return nil
	}
	value := sum / float64(count)
	return &value
}

// skillInsightSortValue is the ranking key. A skill with no scores ranks below
// every scored skill on a score-based key, and below a zero score: unmeasured
// is not zero, and it must not outrank a skill that was measured.
func skillInsightSortValue(metrics SkillInsightMetrics, sortBy string) float64 {
	switch sortBy {
	case SkillInsightsSortEfficacy:
		if metrics.Efficacy != nil {
			return metrics.Efficacy.AverageScore
		}
	case SkillInsightsSortActivations:
		return float64(metrics.Activations)
	case SkillInsightsSortSessionCost:
		return metrics.FullSessionCostUSD
	case SkillInsightsSortEstimatedMinutesSaved:
		if metrics.Efficacy != nil {
			return metrics.Efficacy.EstimatedMinutesSavedTotal
		}
	}
	return -1
}
