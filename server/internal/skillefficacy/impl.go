package skillefficacy

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/skill_efficacy/server"
	gen "github.com/speakeasy-api/gram/server/gen/skill_efficacy"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/chat/analysis"
	analysisrepo "github.com/speakeasy-api/gram/server/internal/chat/analysis/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/skills/efficacy"
	skillsrepo "github.com/speakeasy-api/gram/server/internal/skills/repo"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const maxSamplingCap = 10000

type Service struct {
	tracer   trace.Tracer
	logger   *slog.Logger
	db       *pgxpool.Pool
	auth     *auth.Auth
	authz    *authz.Engine
	features *productfeatures.Client
	audit    *audit.Logger
	insights InsightsReader
}

type InsightsReader interface {
	QuerySkillInsights(context.Context, telemetryrepo.QuerySkillInsightsParams) ([]telemetryrepo.SkillInsightBucket, error)
	ListSkillEfficacyScoreSessions(context.Context, telemetryrepo.ListSkillEfficacyScoreSessionsParams) ([]telemetryrepo.SkillEfficacyScoreSession, error)
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	features *productfeatures.Client,
	auditLogger *audit.Logger,
	insights InsightsReader,
) *Service {
	logger = logger.With(attr.SlogComponent("skillefficacy.api"))
	return &Service{
		tracer:   tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/skillefficacy"),
		logger:   logger,
		db:       db,
		auth:     auth.New(logger, db, sessions, authzEngine),
		authz:    authzEngine,
		features: features,
		audit:    auditLogger,
		insights: insights,
	}
}

func (s *Service) QueryInsights(ctx context.Context, payload *gen.QueryInsightsPayload) (*gen.SkillEfficacyInsightsResult, error) {
	authCtx, logger, err := s.requireProjectAccess(ctx, authz.ScopeSkillRead)
	if err != nil {
		return nil, err
	}
	if len(payload.SkillIds) > 200 {
		return nil, oops.E(oops.CodeInvalid, nil, "skill_ids cannot contain more than 200 IDs")
	}
	seen := map[string]struct{}{}
	skillIDs := make([]string, 0, len(payload.SkillIds))
	for _, id := range payload.SkillIds {
		parsed, err := uuid.Parse(id)
		if err != nil {
			return nil, oops.E(oops.CodeInvalid, err, "skill_ids must contain UUIDs")
		}
		id = parsed.String()
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			skillIDs = append(skillIDs, id)
		}
	}
	includeScoredSessions := payload.IncludeScoredSessions != nil && *payload.IncludeScoredSessions
	if includeScoredSessions {
		if len(skillIDs) == 0 {
			return nil, oops.E(oops.CodeInvalid, nil, "skill_ids are required when including scored sessions")
		}
		if err := s.authz.Require(ctx, authz.Check{
			Scope:        authz.ScopeChatRead,
			ResourceKind: "",
			ResourceID:   authCtx.ProjectID.String(),
			Dimensions:   nil,
		}); err != nil {
			return nil, err
		}
	}
	from, to, err := resolveInsightsWindow(payload.From, payload.To)
	if err != nil {
		return nil, err
	}
	responseSkillIDs := skillIDs
	if len(responseSkillIDs) == 0 {
		activeSkills, err := skillsrepo.New(s.db).ListSkills(ctx, skillsrepo.ListSkillsParams{
			ProjectID: *authCtx.ProjectID, CursorName: pgtype.Text{String: "", Valid: false}, PageLimit: math.MaxInt32,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "list active project skills").LogError(ctx, logger)
		}
		responseSkillIDs = make([]string, 0, len(activeSkills))
		for _, skill := range activeSkills {
			responseSkillIDs = append(responseSkillIDs, skill.Skill.ID.String())
		}
	}
	var rows []telemetryrepo.SkillInsightBucket
	if len(responseSkillIDs) > 0 {
		rows, err = s.insights.QuerySkillInsights(ctx, telemetryrepo.QuerySkillInsightsParams{
			OrganizationID:  authCtx.ActiveOrganizationID,
			ProjectID:       authCtx.ProjectID.String(),
			SkillIDs:        skillIDs,
			SkillVersionIDs: nil,
			From:            from,
			To:              to,
			IntervalSeconds: int64((24 * time.Hour).Seconds()),
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "query skill efficacy insights").LogError(ctx, logger)
		}
	}
	result := buildInsightsView(responseSkillIDs, rows, payload.IncludeVersions != nil && *payload.IncludeVersions)
	result.From = from.Format(time.RFC3339)
	result.To = to.Format(time.RFC3339)
	if includeScoredSessions {
		scores, err := s.insights.ListSkillEfficacyScoreSessions(ctx, telemetryrepo.ListSkillEfficacyScoreSessionsParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			ProjectID:      authCtx.ProjectID.String(),
			SkillIDs:       skillIDs,
			From:           from,
			To:             to,
			Limit:          100,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "list scored skill sessions").LogError(ctx, logger)
		}
		result.ScoredSessions = buildScoredSessionViews(scores)
	}
	return result, nil
}

