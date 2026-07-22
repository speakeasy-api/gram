package skills

import (
	"context"
	"fmt"
	"io"
	"slices"
	"sort"
	"time"

	"github.com/google/uuid"

	genskills "github.com/speakeasy-api/gram/server/gen/skills"
	gentypes "github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const (
	insightDefaultWindow = 30 * 24 * time.Hour
	insightMaxWindow     = 90 * 24 * time.Hour
	insightInterval      = 24 * time.Hour
)

type Insights struct {
	skills   SkillsService
	insights SkillInsightsReader
}

type insightsInput struct {
	SkillID *string `json:"skill_id,omitempty" jsonschema:"Only report this skill ID. Omit to rank active project skills."`
	From    *string `json:"from,omitempty" jsonschema:"Window start in RFC3339. Defaults to 30 days before to."`
	To      *string `json:"to,omitempty" jsonschema:"Window end in RFC3339. Defaults to now."`
	SortBy  string  `json:"sort_by,omitempty" jsonschema:"Rank skills by estimated_minutes_saved, efficacy, activations, or session_cost."`
	Limit   int     `json:"limit,omitempty" jsonschema:"Maximum skills to return (1-20)."`
}

type insightsResult struct {
	From            string         `json:"from"`
	To              string         `json:"to"`
	ScoresAvailable bool           `json:"scores_available"`
	SkillsTruncated bool           `json:"skills_truncated"`
	CostAttribution string         `json:"cost_attribution"`
	ScoreCoverage   string         `json:"score_coverage"`
	Skills          []skillInsight `json:"skills"`
}

type skillInsight struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	DisplayName string           `json:"display_name"`
	Metrics     insightMetrics   `json:"metrics"`
	Versions    []versionInsight `json:"versions"`
}

type versionInsight struct {
	ID        string         `json:"id"`
	CreatedAt string         `json:"created_at,omitempty"`
	Metrics   insightMetrics `json:"metrics"`
	Trend     []insightPoint `json:"trend"`
}

type insightMetrics struct {
	Activations           uint64           `json:"activations"`
	ActivatedSessions     uint64           `json:"activated_sessions"`
	SessionCostUSD        float64          `json:"session_cost_usd"`
	AverageSessionCostUSD *float64         `json:"average_session_cost_usd,omitempty"`
	Efficacy              *efficacyMetrics `json:"efficacy,omitempty"`
}

type efficacyMetrics struct {
	ScoredSessions               uint64            `json:"scored_sessions"`
	AverageScore                 float64           `json:"average_score"`
	EstimatedTurnsSavedTotal     float64           `json:"estimated_turns_saved_total"`
	EstimatedTurnsSavedAverage   *float64          `json:"estimated_turns_saved_average,omitempty"`
	EstimatedTurnsSavedSamples   uint64            `json:"estimated_turns_saved_samples"`
	EstimatedMinutesSavedTotal   float64           `json:"estimated_minutes_saved_total"`
	EstimatedMinutesSavedAverage *float64          `json:"estimated_minutes_saved_average,omitempty"`
	EstimatedMinutesSavedSamples uint64            `json:"estimated_minutes_saved_samples"`
	ROIConfidenceCounts          map[string]uint64 `json:"roi_confidence_counts"`
	FlagCounts                   map[string]uint64 `json:"flag_counts"`
}

type insightPoint struct {
	BucketStart           string   `json:"bucket_start"`
	Activations           uint64   `json:"activations"`
	ActivatedSessions     uint64   `json:"activated_sessions"`
	SessionCostUSD        float64  `json:"session_cost_usd"`
	ScoredSessions        uint64   `json:"scored_sessions"`
	AverageScore          *float64 `json:"average_score,omitempty"`
	EstimatedMinutesSaved float64  `json:"estimated_minutes_saved"`
}

type insightAccumulator struct {
	metrics telemetryrepo.SkillInsightBucket
	trend   []insightPoint
}

func NewInsightsTool(skills SkillsService, insights SkillInsightsReader) *Insights {
	return &Insights{skills: skills, insights: insights}
}

