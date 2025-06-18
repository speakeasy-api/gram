package tools

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/gen/http/tools/server"
	gen "github.com/speakeasy-api/gram/gen/tools"
	"github.com/speakeasy-api/gram/gen/types"
	"github.com/speakeasy-api/gram/internal/auth"
	"github.com/speakeasy-api/gram/internal/auth/sessions"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	"github.com/speakeasy-api/gram/internal/conv"
	"github.com/speakeasy-api/gram/internal/middleware"
	"github.com/speakeasy-api/gram/internal/mv"
	"github.com/speakeasy-api/gram/internal/oops"
	"github.com/speakeasy-api/gram/internal/tools/repo"
	vr "github.com/speakeasy-api/gram/internal/variations/repo"
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
	return &Service{
		tracer:         otel.Tracer("github.com/speakeasy-api/gram/internal/tools"),
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

	limit := conv.PtrValOrEmpty(payload.Limit, 1000)
	if limit < 1 || limit > 1000 {
		limit = 1000
	}

	params := repo.ListToolsParams{
		ProjectID:    *authCtx.ProjectID,
		Cursor:       uuid.NullUUID{Valid: false, UUID: uuid.Nil},
		DeploymentID: uuid.NullUUID{Valid: false, UUID: uuid.Nil},
		Limit:        limit + 1,
	}

	if payload.Cursor != nil {
		cursorUUID, err := uuid.Parse(*payload.Cursor)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").Log(ctx, s.logger)
		}
		params.Cursor = uuid.NullUUID{UUID: cursorUUID, Valid: true}
	}

	if payload.DeploymentID != nil {
		deploymentUUID, err := uuid.Parse(*payload.DeploymentID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid deployment ID").Log(ctx, s.logger)
		}
		params.DeploymentID = uuid.NullUUID{UUID: deploymentUUID, Valid: true}
	}

	tools, err := s.repo.ListTools(ctx, params)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list tools").Log(ctx, s.logger)
	}

	result := &gen.ListToolsResult{
		Tools:      make([]*types.HTTPToolDefinition, len(tools)),
		NextCursor: nil,
	}

	names := make([]string, 0, len(tools))
	for _, def := range tools {
		names = append(names, def.Name)
	}

	allVariations, err := s.variationsRepo.FindGlobalVariationsByToolNames(ctx, vr.FindGlobalVariationsByToolNamesParams{
		ProjectID: *authCtx.ProjectID,
		ToolNames: names,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list global tool variations").Log(ctx, s.logger)
	}

	keyedVariations := make(map[string]vr.FindGlobalVariationsByToolNamesRow, len(allVariations))
	for _, variation := range allVariations {
		keyedVariations[variation.SrcToolName] = variation
	}

	for i, tool := range tools {
		var pkg *string
		if tool.PackageName != "" {
			pkg = &tool.PackageName
		}

		var variation *types.ToolVariation
		var canonical *types.CanonicalToolAttributes

		name := tool.Name
		summary := tool.Summary
		description := tool.Description
		confirmRaw := conv.PtrValOr(conv.FromPGText[string](tool.Confirm), "")
		confirmPrompt := conv.FromPGText[string](tool.ConfirmPrompt)
		tags := tool.Tags
		variations, ok := keyedVariations[tool.Name]
		if ok {
			name = conv.Default(variations.Name.String, name)
			summary = conv.Default(variations.Summary.String, summary)
			description = conv.Default(variations.Description.String, description)
			confirmRaw = conv.Default(variations.Confirm.String, confirmRaw)
			confirmPrompt = conv.Default(conv.FromPGText[string](variations.ConfirmPrompt), confirmPrompt)
			if len(variations.Tags) > 0 {
				tags = variations.Tags
			}

			canonical = &types.CanonicalToolAttributes{
				VariationID:   variations.ID.String(),
				Name:          tool.Name,
				Summary:       conv.PtrEmpty(tool.Summary),
				Description:   conv.PtrEmpty(tool.Description),
				Tags:          tool.Tags,
				Confirm:       conv.FromPGText[string](tool.Confirm),
				ConfirmPrompt: conv.FromPGText[string](tool.ConfirmPrompt),
				Summarizer:    conv.FromPGText[string](tool.Summarizer),
			}

			variation = &types.ToolVariation{
				ID:            variations.ID.String(),
				GroupID:       variations.GroupID.String(),
				SrcToolName:   tool.Name,
				Confirm:       conv.FromPGText[string](variations.Confirm),
				ConfirmPrompt: conv.FromPGText[string](variations.ConfirmPrompt),
				Name:          conv.PtrEmpty(variations.Name.String),
				Summary:       conv.PtrEmpty(variations.Summary.String),
				Description:   conv.PtrEmpty(variations.Description.String),
				Tags:          variations.Tags,
				Summarizer:    conv.FromPGText[string](variations.Summarizer),
				CreatedAt:     variations.CreatedAt.Time.Format(time.RFC3339),
				UpdatedAt:     variations.UpdatedAt.Time.Format(time.RFC3339),
			}
		}

		confirm, _ := mv.SanitizeConfirm(confirmRaw)

		canonicalName := name
		if canonical != nil {
			canonicalName = canonical.Name
		}

		result.Tools[i] = &types.HTTPToolDefinition{
			ID:                  tool.ID.String(),
			DeploymentID:        tool.DeploymentID.String(),
			ProjectID:           authCtx.ProjectID.String(),
			Name:                name,
			CanonicalName:       canonicalName,
			Summary:             summary,
			Description:         description,
			Confirm:             string(confirm),
			ConfirmPrompt:       confirmPrompt,
			Summarizer:          conv.FromPGText[string](tool.Summarizer),
			HTTPMethod:          tool.HttpMethod,
			Path:                tool.Path,
			Tags:                tags,
			Openapiv3DocumentID: conv.Ptr(tool.Openapiv3DocumentID.UUID.String()),
			Openapiv3Operation:  conv.Ptr(tool.Openapiv3Operation.String),
			SchemaVersion:       conv.Ptr(tool.SchemaVersion),
			Schema:              string(tool.Schema),
			Security:            conv.Ptr(string(tool.Security)),
			PackageName:         pkg,
			CreatedAt:           tool.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt:           tool.UpdatedAt.Time.Format(time.RFC3339),
			Canonical:           canonical,
			Variation:           variation,
		}
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
