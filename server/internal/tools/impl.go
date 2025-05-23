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

	params := repo.ListToolsParams{
		ProjectID:    *authCtx.ProjectID,
		Cursor:       uuid.NullUUID{Valid: false, UUID: uuid.Nil},
		DeploymentID: uuid.NullUUID{Valid: false, UUID: uuid.Nil},
	}

	if payload.Cursor != nil {
		params.Cursor = uuid.NullUUID{UUID: uuid.MustParse(*payload.Cursor), Valid: true}
	}

	if payload.DeploymentID != nil {
		params.DeploymentID = uuid.NullUUID{UUID: uuid.MustParse(*payload.DeploymentID), Valid: true}
	}

	tools, err := s.repo.ListTools(ctx, params)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list tools").Log(ctx, s.logger)
	}

	result := &gen.ListToolsResult{
		Tools:      make([]*gen.ToolEntry, len(tools)),
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
			}
		}

		confirm, _ := mv.SanitizeConfirm(confirmRaw)

		result.Tools[i] = &gen.ToolEntry{
			ID:                  tool.ID.String(),
			DeploymentID:        tool.DeploymentID.String(),
			Name:                name,
			Summary:             summary,
			Description:         description,
			Confirm:             string(confirm),
			ConfirmPrompt:       confirmPrompt,
			HTTPMethod:          tool.HttpMethod,
			Path:                tool.Path,
			Tags:                tags,
			Openapiv3DocumentID: tool.Openapiv3DocumentID.UUID.String(),
			PackageName:         pkg,
			CreatedAt:           tool.CreatedAt.Time.Format(time.RFC3339),
			Canonical:           canonical,
		}
	}

	if len(tools) == 100 {
		lastID := tools[len(tools)-1].ID.String()
		result.NextCursor = &lastID
	}

	return result, nil
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}
