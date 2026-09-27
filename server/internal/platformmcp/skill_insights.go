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

// SkillInsightsReader is the ClickHouse read behind get_skill_insights: exact
// activation mappings joined to session-grained usage cost and sampled efficacy
// scores, plus the observation watermark the freshness envelope is classified
// against. It is the same repository read the managed assistant's
// platform_skill_insights tool answers from, so both surfaces report one number.
type SkillInsightsReader interface {
	QuerySkillInsights(context.Context, telemetryrepo.QuerySkillInsightsParams) ([]telemetryrepo.SkillInsightBucket, error)
	GetTelemetryWatermark(context.Context, telemetryrepo.GetTelemetryWatermarkParams) (int64, error)
}

const (
	// skillInsightsDefaultLimit and skillInsightsMaxLimit bound how many ranked
	// skills one answer carries. Each skill also lists its versions, so the cap
	// is lower than list_skills' page size.
	skillInsightsDefaultLimit = 10
	skillInsightsMaxLimit     = 20

	// skillInsightsRegistryPage is how many skills the ranking mode reads from
	// the registry before ranking. A project with more active skills than this
	// is reported as truncated rather than silently ranked over a sample.
	skillInsightsRegistryPage = 200

	// skillInsightsVersionPage is how many versions of one skill are named.
	skillInsightsVersionPage = 50

	SkillInsightsModeRankSkills      = "rank_skills"
	SkillInsightsModeCompareVersions = "compare_versions"

	SkillInsightsSortEstimatedMinutesSaved = "estimated_minutes_saved"
	SkillInsightsSortEfficacy              = "efficacy"
	SkillInsightsSortActivations           = "activations"
	SkillInsightsSortSessionCost           = "session_cost"

	// skillInsightsCostAttribution and skillInsightsScoreCoverage travel on every
	// answer. A cost that fans out to every skill activated in a session is not
	// additive across skills, and a missing efficacy score is not a zero score;
	// stating both beside the numbers is what keeps a caller from summing or
	// averaging them into a claim the data does not support.
	skillInsightsCostAttribution = "full_session: the whole session's model cost is attributed to every skill version activated in that session, so totals across skills or versions are not additive."
	skillInsightsScoreCoverage   = "sampled: efficacy and estimated savings summarize scored sessions only; a skill without scores has unknown efficacy, not zero. The saved-turns and saved-minutes averages are over the scored sessions that carried an estimate, counted in estimated_turns_saved_samples and estimated_minutes_saved_samples, which can be fewer than scored_sessions."
)

// skillInsightsWindowSpec looks back a month by default and at most. Efficacy
// scoring is sampled, so a shorter default would report most skills as
// unscored; a longer maximum would turn the aggregate into a full-history scan.
var skillInsightsWindowSpec = windowSpec{Fallback: DiagnosticWindowLastMonth, Max: DiagnosticWindowLastMonth}

var skillInsightsSortKeys = []string{
	SkillInsightsSortEstimatedMinutesSaved,
	SkillInsightsSortEfficacy,
	SkillInsightsSortActivations,
	SkillInsightsSortSessionCost,
}

// GetSkillInsightsInput names the project and, optionally, one skill. Without a
// skill the project's skills are ranked; with one, that skill's versions are
// compared against each other.
type GetSkillInsightsInput struct {
	ProjectSlug string
	SkillID     string
	Window      string
	SortBy      string
	Limit       int
}

// GetSkillInsightsOutput is one project's skill efficacy, cost, and time-saved
// picture. It names skills and versions by registry ID; it never names a person
// or a session.
type GetSkillInsightsOutput struct {
	ProjectSlug string       `json:"project_slug"`
	Envelope    DataEnvelope `json:"data"`

	// Mode says which question was answered: rank_skills across the project or
	// compare_versions within one skill.
	Mode string `json:"mode"`

	// SortBy echoes the ranking key applied, including the default.
	SortBy string `json:"sort_by"`

	// ScoresAvailable is false when no session in the window was scored, in
	// which case every efficacy block is absent and sorting by an efficacy key
	// falls back to name order.
	ScoresAvailable bool `json:"scores_available"`

	// CostAttribution states how full_session_cost_usd was attributed.
	CostAttribution string `json:"cost_attribution"`

	// ScoreCoverage states which sessions the efficacy figures cover.
	ScoreCoverage string `json:"score_coverage"`

	Skills []SkillInsight `json:"skills"`

	// Truncated is true when not every skill or version is shown: the registry
	// page or a version page had more, or the ranking was cut at the limit.
	Truncated bool `json:"truncated"`
}

