// Package chatanalysis implements the adminChatAnalysis service: the
// platform-admin surface over chat_analysis_settings, the per-(organization,
// judge) switches and daily budgets the chat analysis pipeline spends against
// (server/internal/chat/analysis).
package chatanalysis

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/speakeasy-api/gram/server/internal/businessmemory"
	"github.com/speakeasy-api/gram/server/internal/chat/analysis"
	"github.com/speakeasy-api/gram/server/internal/chat/analysis/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
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

// requirePlatformAdmin enforces the platform-admin flag and returns a logger
// tagged with the actor and the requested target organization. The target
// comes from the payload, never from the session's active organization.
func (s *Service) requirePlatformAdmin(ctx context.Context, organizationID string) (*contextvalues.AuthContext, *slog.Logger, error) {
	authCtx, logger, err := auth.RequirePlatformAdmin(ctx, s.logger)
	if err != nil {
		return nil, logger, err
	}
	if organizationID == "" {
		return nil, logger, oops.E(oops.CodeInvalid, nil, "organization_id is required")
	}

	logger = logger.With(attr.SlogOrganizationID(organizationID))
	if _, err := organizationsrepo.New(s.db).GetOrganizationMetadata(ctx, organizationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, logger, oops.E(oops.CodeNotFound, err, "organization not found")
		}
		return nil, logger, oops.E(oops.CodeUnexpected, err, "get organization metadata").LogError(ctx, logger)
	}

	return authCtx, logger, nil
}

func (s *Service) GetSettings(ctx context.Context, payload *gen.GetSettingsPayload) (*gen.ChatAnalysisSettings, error) {
	_, logger, err := s.requirePlatformAdmin(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}

	view, err := loadSettingsView(ctx, repo.New(s.db), payload.OrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get chat analysis settings").LogError(ctx, logger)
	}
	return view, nil
}

func (s *Service) UpsertWorkUnitsSettings(ctx context.Context, payload *gen.UpsertWorkUnitsSettingsPayload) (*gen.ChatAnalysisSettings, error) {
	return s.upsertJudgeSettings(ctx, payload.OrganizationID, analysis.WorkUnitsJudgeName, payload.WorkUnitsEnabled, payload.WorkUnitsDailyCap)
}

func (s *Service) UpsertBusinessMemorySettings(ctx context.Context, payload *gen.UpsertBusinessMemorySettingsPayload) (*gen.ChatAnalysisSettings, error) {
	return s.upsertJudgeSettings(ctx, payload.OrganizationID, businessmemory.JudgeName, payload.BusinessMemoryEnabled, payload.BusinessMemoryDailyCap)
}