func (s *Service) requireProjectAccess(ctx context.Context, scope authz.Scope) (*contextvalues.AuthContext, *slog.Logger, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, s.logger, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: scope, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, s.logger, err
	}
	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID), attr.SlogProjectID(authCtx.ProjectID.String()))
	enabled, err := s.features.IsFeatureEnabled(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureSkills)
	if err != nil {
		return nil, logger, oops.E(oops.CodeUnexpected, err, "check skills feature").LogError(ctx, logger)
	}
	if !enabled {
		return nil, logger, oops.E(oops.CodeForbidden, nil, "skills are not enabled for this organization")
	}
	return authCtx, logger, nil
}

func resolveInsightsWindow(fromText, toText *string) (time.Time, time.Time, error) {
	to := time.Now().UTC()
	var err error
	if toText != nil {
		to, err = time.Parse(time.RFC3339, *toText)
		if err != nil {
			return time.Time{}, time.Time{}, oops.E(oops.CodeInvalid, err, "to must be RFC3339")
		}
	}
	from := to.Add(-30 * 24 * time.Hour)
	if fromText != nil {
		from, err = time.Parse(time.RFC3339, *fromText)
		if err != nil {
			return time.Time{}, time.Time{}, oops.E(oops.CodeInvalid, err, "from must be RFC3339")
		}
	}
	if !from.Before(to) || to.Sub(from) > 90*24*time.Hour {
		return time.Time{}, time.Time{}, oops.E(oops.CodeInvalid, nil, "insight window must be positive and at most 90 days")
	}
	return from.UTC(), to.UTC(), nil
}

type insightTotals struct {
	row    telemetryrepo.SkillInsightBucket
	points []*gen.SkillInsightPoint
}

func buildInsightsView(skillIDs []string, rows []telemetryrepo.SkillInsightBucket, includeVersions bool) *gen.SkillEfficacyInsightsResult {
	bySkill := make(map[string]map[string]*insightTotals, len(skillIDs))
	for _, id := range skillIDs {
		bySkill[id] = map[string]*insightTotals{}
	}
	scoresAvailable := false
	for _, row := range rows {
		versions, ok := bySkill[row.SkillID]
		if !ok {
			continue
		}
		total := versions[row.SkillVersionID]
		if total == nil {
			total = new(insightTotals)
			versions[row.SkillVersionID] = total
		}
		addInsightRow(&total.row, row)
		if includeVersions {
			total.points = append(total.points, buildInsightPoint(row))
		}
		scoresAvailable = scoresAvailable || row.ScoredSessions > 0
	}
	result := &gen.SkillEfficacyInsightsResult{
		From:            "",
		To:              "",
		IntervalSeconds: int64((24 * time.Hour).Seconds()),
		ScoresAvailable: scoresAvailable,
		Insights:        make([]*gen.SkillEfficacyInsight, 0, len(skillIDs)),
		ScoredSessions:  []*gen.SkillEfficacyScoredSession{},
	}
	for _, skillID := range skillIDs {
		var aggregate telemetryrepo.SkillInsightBucket
		versionIDs := make([]string, 0, len(bySkill[skillID]))
		for versionID := range bySkill[skillID] {
			versionIDs = append(versionIDs, versionID)
		}
		sort.Strings(versionIDs)
		versions := make([]*gen.SkillVersionInsight, 0, len(versionIDs))
		for _, versionID := range versionIDs {
			total := bySkill[skillID][versionID]
			addInsightRow(&aggregate, total.row)
			if includeVersions {
				versions = append(versions, &gen.SkillVersionInsight{SkillVersionID: versionID, Metrics: buildMetricsView(total.row), Trend: total.points})
			}
		}
		result.Insights = append(result.Insights, &gen.SkillEfficacyInsight{SkillID: skillID, Metrics: buildMetricsView(aggregate), Versions: versions})
	}
	return result
}

