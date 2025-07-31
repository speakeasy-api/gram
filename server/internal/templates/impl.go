package templates

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/cbroglie/mustache"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/templates/server"
	gen "github.com/speakeasy-api/gram/server/gen/templates"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/templates/repo"
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
		tracer: otel.Tracer("github.com/speakeasy-api/gram/server/internal/templates"),
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

	logger := s.logger.With(attr.SlogProjectID(projectID.String()))

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to begin transaction").Log(ctx, logger)
	}

	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	tr := s.repo.WithTx(dbtx)

	var args []byte
	if payload.Arguments != nil {
		args = []byte(*payload.Arguments)

		err := validateInputSchema(bytes.NewReader(args))
		switch {
		case errors.Is(err, errSchemaUnsupportedType) || errors.Is(err, errSchemaNotObject):
			return nil, oops.E(oops.CodeInvalid, err, "invalid arguments schema").Log(ctx, logger)
		case errors.Is(err, errSchemaHasNoProperties):
			// This is allowed, it means the schema is empty, which is valid.
		case err != nil:
			return nil, oops.E(oops.CodeBadRequest, err, "failed to validate arguments schema").Log(ctx, logger)
		}
	}

	id, err := tr.CreateTemplate(ctx, repo.CreateTemplateParams{
		ProjectID:   projectID,
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
		return nil, oops.E(oops.CodeUnexpected, err, "unexpected database error").Log(ctx, logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "failed to create template").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to save template").Log(ctx, logger)
	}

	pt, err := mv.DescribePromptTemplate(ctx, logger, s.db, mv.ProjectID(projectID), mv.PromptTemplateID(uuid.NullUUID{UUID: id, Valid: true}), mv.PromptTemplateName(nil))
	if err != nil {
		return nil, err
	}

	return &gen.CreatePromptTemplateResult{Template: pt}, nil
}

func (s *Service) UpdateTemplate(ctx context.Context, payload *gen.UpdateTemplatePayload) (*gen.UpdatePromptTemplateResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	projectID := *authCtx.ProjectID
	logger := s.logger.With(attr.SlogProjectID(projectID.String()))

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to begin update operation").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	tr := s.repo.WithTx(dbtx)

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid template id")
	}

	current, err := tr.GetTemplateByID(ctx, repo.GetTemplateByIDParams{
		ProjectID: projectID,
		ID:        id,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "template not found").Log(ctx, logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get template").Log(ctx, logger)
	}

	nextid := current.ID

	var args []byte
	if payload.Arguments != nil {
		args = []byte(*payload.Arguments)

		err := validateInputSchema(bytes.NewReader(args))
		switch {
		case errors.Is(err, errSchemaUnsupportedType) || errors.Is(err, errSchemaNotObject):
			return nil, oops.E(oops.CodeInvalid, err, "invalid arguments schema").Log(ctx, s.logger)
		case errors.Is(err, errSchemaHasNoProperties):
			// This is allowed, it means the schema is empty, which is valid.
		case err != nil:
			return nil, oops.E(oops.CodeBadRequest, err, "failed to validate arguments schema").Log(ctx, s.logger)
		}
	}

	newid, err := tr.UpdateTemplate(ctx, repo.UpdateTemplateParams{
		ProjectID:   uuid.NullUUID{UUID: projectID, Valid: projectID != uuid.Nil},
		ID:          uuid.NullUUID{UUID: id, Valid: id != uuid.Nil},
		Prompt:      conv.PtrToPGTextEmpty(payload.Prompt),
		Description: conv.PtrToPGTextEmpty(payload.Description),
		Arguments:   args,
		Engine:      conv.PtrToPGTextEmpty(payload.Engine),
		Kind:        conv.PtrToPGTextEmpty(payload.Kind),
		ToolsHint:   payload.ToolsHint,
	})
	switch {
	case err == nil:
		nextid = newid
	case errors.Is(err, sql.ErrNoRows):
		// No change, so we can use the existing id
	default:
		return nil, oops.E(oops.CodeUnexpected, err, "failed to update template").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to save updated template").Log(ctx, s.logger)
	}

	pt, err := mv.DescribePromptTemplate(ctx, logger, s.db,
		mv.ProjectID(projectID),
		mv.PromptTemplateID(uuid.NullUUID{UUID: nextid, Valid: true}),
		mv.PromptTemplateName(nil),
	)
	if err != nil {
		return nil, err
	}

	return &gen.UpdatePromptTemplateResult{Template: pt}, nil
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

func (s *Service) RenderTemplateByID(ctx context.Context, payload *gen.RenderTemplateByIDPayload) (*gen.RenderTemplateResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	projectID := *authCtx.ProjectID
	logger := s.logger.With(attr.SlogProjectID(projectID.String()))

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid template id")
	}

	pt, err := s.repo.GetTemplateByID(ctx, repo.GetTemplateByIDParams{
		ProjectID: projectID,
		ID:        id,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "template not found").Log(ctx, logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get template").Log(ctx, logger)
	}

	prompt := pt.Prompt
	renderedPrompt := prompt
	if pt.Kind.Valid && pt.Kind.String == "higher_order_tool" {
		renderedPrompt, err = s.RenderTemplateJSON(ctx, prompt)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "failed to render template").Log(ctx, logger)
		}
	}

	var data string
	switch pt.Engine.String {
	case "":
		data = pt.Prompt
	case "mustache":
		data, err = mustache.Render(renderedPrompt, payload.Arguments)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "failed to render template").Log(ctx, logger)
		}
	default:
		return nil, oops.E(oops.CodeBadRequest, nil, "unsupported template engine").Log(ctx, logger)
	}

	return &gen.RenderTemplateResult{Prompt: data}, nil
}

