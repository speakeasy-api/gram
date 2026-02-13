package agentdefinitions

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
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/agent_definitions"
	srv "github.com/speakeasy-api/gram/server/gen/http/agent_definitions/server"
	"github.com/speakeasy-api/gram/server/internal/agentdefinitions/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
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
	logger = logger.With(attr.SlogComponent("agent_definitions"))

	return &Service{
		tracer: otel.Tracer("github.com/speakeasy-api/gram/server/internal/agentdefinitions"),
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

func (s *Service) CreateAgentDefinition(ctx context.Context, payload *gen.CreateAgentDefinitionPayload) (*gen.AgentDefinitionResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	projectID := *authCtx.ProjectID
	logger := s.logger.With(attr.SlogProjectID(projectID.String()))

	toolURN := urn.NewTool(urn.ToolKindAgent, "gram", string(payload.Name))

	row, err := s.repo.CreateAgentDefinition(ctx, repo.CreateAgentDefinitionParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID,
		Name:           string(payload.Name),
		ToolUrn:        toolURN,
		Model:          payload.Model,
		Title:          ptrToAny(payload.Title),
		Description:    payload.Description,
		Instruction:    payload.Instruction,
		Tools:          payload.Tools,
	})

	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation:
		return nil, oops.E(oops.CodeConflict, err, "agent definition name already exists")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "create agent definition").Log(ctx, logger)
	}

	return &gen.AgentDefinitionResult{AgentDefinition: toView(&row)}, nil
}

func (s *Service) GetAgentDefinition(ctx context.Context, payload *gen.GetAgentDefinitionPayload) (*gen.AgentDefinitionResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	projectID := *authCtx.ProjectID
	logger := s.logger.With(attr.SlogProjectID(projectID.String()))

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid agent definition id")
	}

	row, err := s.repo.GetAgentDefinition(ctx, repo.GetAgentDefinitionParams{
		ID:        id,
		ProjectID: projectID,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "agent definition not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "get agent definition").Log(ctx, logger)
	}

	return &gen.AgentDefinitionResult{AgentDefinition: toView(&row)}, nil
}

func (s *Service) ListAgentDefinitions(ctx context.Context, payload *gen.ListAgentDefinitionsPayload) (*gen.ListAgentDefinitionsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	projectID := *authCtx.ProjectID
	logger := s.logger.With(attr.SlogProjectID(projectID.String()))

	rows, err := s.repo.ListAgentDefinitions(ctx, projectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list agent definitions").Log(ctx, logger)
	}

	views := make([]*gen.AgentDefinitionView, 0, len(rows))
	for _, row := range rows {
		views = append(views, toView(&row))
	}

	return &gen.ListAgentDefinitionsResult{AgentDefinitions: views}, nil
}

func (s *Service) UpdateAgentDefinition(ctx context.Context, payload *gen.UpdateAgentDefinitionPayload) (*gen.AgentDefinitionResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	projectID := *authCtx.ProjectID
	logger := s.logger.With(attr.SlogProjectID(projectID.String()))

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid agent definition id")
	}

	row, err := s.repo.UpdateAgentDefinition(ctx, repo.UpdateAgentDefinitionParams{
		ID:          id,
		ProjectID:   projectID,
		Model:       ptrToAny(payload.Model),
		Title:       ptrToAny(payload.Title),
		Description: ptrToAny(payload.Description),
		Instruction: ptrToAny(payload.Instruction),
		Tools:       payload.Tools,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "agent definition not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "update agent definition").Log(ctx, logger)
	}

	return &gen.AgentDefinitionResult{AgentDefinition: toView(&row)}, nil
}

func (s *Service) DeleteAgentDefinition(ctx context.Context, payload *gen.DeleteAgentDefinitionPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	projectID := *authCtx.ProjectID
	logger := s.logger.With(attr.SlogProjectID(projectID.String()))

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid agent definition id")
	}

	if err := s.repo.DeleteAgentDefinition(ctx, repo.DeleteAgentDefinitionParams{
		ID:        id,
		ProjectID: projectID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete agent definition").Log(ctx, logger)
	}

	return nil
}

func toView(row *repo.AgentDefinition) *gen.AgentDefinitionView {
	var title *string
	if row.Title.Valid {
		title = &row.Title.String
	}

	tools := row.Tools
	if tools == nil {
		tools = []string{}
	}

	return &gen.AgentDefinitionView{
		ID:          row.ID.String(),
		Name:        row.Name,
		ToolUrn:     row.ToolUrn.String(),
		Model:       row.Model,
		Title:       title,
		Description: row.Description,
		Instruction: row.Instruction,
		Tools:       tools,
		CreatedAt:   row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:   row.UpdatedAt.Time.Format(time.RFC3339),
	}
}

func ptrToAny(s *string) any {
	if s == nil {
		return ""
	}
	return *s
}
