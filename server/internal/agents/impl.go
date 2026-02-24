package agents

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/agents"
	srv "github.com/speakeasy-api/gram/server/gen/http/agents/server"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
	repo   *repo.Queries
	auth   *auth.Auth
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager) *Service {
	logger = logger.With(attr.SlogComponent("agents"))
	return &Service{
		tracer: otel.Tracer("github.com/speakeasy-api/gram/server/internal/agents"),
		logger: logger,
		db:     db,
		repo:   repo.New(db),
		auth:   auth.New(logger, db, sessions),
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

func (s *Service) CreateAgentDefinition(ctx context.Context, payload *gen.CreateAgentDefinitionPayload) (*types.AgentDefinition, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	slug := conv.ToSlug(string(payload.Name))
	toolURN := urn.NewTool(urn.ToolKindAgent, "agent", slug)

	tools := make([]urn.Tool, 0, len(payload.Tools))
	for _, t := range payload.Tools {
		parsed, err := urn.ParseTool(t)
		if err != nil {
			return nil, oops.E(oops.CodeInvalid, err, "invalid tool URN: %s", t).Log(ctx, s.logger)
		}
		tools = append(tools, parsed)
	}

	row, err := s.repo.CreateAgentDefinition(ctx, repo.CreateAgentDefinitionParams{
		ProjectID:       *authCtx.ProjectID,
		ToolUrn:         toolURN,
		Name:            slug,
		Description:     payload.Description,
		Title:           conv.PtrToPGText(payload.Title),
		Instructions:    payload.Instructions,
		Tools:           tools,
		Model:           conv.PtrToPGText(payload.Model),
		ReadOnlyHint:    conv.PtrToPGBool(payload.ReadOnlyHint),
		DestructiveHint: conv.PtrToPGBool(payload.DestructiveHint),
		IdempotentHint:  conv.PtrToPGBool(payload.IdempotentHint),
		OpenWorldHint:   conv.PtrToPGBool(payload.OpenWorldHint),
	})

	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr):
		if pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "agent definition name already exists")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "create agent definition").Log(ctx, s.logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "create agent definition").Log(ctx, s.logger)
	}

	return agentDefinitionToResult(row), nil
}

func (s *Service) GetAgentDefinition(ctx context.Context, payload *gen.GetAgentDefinitionPayload) (*types.AgentDefinition, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid agent definition id").Log(ctx, s.logger)
	}

	row, err := s.repo.GetAgentDefinitionByID(ctx, repo.GetAgentDefinitionByIDParams{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "agent definition not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get agent definition").Log(ctx, s.logger)
	}

	return agentDefinitionToResult(row), nil
}

func (s *Service) ListAgentDefinitions(ctx context.Context, payload *gen.ListAgentDefinitionsPayload) (*gen.ListAgentDefinitionsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	rows, err := s.repo.ListAgentDefinitions(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list agent definitions").Log(ctx, s.logger)
	}

	result := make([]*types.AgentDefinition, len(rows))
	for i, row := range rows {
		result[i] = agentDefinitionToResult(row)
	}

	return &gen.ListAgentDefinitionsResult{AgentDefinitions: result}, nil
}

func (s *Service) UpdateAgentDefinition(ctx context.Context, payload *gen.UpdateAgentDefinitionPayload) (*types.AgentDefinition, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid agent definition id").Log(ctx, s.logger)
	}

	var tools []urn.Tool
	if payload.Tools != nil {
		tools = make([]urn.Tool, len(payload.Tools))
		for i, t := range payload.Tools {
			parsed, err := urn.ParseTool(t)
			if err != nil {
				return nil, oops.E(oops.CodeInvalid, err, "invalid tool URN: %s", t).Log(ctx, s.logger)
			}
			tools[i] = parsed
		}
	}

	row, err := s.repo.UpdateAgentDefinition(ctx, repo.UpdateAgentDefinitionParams{
		ID:              id,
		ProjectID:       *authCtx.ProjectID,
		Description:     conv.PtrToPGText(payload.Description),
		Title:           conv.PtrToPGText(payload.Title),
		Instructions:    conv.PtrToPGText(payload.Instructions),
		Tools:           tools,
		Model:           conv.PtrToPGText(payload.Model),
		ReadOnlyHint:    conv.PtrToPGBool(payload.ReadOnlyHint),
		DestructiveHint: conv.PtrToPGBool(payload.DestructiveHint),
		IdempotentHint:  conv.PtrToPGBool(payload.IdempotentHint),
		OpenWorldHint:   conv.PtrToPGBool(payload.OpenWorldHint),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "agent definition not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update agent definition").Log(ctx, s.logger)
	}

	return agentDefinitionToResult(row), nil
}

func (s *Service) DeleteAgentDefinition(ctx context.Context, payload *gen.DeleteAgentDefinitionPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid agent definition id").Log(ctx, s.logger)
	}

	err = s.repo.DeleteAgentDefinition(ctx, repo.DeleteAgentDefinitionParams{
		ID:        id,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return oops.E(oops.CodeUnexpected, err, "delete agent definition").Log(ctx, s.logger)
	}

	return nil
}

func agentDefinitionToResult(row repo.AgentDefinition) *types.AgentDefinition {
	toolStrings := make([]string, len(row.Tools))
	for i, t := range row.Tools {
		toolStrings[i] = t.String()
	}

	var annotations *types.ToolAnnotations
	if row.ReadOnlyHint.Valid || row.DestructiveHint.Valid || row.IdempotentHint.Valid || row.OpenWorldHint.Valid {
		annotations = &types.ToolAnnotations{
			Title:           conv.FromPGText[string](row.Title),
			ReadOnlyHint:    conv.FromPGBool[bool](row.ReadOnlyHint),
			DestructiveHint: conv.FromPGBool[bool](row.DestructiveHint),
			IdempotentHint:  conv.FromPGBool[bool](row.IdempotentHint),
			OpenWorldHint:   conv.FromPGBool[bool](row.OpenWorldHint),
		}
	}

	return &types.AgentDefinition{
		ID:           row.ID.String(),
		ProjectID:    row.ProjectID.String(),
		ToolUrn:      row.ToolUrn.String(),
		Name:         row.Name,
		Description:  row.Description,
		Title:        conv.FromPGText[string](row.Title),
		Instructions: row.Instructions,
		Tools:        toolStrings,
		Model:        conv.FromPGText[string](row.Model),
		Annotations:  annotations,
		CreatedAt:    row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:    row.UpdatedAt.Time.Format(time.RFC3339),
	}
}