func (s *Service) RenderTemplate(ctx context.Context, payload *gen.RenderTemplatePayload) (*gen.RenderTemplateResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	projectID := *authCtx.ProjectID
	logger := s.logger.With(attr.SlogProjectID(projectID.String()))

	prompt := payload.Prompt
	renderedPrompt := prompt
	if payload.Kind == "higher_order_tool" {
		var err error
		renderedPrompt, err = s.RenderTemplateJSON(ctx, prompt)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "failed to render template").Log(ctx, logger)
		}
	}

	var data string
	switch payload.Engine {
	case "":
		data = payload.Prompt
	case "mustache":
		var err error
		data, err = mustache.Render(renderedPrompt, payload.Arguments)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "failed to render template").Log(ctx, logger)
		}
	default:
		return nil, oops.E(oops.CodeBadRequest, nil, "unsupported template engine").Log(ctx, logger)
	}

	return &gen.RenderTemplateResult{Prompt: data}, nil
}

type CustomToolJSONV1 struct {
	ToolName string  `json:"toolName"`
	Purpose  string  `json:"purpose"`
	Inputs   []Input `json:"inputs"`
	Steps    []Step  `json:"steps"`
}

type Input struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type Step struct {
	ID            string   `json:"id"`
	Tool          string   `json:"tool"`
	CanonicalTool string   `json:"canonicalTool"`
	Instructions  string   `json:"instructions"`
	Inputs        []string `json:"inputs"`
}

func (s *Service) RenderTemplateJSON(ctx context.Context, promptJSON string) (string, error) {
	var prompt CustomToolJSONV1
	if err := json.Unmarshal([]byte(promptJSON), &prompt); err != nil {
		return "", oops.E(oops.CodeBadRequest, err, "failed to unmarshal prompt").Log(ctx, s.logger)
	}

	inputsPortion := ""
	for _, input := range prompt.Inputs {
		inputsPortion += fmt.Sprintf("  <Input name=\"%s\" description=\"%s\" />\n", input.Name, input.Description)
	}
	if inputsPortion == "" {
		inputsPortion = "  No inputs needed\n"
	}

	stepsPortion := ""
	for _, step := range prompt.Steps {
		stepInputs := ""
		for _, input := range step.Inputs {
			stepInputs += fmt.Sprintf("    <Input name=\"%s\" />\n", input)
		}

		stepInstructions := fmt.Sprintf("  <Instruction>%s</Instruction>\n%s", step.Instructions, stepInputs)
		if step.Tool == "" {
			// When tool is empty, just show the instruction without CallTool wrapper
			stepsPortion += stepInstructions
		} else {
			stepsPortion += fmt.Sprintf("  <CallTool tool_name=\"%s\">\n  %s  </CallTool>\n", step.Tool, stepInstructions)
		}
	}

	renderedPrompt := fmt.Sprintf(`Here are instructions on how to use the other tools in this toolset to complete the task.
Do NOT use this tool (%s) again when executing the plan.

<Purpose>
  <Instruction>
    You will be provided with a <Purpose>, a list of <Inputs>, and a <Plan>. Your goal is to use the <Plan> and <Inputs> to complete the <Purpose>.
  </Instruction>
  <Purpose>
    %s
  </Purpose>
</Purpose>
<Inputs>
  <Instruction>
    Ask me for each of these inputs before proceeding with the <Plan> below.
  </Instruction>
%s</Inputs>
<Plan>
%s</Plan>`, prompt.ToolName, prompt.Purpose, inputsPortion, stepsPortion)

	return renderedPrompt, nil
}