func addInsightRow(dst *telemetryrepo.SkillInsightBucket, src telemetryrepo.SkillInsightBucket) {
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

func buildMetricsView(row telemetryrepo.SkillInsightBucket) *gen.SkillInsightMetrics {
	view := &gen.SkillInsightMetrics{
		Activations:           row.ActivationCount,
		ActivatedSessions:     row.ActivatedSessions,
		SessionCostUsd:        row.TotalSessionCost,
		AverageSessionCostUsd: ratio(row.TotalSessionCost, row.ActivatedSessions),
		Efficacy:              nil,
	}
	if row.ScoredSessions > 0 {
		view.Efficacy = &gen.SkillEfficacyMetrics{
			ScoredSessions:               row.ScoredSessions,
			AverageScore:                 row.ScoreSum / float64(row.ScoredSessions),
			EstimatedTurnsSavedTotal:     row.EstimatedTurnsSavedSum,
			EstimatedTurnsSavedAverage:   ratio(row.EstimatedTurnsSavedSum, row.EstimatedTurnsSamples),
			EstimatedTurnsSavedSamples:   row.EstimatedTurnsSamples,
			EstimatedMinutesSavedTotal:   row.EstimatedMinutesSavedSum,
			EstimatedMinutesSavedAverage: ratio(row.EstimatedMinutesSavedSum, row.EstimatedMinutesSamples),
			EstimatedMinutesSavedSamples: row.EstimatedMinutesSamples,
			RoiConfidenceCounts:          map[string]uint64{"low": row.ROIConfidenceLow, "med": row.ROIConfidenceMed, "high": row.ROIConfidenceHigh},
			FlagCounts:                   map[string]uint64{"ignored": row.IgnoredCount, "misapplied": row.MisappliedCount, "partially_followed": row.PartiallyFollowedCount, "harmful": row.HarmfulCount},
		}
	}
	return view
}
func ratio(sum float64, count uint64) *float64 {
	if count == 0 {
		return nil
	}
	value := sum / float64(count)
	return &value
}

func buildInsightPoint(row telemetryrepo.SkillInsightBucket) *gen.SkillInsightPoint {
	return &gen.SkillInsightPoint{
		BucketStart:           time.Unix(0, row.BucketTimeUnixNano).UTC().Format(time.RFC3339),
		Activations:           row.ActivationCount,
		ActivatedSessions:     row.ActivatedSessions,
		SessionCostUsd:        row.TotalSessionCost,
		ScoredSessions:        row.ScoredSessions,
		AverageScore:          ratio(row.ScoreSum, row.ScoredSessions),
		EstimatedMinutesSaved: row.EstimatedMinutesSavedSum,
	}
}

func buildScoredSessionViews(rows []telemetryrepo.SkillEfficacyScoreSession) []*gen.SkillEfficacyScoredSession {
	result := make([]*gen.SkillEfficacyScoredSession, 0, len(rows))
	for _, row := range rows {
		var chatID *string
		if id, err := uuid.Parse(row.GramChatID); err == nil {
			value := id.String()
			chatID = &value
		}
		result = append(result, &gen.SkillEfficacyScoredSession{
			ID:                    row.ID,
			SkillID:               row.SkillID,
			SkillVersionID:        row.SkillVersionID,
			Surface:               row.Surface,
			ActivatedAt:           row.ActivatedAt.UTC().Format(time.RFC3339),
			ScoredAt:              row.ScoredAt.UTC().Format(time.RFC3339),
			Score:                 row.Score,
			Rationale:             row.Rationale,
			EstimatedTurnsSaved:   row.EstimatedTurnsSaved,
			EstimatedMinutesSaved: row.EstimatedMinutesSaved,
			RoiConfidence:         row.ROIConfidence,
			Flags:                 row.Flags,
			GramChatID:            chatID,
		})
	}
	return result
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(mux, srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) requireAccess(ctx context.Context, scope authz.Scope) (*contextvalues.AuthContext, *slog.Logger, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, s.logger, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{
		Scope:        scope,
		ResourceKind: "",
		ResourceID:   authCtx.ActiveOrganizationID,
		Dimensions:   nil,
	}); err != nil {
		return nil, s.logger, err
	}

	logger := s.logger.With(
		attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		attr.SlogUserID(authCtx.UserID),
	)
	enabled, err := s.features.IsFeatureEnabled(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureSkills)
	if err != nil {
		return nil, logger, oops.E(oops.CodeUnexpected, err, "check skills feature").LogError(ctx, logger)
	}
	if !enabled {
		return nil, logger, oops.E(oops.CodeForbidden, nil, "skills are not enabled for this organization")
	}

	return authCtx, logger, nil
}

func (s *Service) GetSettings(ctx context.Context, _ *gen.GetSettingsPayload) (*gen.SkillEfficacySettings, error) {
	authCtx, logger, err := s.requireAccess(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}

	row, err := analysisrepo.New(s.db).GetChatAnalysisSettingForOrganizationJudge(ctx, analysisrepo.GetChatAnalysisSettingForOrganizationJudgeParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Judge:          efficacy.JudgeName,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return defaultView(authCtx.ActiveOrganizationID), nil
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "get skill efficacy settings").LogError(ctx, logger)
	default:
		return buildView(row, false), nil
	}
}