func (s *Service) upsertJudgeSettings(ctx context.Context, organizationID string, judge string, enabled bool, dailyCap int) (*gen.ChatAnalysisSettings, error) {
	authCtx, logger, err := s.requirePlatformAdmin(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	if dailyCap < 0 || dailyCap > maxDailyCap {
		return nil, oops.E(oops.CodeInvalid, nil, "daily cap must be between 0 and %d", maxDailyCap)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin chat analysis settings upsert").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	queries := repo.New(dbtx)
	// The reservation transaction counts spend under this organization lock, so
	// holding it here keeps a budget change from landing mid-count.
	if err := queries.LockOrganizationChatAnalysisBudget(ctx, organizationID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock chat analysis settings").LogError(ctx, logger)
	}

	var beforeSnapshot *audit.ChatAnalysisSettingsSnapshot
	before, err := queries.GetChatAnalysisSettingForOrganizationJudge(ctx, repo.GetChatAnalysisSettingForOrganizationJudgeParams{
		OrganizationID: organizationID,
		Judge:          judge,
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
		OrganizationID: organizationID,
		Judge:          judge,
		Enabled:        enabled,
		DailyCap:       conv.SafeInt32(dailyCap),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "upsert chat analysis settings").LogError(ctx, logger)
	}
	afterSnapshot := buildSnapshot(row)

	if err := s.audit.LogChatAnalysisSettingsUpsert(ctx, dbtx, audit.LogChatAnalysisSettingsUpsertEvent{
		OrganizationID:                     organizationID,
		Actor:                              urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:                   authCtx.Email,
		ActorSlug:                          nil,
		ChatAnalysisSettingsURN:            urn.NewChatAnalysisSettings(organizationID),
		ChatAnalysisSettingsSnapshotBefore: beforeSnapshot,
		ChatAnalysisSettingsSnapshotAfter:  &afterSnapshot,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log chat analysis settings upsert").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit chat analysis settings upsert").LogError(ctx, logger)
	}

	view, err := loadSettingsView(ctx, repo.New(s.db), organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "reload chat analysis settings").LogError(ctx, logger)
	}
	return view, nil
}

// TriggerAnalysis wakes the chat analysis coordinator of every live project in
// the named organization. It moves no queue state itself: the coordinator's
// pass still enqueues, reserves against the daily budget, and applies the
// inactivity window — this only replaces waiting for a chat write or the
// periodic sweep to deliver the same signal.
func (s *Service) TriggerAnalysis(ctx context.Context, payload *gen.TriggerAnalysisPayload) (*gen.TriggerAnalysisResult, error) {
	_, logger, err := s.requirePlatformAdmin(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}

	projectIDs, err := repo.New(s.db).ListChatAnalysisProjectsForOrganization(ctx, payload.OrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list organization projects").LogError(ctx, logger)
	}

	// Continue through failures so one bad project doesn't starve the rest of
	// the organization of its signal.
	var signalErrs []error
	for _, projectID := range projectIDs {
		if err := s.signaler.Signal(ctx, projectID); err != nil {
			logger.ErrorContext(ctx, "failed to signal chat analysis coordinator", attr.SlogProjectID(projectID.String()), attr.SlogError(err))
			signalErrs = append(signalErrs, err)
		}
	}
	if len(signalErrs) > 0 {
		return nil, oops.E(
			oops.CodeUnexpected,
			errors.Join(signalErrs...),
			"failed to signal chat analysis coordinator for %d of %d projects", len(signalErrs), len(projectIDs),
		).LogError(ctx, logger)
	}

	return &gen.TriggerAnalysisResult{ProjectsSignaled: len(projectIDs)}, nil
}

// defaultView is what an organization with no stored row gets: everything off.
// The pipeline has no default enablement — a judge with no settings row spends
// nothing — so the defaults mirror that rather than suggesting a budget.
func defaultView(organizationID string) *gen.ChatAnalysisSettings {
	return &gen.ChatAnalysisSettings{
		OrganizationID:         organizationID,
		WorkUnitsEnabled:       false,
		WorkUnitsDailyCap:      0,
		BusinessMemoryEnabled:  false,
		BusinessMemoryDailyCap: 0,
		IsDefault:              true,
	}
}

func loadSettingsView(ctx context.Context, queries *repo.Queries, organizationID string) (*gen.ChatAnalysisSettings, error) {
	view := defaultView(organizationID)
	settings := []struct {
		judge string
		apply func(repo.ChatAnalysisSetting)
	}{
		{
			judge: analysis.WorkUnitsJudgeName,
			apply: func(row repo.ChatAnalysisSetting) {
				view.WorkUnitsEnabled = row.Enabled
				view.WorkUnitsDailyCap = int(row.DailyCap)
			},
		},
		{
			judge: businessmemory.JudgeName,
			apply: func(row repo.ChatAnalysisSetting) {
				view.BusinessMemoryEnabled = row.Enabled
				view.BusinessMemoryDailyCap = int(row.DailyCap)
			},
		},
	}
	for _, setting := range settings {
		row, err := queries.GetChatAnalysisSettingForOrganizationJudge(ctx, repo.GetChatAnalysisSettingForOrganizationJudgeParams{
			OrganizationID: organizationID,
			Judge:          setting.judge,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			continue
		case err != nil:
			return nil, fmt.Errorf("get %s chat analysis setting: %w", setting.judge, err)
		default:
			view.IsDefault = false
			setting.apply(row)
		}
	}
	return view, nil
}

func buildSnapshot(row repo.ChatAnalysisSetting) audit.ChatAnalysisSettingsSnapshot {
	return audit.ChatAnalysisSettingsSnapshot{
		Judge:    row.Judge,
		Enabled:  row.Enabled,
		DailyCap: row.DailyCap,
	}
}
