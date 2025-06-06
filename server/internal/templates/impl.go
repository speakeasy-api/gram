package templates

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/gen/http/templates/server"
	gen "github.com/speakeasy-api/gram/gen/templates"
	"github.com/speakeasy-api/gram/internal/auth"
	"github.com/speakeasy-api/gram/internal/auth/sessions"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	"github.com/speakeasy-api/gram/internal/conv"
	"github.com/speakeasy-api/gram/internal/middleware"
	"github.com/speakeasy-api/gram/internal/mv"
	"github.com/speakeasy-api/gram/internal/o11y"
	"github.com/speakeasy-api/gram/internal/oops"
	"github.com/speakeasy-api/gram/internal/templates/repo"
)

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
	auth   *auth.Auth
	repo   *repo.Queries
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager) *Service {
	return &Service{
		tracer: otel.Tracer("github.com/speakeasy-api/gram/internal/templates"),
		logger: logger,
		db:     db,
		auth:   auth.New(logger, db, sessions),
		repo:   repo.New(db),
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) CreateTemplate(ctx context.Context, payload *gen.CreateTemplatePayload) (*gen.CreatePromptTemplateResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	projectID := *authCtx.ProjectID

	logger := s.logger.With(slog.String("project_id", projectID.String()))

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to begin transaction").Log(ctx, s.logger)
	}

	defer o11y.LogDefer(ctx, logger, func() error { return dbtx.Rollback(ctx) })

	tr := s.repo.WithTx(dbtx)

	var predecessorID uuid.UUID
	var historyID uuid.UUID

	if payload.PredecessorID != nil && *payload.PredecessorID != "" {
		parsed, err := uuid.Parse(*payload.PredecessorID)
		if err != nil {
			return nil, oops.E(oops.CodeInvalid, err, "invalid predecessor id")
		}

		if parsed == uuid.Nil {
			return nil, oops.E(oops.CodeInvalid, nil, "invalid predecessor id")
		}

		predRow, err := tr.PeekTemplateByID(ctx, repo.PeekTemplateByIDParams{
			ProjectID: projectID,
			ID:        parsed,
		})
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return nil, oops.E(oops.CodeInvalid, nil, "previous revision not found")
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "error reading previous revision").Log(ctx, s.logger)
		}

		predecessorID = predRow.ID
		historyID = predRow.HistoryID
	}

	if historyID == uuid.Nil {
		historyID = uuid.New()
	}

	var args []byte
	if payload.Arguments != nil {
		args = []byte(*payload.Arguments)
	}

	id, err := tr.CreateTemplate(ctx, repo.CreateTemplateParams{
		ProjectID: projectID,
		HistoryID: historyID,
		PredecessorID: uuid.NullUUID{
			UUID:  predecessorID,
			Valid: predecessorID != uuid.Nil,
		},
		Name:        string(payload.Name),
		Prompt:      payload.Prompt,
		Description: conv.PtrToPGTextEmpty(payload.Description),
		Arguments:   args,
		Engine:      conv.ToPGTextEmpty(payload.Engine),
		Kind:        conv.ToPGTextEmpty(payload.Kind),
		ToolsHint:   payload.ToolsHint,
	})
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr):
		if pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "template name already exists")
		}
		return nil, err
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "failed to create template").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to save template").Log(ctx, s.logger)
	}

	pt, err := mv.DescribePromptTemplate(ctx, s.logger, s.db, mv.ProjectID(projectID), mv.PromptTemplateID(uuid.NullUUID{UUID: id, Valid: true}), mv.PromptTemplateName(nil))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to read template").Log(ctx, s.logger)
	}

	return &gen.CreatePromptTemplateResult{Template: pt}, nil
}

func (s *Service) DeleteTemplate(ctx context.Context, payload *gen.DeleteTemplatePayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	projectID := *authCtx.ProjectID

	if payload.ID != nil && *payload.ID != "" {
		id, err := uuid.Parse(*payload.ID)
		if err != nil {
			return oops.E(oops.CodeBadRequest, err, "invalid template id")
		}

		if id == uuid.Nil {
			return oops.E(oops.CodeBadRequest, nil, "invalid template id")
		}

		if err := s.repo.DeleteTemplateByID(ctx, repo.DeleteTemplateByIDParams{
			ProjectID: projectID,
			ID:        id,
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to delete template").Log(ctx, s.logger)
		}
	} else if payload.Name != nil && *payload.Name != "" {
		name := *payload.Name

		if err := s.repo.DeleteTemplateByName(ctx, repo.DeleteTemplateByNameParams{
			ProjectID: projectID,
			Name:      name,
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to delete template").Log(ctx, s.logger)
		}
	} else {
		return oops.E(oops.CodeBadRequest, nil, "either id or name must be provided")
	}

	return nil
}

func (s *Service) GetTemplate(ctx context.Context, payload *gen.GetTemplatePayload) (*gen.GetPromptTemplateResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	projectID := *authCtx.ProjectID
	var id uuid.NullUUID
	if payload.ID != nil && *payload.ID != "" {
		parsed, err := uuid.Parse(*payload.ID)
		if err != nil {
			return nil, oops.E(oops.CodeInvalid, err, "invalid template id")
		}

		id = uuid.NullUUID{
			UUID:  parsed,
			Valid: parsed != uuid.Nil,
		}
	}

	if conv.PtrValOrEmpty(payload.Name, "") == "" && !id.Valid {
		return nil, oops.E(oops.CodeBadRequest, nil, "either id or name must be provided")
	}

	pt, err := mv.DescribePromptTemplate(ctx, s.logger, s.db, mv.ProjectID(projectID), mv.PromptTemplateID(id), mv.PromptTemplateName(payload.Name))
	if err != nil {
		return nil, err
	}

	return &gen.GetPromptTemplateResult{Template: pt}, nil
}

func (s *Service) ListTemplates(ctx context.Context, payload *gen.ListTemplatesPayload) (res *gen.ListPromptTemplatesResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	pt, err := mv.DescribePromptTemplates(ctx, s.logger, s.db, mv.ProjectID(*authCtx.ProjectID))
	if err != nil {
		return nil, err
	}

	return &gen.ListPromptTemplatesResult{Templates: pt}, nil
}