func (t *Insights) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "skills",
		HandlerName: "skill_insights",
		Name:        "platform_skill_insights",
		Description: "Rank active project skills or compare one skill's versions by activations, sampled efficacy, attributed session cost, and estimated time saved. Costs are full-session and fan out to every skill used in the session; efficacy and savings cover sampled scored sessions only.",
		InputSchema: core.BuildInputSchema[insightsInput](
			core.WithPropertyFormat("skill_id", "uuid"),
			core.WithPropertyFormat("from", "date-time"),
			core.WithPropertyFormat("to", "date-time"),
			core.WithPropertyEnum("sort_by", "estimated_minutes_saved", "efficacy", "activations", "session_cost"),
			core.WithPropertyNumberRange("limit", 1, 20),
		),
		Variables:   nil,
		Annotations: core.ReadOnlyAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (t *Insights) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if t.skills == nil || t.insights == nil {
		return fmt.Errorf("skill insights dependencies not configured")
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx.ProjectID == nil {
		return fmt.Errorf("skill insights requires project auth context")
	}

	input := insightsInput{SkillID: nil, From: nil, To: nil, SortBy: "estimated_minutes_saved", Limit: 10}
	if err := core.DecodeInput(payload, &input); err != nil {
		return err
	}
	if input.Limit == 0 {
		input.Limit = 10
	}
	if input.Limit < 1 || input.Limit > 20 {
		return fmt.Errorf("limit must be between 1 and 20")
	}
	if input.SortBy == "" {
		input.SortBy = "estimated_minutes_saved"
	}
	if !slices.Contains([]string{"estimated_minutes_saved", "efficacy", "activations", "session_cost"}, input.SortBy) {
		return fmt.Errorf("unsupported sort_by %q", input.SortBy)
	}
	from, to, err := insightWindow(input.From, input.To)
	if err != nil {
		return err
	}

	skillByID := map[string]*gentypes.Skill{}
	versionCreatedAt := map[string]string{}
	truncated := false
	var skillIDs []string
	if input.SkillID != nil {
		if _, err := uuid.Parse(*input.SkillID); err != nil {
			return fmt.Errorf("skill_id must be a UUID")
		}
		got, err := t.skills.Get(ctx, &genskills.GetPayload{ID: *input.SkillID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
		if err != nil {
			return fmt.Errorf("get skill: %w", err)
		}
		if got.Skill == nil {
			return fmt.Errorf("get skill returned no skill")
		}
		skillByID[got.Skill.ID] = got.Skill
		skillIDs = []string{got.Skill.ID}
		versions, err := t.skills.ListVersions(ctx, &genskills.ListVersionsPayload{ID: got.Skill.ID, Cursor: nil, Limit: 50, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
		if err != nil {
			return fmt.Errorf("list skill versions: %w", err)
		}
		truncated = versions.NextCursor != nil
		for _, version := range versions.Versions {
			versionCreatedAt[version.ID] = version.CreatedAt
		}
	} else {
		listed, err := t.skills.List(ctx, &genskills.ListPayload{Cursor: nil, Limit: 200, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
		if err != nil {
			return fmt.Errorf("list skills: %w", err)
		}
		truncated = listed.NextCursor != nil
		for _, skill := range listed.Skills {
			skillByID[skill.ID] = skill
			skillIDs = append(skillIDs, skill.ID)
		}
	}
	if len(skillIDs) == 0 {
		result := buildInsightsResult(nil, skillByID, versionCreatedAt, input.SortBy, input.Limit)
		result.From = from.Format(time.RFC3339)
		result.To = to.Format(time.RFC3339)
		result.SkillsTruncated = truncated
		return core.EncodeResult(wr, result)
	}

	rows, err := t.insights.QuerySkillInsights(ctx, telemetryrepo.QuerySkillInsightsParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		ProjectID:       authCtx.ProjectID.String(),
		SkillIDs:        skillIDs,
		SkillVersionIDs: nil,
		From:            from,
		To:              to,
		IntervalSeconds: int64(insightInterval.Seconds()),
	})
	if err != nil {
		return fmt.Errorf("query skill insights: %w", err)
	}

	result := buildInsightsResult(rows, skillByID, versionCreatedAt, input.SortBy, input.Limit)
	if input.SkillID == nil {
		for i := range result.Skills {
			versions, err := t.skills.ListVersions(ctx, &genskills.ListVersionsPayload{ID: result.Skills[i].ID, Cursor: nil, Limit: 50, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
			if err != nil {
				return fmt.Errorf("list skill versions: %w", err)
			}
			truncated = truncated || versions.NextCursor != nil
			createdAt := make(map[string]string, len(versions.Versions))
			for _, version := range versions.Versions {
				createdAt[version.ID] = version.CreatedAt
			}
			for j := range result.Skills[i].Versions {
				result.Skills[i].Versions[j].CreatedAt = createdAt[result.Skills[i].Versions[j].ID]
			}
			sortVersionInsights(result.Skills[i].Versions)
		}
	}
	result.From = from.Format(time.RFC3339)
	result.To = to.Format(time.RFC3339)
	result.SkillsTruncated = truncated
	return core.EncodeResult(wr, result)
}

func insightWindow(fromText, toText *string) (time.Time, time.Time, error) {
	to := time.Now().UTC()
	var err error
	if toText != nil {
		to, err = time.Parse(time.RFC3339, *toText)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("to must be RFC3339")
		}
	}
	from := to.Add(-insightDefaultWindow)
	if fromText != nil {
		from, err = time.Parse(time.RFC3339, *fromText)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("from must be RFC3339")
		}
	}
	if !from.Before(to) {
		return time.Time{}, time.Time{}, fmt.Errorf("from must be before to")
	}
	if to.Sub(from) > insightMaxWindow {
		return time.Time{}, time.Time{}, fmt.Errorf("insight window cannot exceed 90 days")
	}
	return from.UTC(), to.UTC(), nil
}

func buildInsightsResult(rows []telemetryrepo.SkillInsightBucket, skills map[string]*gentypes.Skill, versionCreatedAt map[string]string, sortBy string, limit int) insightsResult {
	bySkill := map[string]map[string]*insightAccumulator{}
	if len(skills) == 1 && len(versionCreatedAt) > 0 {
		for skillID := range skills {
			bySkill[skillID] = make(map[string]*insightAccumulator, len(versionCreatedAt))
			for versionID := range versionCreatedAt {
				bySkill[skillID][versionID] = &insightAccumulator{metrics: telemetryrepo.SkillInsightBucket{}, trend: nil}
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
			versions = map[string]*insightAccumulator{}
			bySkill[row.SkillID] = versions
		}
		acc := versions[row.SkillVersionID]
		if acc == nil {
			acc = &insightAccumulator{metrics: telemetryrepo.SkillInsightBucket{}, trend: nil}
			versions[row.SkillVersionID] = acc
		}
		addInsightBucket(&acc.metrics, row)
		acc.trend = append(acc.trend, insightPointFromBucket(row))
		scoresAvailable = scoresAvailable || row.ScoredSessions > 0
	}

	items := make([]skillInsight, 0, len(bySkill))
	for skillID, versions := range bySkill {
		skill := skills[skillID]
		item := skillInsight{ID: skill.ID, Name: skill.Name, DisplayName: skill.DisplayName, Metrics: insightMetrics{}, Versions: make([]versionInsight, 0, len(versions))}
		var skillTotal telemetryrepo.SkillInsightBucket
		for versionID, acc := range versions {
			addInsightBucket(&skillTotal, acc.metrics)
			item.Versions = append(item.Versions, versionInsight{ID: versionID, CreatedAt: versionCreatedAt[versionID], Metrics: metricsFromBucket(acc.metrics), Trend: acc.trend})
		}
		item.Metrics = metricsFromBucket(skillTotal)
		sortVersionInsights(item.Versions)
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		left, right := insightSortValue(items[i].Metrics, sortBy), insightSortValue(items[j].Metrics, sortBy)
		if left == right {
			return items[i].Name < items[j].Name
		}
		return left > right
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return insightsResult{
		From:            "",
		To:              "",
		ScoresAvailable: scoresAvailable,
		SkillsTruncated: false,
		CostAttribution: "Full session cost is attributed to every skill version activated in that session; totals across skills or versions are not additive.",
		ScoreCoverage:   "Efficacy and estimated savings summarize sampled scored sessions only; missing scores are not zero efficacy.",
		Skills:          items,
	}
}

func sortVersionInsights(versions []versionInsight) {
	sort.Slice(versions, func(i, j int) bool {
		if versions[i].CreatedAt == versions[j].CreatedAt {
			return versions[i].ID < versions[j].ID
		}
		return versions[i].CreatedAt > versions[j].CreatedAt
	})
}

func addInsightBucket(dst *telemetryrepo.SkillInsightBucket, src telemetryrepo.SkillInsightBucket) {
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

func metricsFromBucket(row telemetryrepo.SkillInsightBucket) insightMetrics {
	metrics := insightMetrics{
		Activations:           row.ActivationCount,
		ActivatedSessions:     row.ActivatedSessions,
		SessionCostUSD:        row.TotalSessionCost,
		AverageSessionCostUSD: ratioPtr(row.TotalSessionCost, row.ActivatedSessions),
		Efficacy:              nil,
	}
	if row.ScoredSessions == 0 {
		return metrics
	}
	efficacy := &efficacyMetrics{
		ScoredSessions:               row.ScoredSessions,
		AverageScore:                 row.ScoreSum / float64(row.ScoredSessions),
		EstimatedTurnsSavedTotal:     row.EstimatedTurnsSavedSum,
		EstimatedTurnsSavedAverage:   ratioPtr(row.EstimatedTurnsSavedSum, row.EstimatedTurnsSamples),
		EstimatedTurnsSavedSamples:   row.EstimatedTurnsSamples,
		EstimatedMinutesSavedTotal:   row.EstimatedMinutesSavedSum,
		EstimatedMinutesSavedAverage: ratioPtr(row.EstimatedMinutesSavedSum, row.EstimatedMinutesSamples),
		EstimatedMinutesSavedSamples: row.EstimatedMinutesSamples,
		ROIConfidenceCounts:          map[string]uint64{"low": row.ROIConfidenceLow, "med": row.ROIConfidenceMed, "high": row.ROIConfidenceHigh},
		FlagCounts:                   map[string]uint64{"ignored": row.IgnoredCount, "misapplied": row.MisappliedCount, "partially_followed": row.PartiallyFollowedCount, "harmful": row.HarmfulCount},
	}
	metrics.Efficacy = efficacy
	return metrics
}

func insightPointFromBucket(row telemetryrepo.SkillInsightBucket) insightPoint {
	return insightPoint{
		BucketStart:           time.Unix(0, row.BucketTimeUnixNano).UTC().Format(time.RFC3339),
		Activations:           row.ActivationCount,
		ActivatedSessions:     row.ActivatedSessions,
		SessionCostUSD:        row.TotalSessionCost,
		ScoredSessions:        row.ScoredSessions,
		AverageScore:          ratioPtr(row.ScoreSum, row.ScoredSessions),
		EstimatedMinutesSaved: row.EstimatedMinutesSavedSum,
	}
}

func ratioPtr(sum float64, count uint64) *float64 {
	if count == 0 {
		return nil
	}
	value := sum / float64(count)
	return &value
}

func insightSortValue(metrics insightMetrics, sortBy string) float64 {
	switch sortBy {
	case "efficacy":
		if metrics.Efficacy != nil {
			return metrics.Efficacy.AverageScore
		}
	case "activations":
		return float64(metrics.Activations)
	case "session_cost":
		return metrics.SessionCostUSD
	case "estimated_minutes_saved":
		if metrics.Efficacy != nil {
			return metrics.Efficacy.EstimatedMinutesSavedTotal
		}
	}
	return -1
}
