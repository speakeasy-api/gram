package tools

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/tools/server"
	gen "github.com/speakeasy-api/gram/server/gen/tools"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	tplRepo "github.com/speakeasy-api/gram/server/internal/templates/repo"
	"github.com/speakeasy-api/gram/server/internal/tools/repo"
	vr "github.com/speakeasy-api/gram/server/internal/variations/repo"
)

type Service struct {
	tracer         trace.Tracer
	logger         *slog.Logger
	db             *pgxpool.Pool
	repo           *repo.Queries
	variationsRepo *vr.Queries
	auth           *auth.Auth
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager) *Service {
	logger = logger.With(attr.SlogComponent("tools"))

	return &Service{
		tracer:         otel.Tracer("github.com/speakeasy-api/gram/server/internal/tools"),
		logger:         logger,
		db:             db,
		repo:           repo.New(db),
		variationsRepo: vr.New(db),
		auth:           auth.New(logger, db, sessions),
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

func (s *Service) ListTools(ctx context.Context, payload *gen.ListToolsPayload) (res *gen.ListToolsResult, err error) {
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	if authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	// TODO: for now setting a sufficiently large limit that is still safe
	// we will need to decide if we will apply filters or paginate on the client side here
	limit := conv.PtrValOrEmpty(payload.Limit, 10000)
	if limit < 1 || limit > 10000 {
		limit = 10000
	}

	// Get HTTP tools
	toolParams := repo.ListHttpToolsParams{
		ProjectID:    *authCtx.ProjectID,
		Cursor:       uuid.NullUUID{Valid: false, UUID: uuid.Nil},
		DeploymentID: uuid.NullUUID{Valid: false, UUID: uuid.Nil},
		UrnPrefix:    pgtype.Text{String: "", Valid: false},
		Limit:        limit + 1,
	}

	if payload.Cursor != nil {
		cursorUUID, err := uuid.Parse(*payload.Cursor)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").Log(ctx, s.logger)
		}
		toolParams.Cursor = uuid.NullUUID{UUID: cursorUUID, Valid: true}
	}

	if payload.DeploymentID != nil {
		deploymentUUID, err := uuid.Parse(*payload.DeploymentID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid deployment ID").Log(ctx, s.logger)
		}
		toolParams.DeploymentID = uuid.NullUUID{UUID: deploymentUUID, Valid: true}
	}

	if payload.UrnPrefix != nil {
		// Escape LIKE wildcards and backslash to treat urn_prefix as a literal value
		escaped := strings.ReplaceAll(*payload.UrnPrefix, "\\", "\\\\")
		escaped = strings.ReplaceAll(escaped, "%", "\\%")
		escaped = strings.ReplaceAll(escaped, "_", "\\_")
		toolParams.UrnPrefix = pgtype.Text{String: escaped, Valid: true}
	}

	tools, err := s.repo.ListHttpTools(ctx, toolParams)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list tools").Log(ctx, s.logger)
	}

	functionTools, err := s.repo.ListFunctionTools(ctx, repo.ListFunctionToolsParams{
		ProjectID:    *authCtx.ProjectID,
		Cursor:       uuid.NullUUID{Valid: false, UUID: uuid.Nil},
		DeploymentID: uuid.NullUUID{Valid: false, UUID: uuid.Nil},
		UrnPrefix:    toolParams.UrnPrefix,
		Limit:        limit + 1,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list function tools").Log(ctx, s.logger)
	}

	// Get prompt templates
	templateRepo := tplRepo.New(s.db)
	templates, err := templateRepo.ListTemplates(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list prompt templates").Log(ctx, s.logger)
	}

	result := &gen.ListToolsResult{
		Tools:      make([]*types.Tool, 0, len(tools)+len(templates)),
		NextCursor: nil,
	}

	for _, tool := range tools {
		var pkg *string
		if tool.PackageName != "" {
			pkg = &tool.PackageName
		}

		name := tool.Name
		summary := tool.Summary
		description := tool.Description
		confirmRaw := conv.PtrValOr(conv.FromPGText[string](tool.Confirm), "")
		confirmPrompt := conv.FromPGText[string](tool.ConfirmPrompt)
		tags := tool.Tags

		confirm, _ := mv.SanitizeConfirm(confirmRaw)

		var responseFilter *types.ResponseFilter
		if tool.ResponseFilter != nil {
			responseFilter = &types.ResponseFilter{
				Type:         string(tool.ResponseFilter.Type),
				StatusCodes:  tool.ResponseFilter.StatusCodes,
				ContentTypes: tool.ResponseFilter.ContentTypes,
			}
		}

		result.Tools = append(result.Tools, &types.Tool{
			HTTPToolDefinition: &types.HTTPToolDefinition{
				ID:                  tool.ID.String(),
				ToolUrn:             tool.ToolUrn.String(),
				DeploymentID:        tool.DeploymentID.String(),
				ProjectID:           authCtx.ProjectID.String(),
				AssetID:             tool.AssetID.UUID.String(),
				Name:                name,
				CanonicalName:       name,
				Summary:             summary,
				Description:         description,
				Confirm:             conv.Ptr(string(confirm)),
				ConfirmPrompt:       confirmPrompt,
				Summarizer:          conv.FromPGText[string](tool.Summarizer),
				ResponseFilter:      responseFilter,
				HTTPMethod:          tool.HttpMethod,
				Path:                tool.Path,
				Tags:                tags,
				Openapiv3DocumentID: conv.Ptr(tool.Openapiv3DocumentID.UUID.String()),
				Openapiv3Operation:  conv.Ptr(tool.Openapiv3Operation.String),
				SchemaVersion:       conv.Ptr(tool.SchemaVersion),
				Schema:              string(tool.Schema),
				Security:            conv.Ptr(string(tool.Security)),
				DefaultServerURL:    conv.FromPGText[string](tool.DefaultServerUrl),
				PackageName:         pkg,
				CreatedAt:           tool.CreatedAt.Time.Format(time.RFC3339),
				UpdatedAt:           tool.UpdatedAt.Time.Format(time.RFC3339),
				Variation:           nil, // Applied later
				Canonical:           nil,
				Annotations:         nil,
			},
		})
	}

	for _, tool := range functionTools {
		var meta map[string]any
		if tool.Meta != nil {
			err = json.Unmarshal(tool.Meta, &meta)
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "failed to unmarshal meta tags").Log(ctx, s.logger)
			}
		}
		result.Tools = append(result.Tools, &types.Tool{
			FunctionToolDefinition: &types.FunctionToolDefinition{
				ID:            tool.ID.String(),
				ToolUrn:       tool.ToolUrn.String(),
				DeploymentID:  tool.DeploymentID.String(),
				ProjectID:     authCtx.ProjectID.String(),
				FunctionID:    tool.FunctionID.String(),
				AssetID:       tool.AssetID.UUID.String(),
				Runtime:       tool.Runtime,
				Name:          tool.Name,
				CanonicalName: tool.Name,
				Description:   tool.Description,
				Variables:     tool.Variables,
				Meta:          meta,
				SchemaVersion: nil,
				Schema:        string(tool.InputSchema),
				Confirm:       nil,
				ConfirmPrompt: nil,
				Summarizer:    nil,
				CreatedAt:     tool.CreatedAt.Time.Format(time.RFC3339),
				UpdatedAt:     tool.UpdatedAt.Time.Format(time.RFC3339),
				Canonical:     nil,
				Variation:     nil,
				Annotations:   nil,
			},
		})
	}

	// Process prompt templates
	for _, template := range templates {
		result.Tools = append(result.Tools, &types.Tool{
			PromptTemplate: &types.PromptTemplate{
				ID:            template.ID.String(),
				HistoryID:     template.HistoryID.String(),
				PredecessorID: conv.FromNullableUUID(template.PredecessorID),
				ToolUrn:       template.ToolUrn.String(),
				Name:          template.Name,
				Prompt:        template.Prompt,
				Description:   conv.PtrValOrEmpty(conv.FromPGText[string](template.Description), ""),
				Schema:        string(template.Arguments),
				SchemaVersion: nil,
				Engine:        conv.PtrValOrEmpty(conv.FromPGText[string](template.Engine), "none"),
				Kind:          conv.PtrValOrEmpty(conv.FromPGText[string](template.Kind), "prompt"),
				ToolsHint:     template.ToolsHint,
				ToolUrnsHint:  template.ToolUrnsHint,
				CreatedAt:     template.CreatedAt.Time.Format(time.RFC3339),
				UpdatedAt:     template.UpdatedAt.Time.Format(time.RFC3339),
				ProjectID:     template.ProjectID.String(),
				CanonicalName: template.Name,
				Confirm:       nil,
				ConfirmPrompt: nil,
				Summarizer:    nil,
				Canonical:     nil,
				Variation:     nil,
				Annotations:   nil,
			},
		})
	}

	err = mv.ApplyVariations(ctx, s.logger, s.db, *authCtx.ProjectID, result.Tools)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to apply variations to tools").Log(ctx, s.logger)
	}

	if len(tools) >= int(limit+1) {
		lastID := tools[len(tools)-1].ID.String()
		result.NextCursor = &lastID
		result.Tools = result.Tools[:len(result.Tools)-1]
	}

	return result, nil
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}