func (s *Service) UpsertSettings(ctx context.Context, payload *gen.UpsertSettingsPayload) (*gen.SkillEfficacySettings, error) {
	authCtx, logger, err := s.requireAccess(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}
	if err := validateCap("daily_cap", payload.DailyCap); err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin skill efficacy settings upsert").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	queries := analysisrepo.New(dbtx)
	if err := queries.LockOrganizationChatAnalysisBudget(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock skill efficacy settings").LogError(ctx, logger)
	}
	var beforeSnapshot *audit.SkillEfficacySettingsSnapshot
	before, err := queries.GetChatAnalysisSettingForOrganizationJudge(ctx, analysisrepo.GetChatAnalysisSettingForOrganizationJudgeParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Judge:          efficacy.JudgeName,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "get existing skill efficacy settings").LogError(ctx, logger)
	default:
		snapshot := buildSnapshot(before)
		beforeSnapshot = &snapshot
	}

	row, err := queries.UpsertChatAnalysisSettingForOrganizationJudge(ctx, analysisrepo.UpsertChatAnalysisSettingForOrganizationJudgeParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Judge:          efficacy.JudgeName,
		Enabled:        payload.Enabled,
		DailyCap:       conv.SafeInt32(payload.DailyCap),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "upsert skill efficacy settings").LogError(ctx, logger)
	}
	afterSnapshot := buildSnapshot(row)

	if err := s.audit.LogSkillEfficacySettingsUpsert(ctx, dbtx, audit.LogSkillEfficacySettingsUpsertEvent{
		OrganizationID:                      authCtx.ActiveOrganizationID,
		Actor:                               urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:                    authCtx.Email,
		ActorSlug:                           nil,
		SkillEfficacySettingsURN:            urn.NewSkillEfficacySettings(authCtx.ActiveOrganizationID),
		SkillEfficacySettingsSnapshotBefore: beforeSnapshot,
		SkillEfficacySettingsSnapshotAfter:  &afterSnapshot,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log skill efficacy settings upsert").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit skill efficacy settings upsert").LogError(ctx, logger)
	}

	return buildView(row, false), nil
}

func validateCap(name string, value int) error {
	if value < 0 || value > maxSamplingCap {
		return oops.E(oops.CodeInvalid, nil, "%s must be between 0 and %d", name, maxSamplingCap)
	}
	return nil
}

// defaultView is what an organization with no settings row runs on: scoring
// off, the package default cap shown for when it is switched on. The pipeline
// is opt-in per judge, so absence means disabled.
func defaultView(organizationID string) *gen.SkillEfficacySettings {
	return &gen.SkillEfficacySettings{
		OrganizationID: organizationID,
		Enabled:        false,
		DailyCap:       int(analysis.DefaultJudgeDailyCap),
		IsDefault:      true,
	}
}

func buildView(row analysisrepo.ChatAnalysisSetting, isDefault bool) *gen.SkillEfficacySettings {
	return &gen.SkillEfficacySettings{
		OrganizationID: row.OrganizationID,
		Enabled:        row.Enabled,
		DailyCap:       int(row.DailyCap),
		IsDefault:      isDefault,
	}
}

func buildSnapshot(row analysisrepo.ChatAnalysisSetting) audit.SkillEfficacySettingsSnapshot {
	return audit.SkillEfficacySettingsSnapshot{
		Enabled:  row.Enabled,
		DailyCap: row.DailyCap,
	}
}
