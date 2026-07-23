// Package chatanalysis implements the adminChatAnalysis service: the
// platform-admin surface over chat_analysis_settings, the per-(organization,
// judge) switches and daily budgets the chat analysis pipeline spends against
// (server/internal/chat/analysis). Only the work_units judge is registered
// today, so the API models exactly that judge's row.
package chatanalysis

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/admin_chat_analysis"
	srv "github.com/speakeasy-api/gram/server/gen/http/admin_chat_analysis/server"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/chat/analysis"
	"github.com/speakeasy-api/gram/server/internal/chat/analysis/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const maxDailyCap = 10000

type Service struct {
	tracer   trace.Tracer
	logger   *slog.Logger
	db       *pgxpool.Pool
	auth     *auth.Auth
	audit    *audit.Logger
	signaler analysis.Signaler
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	auditLogger *audit.Logger,
	signaler analysis.Signaler,
) *Service {
	logger = logger.With(attr.SlogComponent("chatanalysis.api"))
	return &Service{
		tracer:   tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/chatanalysis"),
		logger:   logger,
		db:       db,
		auth:     auth.New(logger, db, sessions, authzEngine),
		audit:    auditLogger,
		signaler: signaler,
	}
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

// requirePlatformAdmin extracts the auth context and enforces the
// platform-admin flag. The settings row is keyed by the caller's active
// organization, so a session with no organization has nothing to read or
// write.
func (s *Service) requirePlatformAdmin(ctx context.Context) (*contextvalues.AuthContext, *slog.Logger, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, s.logger, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(
		attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		attr.SlogUserID(authCtx.UserID),
	)

	if !authCtx.IsAdmin {
		return nil, logger, oops.E(oops.CodeForbidden, nil, "platform admin required").LogError(ctx, logger)
	}
	if authCtx.ActiveOrganizationID == "" {
		return nil, logger, oops.E(oops.CodeForbidden, nil, "no active organization")
	}

	return authCtx, logger, nil
}

func (s *Service) GetSettings(ctx context.Context, _ *gen.GetSettingsPayload) (*gen.ChatAnalysisSettings, error) {
	authCtx, logger, err := s.requirePlatformAdmin(ctx)
	if err != nil {
		return nil, err
	}

	row, err := repo.New(s.db).GetChatAnalysisSettingForOrganizationJudge(ctx, repo.GetChatAnalysisSettingForOrganizationJudgeParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Judge:          analysis.WorkUnitsJudgeName,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return defaultView(authCtx.ActiveOrganizationID), nil
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "get chat analysis settings").LogError(ctx, logger)
	default:
		return buildView(row, false), nil
	}
}

func (s *Service) UpsertWorkUnitsSettings(ctx context.Context, payload *gen.UpsertWorkUnitsSettingsPayload) (*gen.ChatAnalysisSettings, error) {
	authCtx, logger, err := s.requirePlatformAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if payload.WorkUnitsDailyCap < 0 || payload.WorkUnitsDailyCap > maxDailyCap {
		return nil, oops.E(oops.CodeInvalid, nil, "work_units_daily_cap must be between 0 and %d", maxDailyCap)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin chat analysis settings upsert").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	queries := repo.New(dbtx)
	// The reservation transaction counts spend under this organization lock, so
	// holding it here keeps a budget change from landing mid-count.
	if err := queries.LockOrganizationChatAnalysisBudget(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock chat analysis settings").LogError(ctx, logger)
	}

	var beforeSnapshot *audit.ChatAnalysisSettingsSnapshot
	before, err := queries.GetChatAnalysisSettingForOrganizationJudge(ctx, repo.GetChatAnalysisSettingForOrganizationJudgeParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Judge:          analysis.WorkUnitsJudgeName,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "get existing chat analysis settings").LogError(ctx, logger)
	default:
		snapshot := buildSnapshot(before)
		beforeSnapshot = &snapshot
	}

	row, err := queries.UpsertChatAnalysisSettingForOrganizationJudge(ctx, repo.UpsertChatAnalysisSettingForOrganizationJudgeParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Judge:          analysis.WorkUnitsJudgeName,
		Enabled:        payload.WorkUnitsEnabled,
		DailyCap:       conv.SafeInt32(payload.WorkUnitsDailyCap),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "upsert chat analysis settings").LogError(ctx, logger)
	}
	afterSnapshot := buildSnapshot(row)

	if err := s.audit.LogChatAnalysisSettingsUpsert(ctx, dbtx, audit.LogChatAnalysisSettingsUpsertEvent{
		OrganizationID:                     authCtx.ActiveOrganizationID,
		Actor:                              urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:                   authCtx.Email,
		ActorSlug:                          nil,
		ChatAnalysisSettingsURN:            urn.NewChatAnalysisSettings(authCtx.ActiveOrganizationID),
		ChatAnalysisSettingsSnapshotBefore: beforeSnapshot,
		ChatAnalysisSettingsSnapshotAfter:  &afterSnapshot,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log chat analysis settings upsert").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit chat analysis settings upsert").LogError(ctx, logger)
	}

	return buildView(row, false), nil
}

// TriggerAnalysis wakes the chat analysis coordinator of every live project in
// the caller's organization. It moves no queue state itself: the coordinator's
// pass still enqueues, reserves against the daily budget, and applies the
// inactivity window — this only replaces waiting for a chat write or the
// periodic sweep to deliver the same signal.
func (s *Service) TriggerAnalysis(ctx context.Context, _ *gen.TriggerAnalysisPayload) (*gen.TriggerAnalysisResult, error) {
	authCtx, logger, err := s.requirePlatformAdmin(ctx)
	if err != nil {
		return nil, err
	}

	projectIDs, err := repo.New(s.db).ListChatAnalysisProjectsForOrganization(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list organization projects").LogError(ctx, logger)
	}

	for _, projectID := range projectIDs {
		if err := s.signaler.Signal(ctx, projectID); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "signal chat analysis coordinator").LogError(ctx, logger)
		}
	}

	return &gen.TriggerAnalysisResult{ProjectsSignaled: len(projectIDs)}, nil
}

// defaultView is what an organization with no stored row gets: everything off.
// The pipeline has no default enablement — a judge with no settings row spends
// nothing — so the defaults mirror that rather than suggesting a budget.
func defaultView(organizationID string) *gen.ChatAnalysisSettings {
	return &gen.ChatAnalysisSettings{
		OrganizationID:    organizationID,
		WorkUnitsEnabled:  false,
		WorkUnitsDailyCap: 0,
		IsDefault:         true,
	}
}

func buildView(row repo.ChatAnalysisSetting, isDefault bool) *gen.ChatAnalysisSettings {
	return &gen.ChatAnalysisSettings{
		OrganizationID:    row.OrganizationID,
		WorkUnitsEnabled:  row.Enabled,
		WorkUnitsDailyCap: int(row.DailyCap),
		IsDefault:         isDefault,
	}
}

func buildSnapshot(row repo.ChatAnalysisSetting) audit.ChatAnalysisSettingsSnapshot {
	return audit.ChatAnalysisSettingsSnapshot{
		Judge:    row.Judge,
		Enabled:  row.Enabled,
		DailyCap: row.DailyCap,
	}
}