// SkillInsight is one skill's totals with a per-version breakdown.
type SkillInsight struct {
	ID          string                `json:"id"`
	Name        string                `json:"name"`
	DisplayName string                `json:"display_name"`
	Metrics     SkillInsightMetrics   `json:"metrics"`
	Versions    []SkillVersionInsight `json:"versions"`
}

// SkillVersionInsight is one version's share of the skill's totals. CreatedAt
// is empty for a version the registry page did not include.
type SkillVersionInsight struct {
	ID        string              `json:"id"`
	CreatedAt string              `json:"created_at,omitempty"`
	Metrics   SkillInsightMetrics `json:"metrics"`
}

// SkillInsightMetrics carries the exact activation counts, the full-session cost
// attribution, and, when any session was scored, the sampled efficacy figures.
type SkillInsightMetrics struct {
	Activations       uint64 `json:"activations"`
	ActivatedSessions uint64 `json:"activated_sessions"`

	// FullSessionCostUSD is the model cost of every session that activated this
	// skill, attributed in full. The same session's cost also lands on every
	// other skill it activated, so these figures are not additive across rows.
	FullSessionCostUSD float64 `json:"full_session_cost_usd"`

	// AverageFullSessionCostUSD is FullSessionCostUSD per activated session,
	// absent when nothing was activated.
	AverageFullSessionCostUSD *float64 `json:"average_full_session_cost_usd,omitempty"`

	// Efficacy is absent when no session was scored in the window.
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

// WithInsights attaches the ClickHouse insights read and the budget it is
// metered on. The budget is the observability lane the diagnostics summaries
// share, not the skills authoring allowance: the cost being metered is a
// ClickHouse aggregate, and authoring a skill must not be able to exhaust it.
// A nil reader leaves get_skill_insights registered as a stub.
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

// GetSkillInsights ranks a project's skills, or compares one skill's versions,
// by activations, sampled efficacy, full-session cost, and estimated time saved.
//
// Skill identity comes from the registry under the caller's own grants, exactly
// as list_skills resolves it, so a caller sees insights for precisely the skills
// it may read. The numbers come from ClickHouse keyed by those registry IDs.
func (s *SkillsService) GetSkillInsights(ctx context.Context, principal Principal, input GetSkillInsightsInput) (GetSkillInsightsOutput, error) {
	var zero GetSkillInsightsOutput
	if !s.insightsValid() {
		return zero, ErrSkillsUnavailable
	}
	input.SkillID = strings.TrimSpace(input.SkillID)
	if input.SkillID != "" {
		if _, err := uuid.Parse(input.SkillID); err != nil {
			return zero, fmt.Errorf("%w: skill_id must be a skill ID returned by list_skills", ErrRegistrationInvalid)
		}
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

	mode := SkillInsightsModeRankSkills
	skillByID := map[string]*types.Skill{}
	versionCreatedAt := map[string]string{}
	truncated := false
	var skillIDs []string
	if input.SkillID != "" {
		mode = SkillInsightsModeCompareVersions
		got, err := s.skills.Get(ctx, &genskills.GetPayload{ID: input.SkillID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
		if err != nil {
			return zero, fmt.Errorf("read skill %s from the registry: %w", input.SkillID, err)
		}
		if got.Skill == nil {
			return zero, fmt.Errorf("get skill %s: registry returned no skill", input.SkillID)
		}
		skillByID[got.Skill.ID] = got.Skill
		skillIDs = []string{got.Skill.ID}
		versions, err := s.skills.ListVersions(ctx, &genskills.ListVersionsPayload{ID: got.Skill.ID, Cursor: nil, Limit: skillInsightsVersionPage, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
		if err != nil {
			return zero, fmt.Errorf("list versions of skill %s: %w", got.Skill.ID, err)
		}
		truncated = versions.NextCursor != nil
		for _, version := range versions.Versions {
			versionCreatedAt[version.ID] = version.CreatedAt
		}
	} else {
		listed, err := s.skills.List(ctx, &genskills.ListPayload{
			Cursor: nil, Limit: skillInsightsRegistryPage, Search: nil, SourceKinds: nil, Classifications: nil, Tags: nil, AccessibleBy: nil, Sort: "name",
			SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		})
		if err != nil {
			return zero, fmt.Errorf("list project skills: %w", err)
		}
		truncated = listed.NextCursor != nil
		for _, skill := range listed.Skills {
			skillByID[skill.ID] = skill
			skillIDs = append(skillIDs, skill.ID)
		}
	}

	var rows []telemetryrepo.SkillInsightBucket
	if len(skillIDs) > 0 {
		rows, err = s.insights.QuerySkillInsights(ctx, telemetryrepo.QuerySkillInsightsParams{
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
			return zero, fmt.Errorf("read skill insights: %w", err)
		}
	}
	watermark, err := s.insights.GetTelemetryWatermark(ctx, telemetryrepo.GetTelemetryWatermarkParams{GramProjectIDs: []string{project.ID.String()}})
	if err != nil {
		return zero, fmt.Errorf("read skill insights watermark: %w", err)
	}

	skills, scoresAvailable := rankSkillInsights(rows, skillByID, versionCreatedAt, sortBy)
	if len(skills) > limit {
		skills = skills[:limit]
		truncated = true
	}
	if mode == SkillInsightsModeRankSkills {
		// Version creation times come from the registry, one page per ranked
		// skill. Reading them for every skill in the project before ranking
		// would spend a page on each skill the answer then drops.
		for i := range skills {
			versions, err := s.skills.ListVersions(ctx, &genskills.ListVersionsPayload{ID: skills[i].ID, Cursor: nil, Limit: skillInsightsVersionPage, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
			if err != nil {
				return zero, fmt.Errorf("list versions of skill %s: %w", skills[i].ID, err)
			}
			truncated = truncated || versions.NextCursor != nil
			createdAt := make(map[string]string, len(versions.Versions))
			for _, version := range versions.Versions {
				createdAt[version.ID] = version.CreatedAt
			}
			for j := range skills[i].Versions {
				skills[i].Versions[j].CreatedAt = createdAt[skills[i].Versions[j].ID]
			}
			sortSkillVersionInsights(skills[i].Versions)
		}
	}

	return GetSkillInsightsOutput{
		ProjectSlug:     project.Slug,
		Envelope:        newDataEnvelope(now, watermarkTime(watermark), window, len(rows) > 0),
		Mode:            mode,
		SortBy:          sortBy,
		ScoresAvailable: scoresAvailable,
		CostAttribution: skillInsightsCostAttribution,
		ScoreCoverage:   skillInsightsScoreCoverage,
		Skills:          skills,
		Truncated:       truncated,
	}, nil
}

// rankSkillInsights folds bucket rows into per-skill and per-version totals and
// orders the skills by sortBy, highest first, then by name so equal values keep
// a stable order. Rows for a skill the registry did not return are dropped: the
// registry read ran under the caller's grants and is the authority on what the
// caller may see.
//
// In compare mode every version the registry named is present even with no
// activity, so a version that was never activated reads as zero rather than
// disappearing from the comparison.
func rankSkillInsights(rows []telemetryrepo.SkillInsightBucket, skills map[string]*types.Skill, versionCreatedAt map[string]string, sortBy string) ([]SkillInsight, bool) {
	bySkill := map[string]map[string]*telemetryrepo.SkillInsightBucket{}
	if len(skills) == 1 && len(versionCreatedAt) > 0 {
		for skillID := range skills {
			bySkill[skillID] = make(map[string]*telemetryrepo.SkillInsightBucket, len(versionCreatedAt))
			for versionID := range versionCreatedAt {
				bySkill[skillID][versionID] = &telemetryrepo.SkillInsightBucket{}
			}
		}
	}
	scoresAvailable := false
	for _, row := range rows {
		if skills[row.SkillID] == nil {
			continue
		}
		versions := bySkill[row.SkillID]
		if versions == nil {
			versions = map[string]*telemetryrepo.SkillInsightBucket{}
			bySkill[row.SkillID] = versions
		}
		total := versions[row.SkillVersionID]
		if total == nil {
			total = &telemetryrepo.SkillInsightBucket{}
			versions[row.SkillVersionID] = total
		}
		addSkillInsightBucket(total, row)
		scoresAvailable = scoresAvailable || row.ScoredSessions > 0
	}

	items := make([]SkillInsight, 0, len(bySkill))
	for skillID, versions := range bySkill {
		skill := skills[skillID]
		item := SkillInsight{ID: skill.ID, Name: skill.Name, DisplayName: skill.DisplayName, Versions: make([]SkillVersionInsight, 0, len(versions))}
		var skillTotal telemetryrepo.SkillInsightBucket
		for versionID, total := range versions {
			addSkillInsightBucket(&skillTotal, *total)
			item.Versions = append(item.Versions, SkillVersionInsight{ID: versionID, CreatedAt: versionCreatedAt[versionID], Metrics: skillInsightMetrics(*total)})
		}
		item.Metrics = skillInsightMetrics(skillTotal)
		sortSkillVersionInsights(item.Versions)
		items = append(items, item)
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

// skillInsightSortValue is the ranking key. A skill with no efficacy scores
// ranks below every scored skill on an efficacy key, and below a zero score:
// unknown is not zero, and it must not outrank a skill that was measured.
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
