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
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/tools/server"
	gen "github.com/speakeasy-api/gram/server/gen/tools"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	externalmcprepo "github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	tplRepo "github.com/speakeasy-api/gram/server/internal/templates/repo"
	"github.com/speakeasy-api/gram/server/internal/tools/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	vr "github.com/speakeasy-api/gram/server/internal/variations/repo"
)

type Service struct {
	tracer          trace.Tracer
	logger          *slog.Logger
	db              *pgxpool.Pool
	repo            *repo.Queries
	variationsRepo  *vr.Queries
	auth            *auth.Auth
	authz           *authz.Engine
	featureChecker  platformtools.FeatureChecker
	platformExtras  []platformtools.ExternalTool
	externalMcpRepo *externalmcprepo.Queries
	templateRepo    *tplRepo.Queries
}

var _ gen.Service = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	featureChecker platformtools.FeatureChecker,
	platformExtras []platformtools.ExternalTool,
) *Service {
	logger = logger.With(attr.SlogComponent("tools"))

	return &Service{
		tracer:          tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/tools"),
		logger:          logger,
		db:              db,
		repo:            repo.New(db),
		variationsRepo:  vr.New(db),
		auth:            auth.New(logger, db, sessions, authzEngine),
		authz:           authzEngine,
		featureChecker:  featureChecker,
		platformExtras:  platformExtras,
		externalMcpRepo: externalmcprepo.New(db),
		templateRepo:    tplRepo.New(db),
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

var defaultToolTypes = []types.ToolType{
	types.ToolType(urn.ToolKindHTTP),
	types.ToolType(urn.ToolKindFunction),
	types.ToolType(urn.ToolKindPrompt),
	types.ToolType(urn.ToolKindPlatform),
}

func (s *Service) ListTools(ctx context.Context, payload *gen.ListToolsPayload) (*gen.ListToolsResult, error) {
	// enforce default tool types
	if len(payload.ToolTypes) == 0 {
		payload.ToolTypes = defaultToolTypes
	}

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	if authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	// TODO: for now setting a sufficiently large limit that is still safe
	// we will need to decide if we will apply filters or paginate on the client side here
	limit := conv.PtrValOrEmpty(payload.Limit, 10000)
	if limit < 1 || limit > 10000 {
		limit = 10000
	}

	urnPrefix := pgtype.Text{String: "", Valid: false}
	if payload.UrnPrefix != nil {
		// Escape LIKE wildcards and backslash to treat urn_prefix as a literal value
		escaped := strings.ReplaceAll(*payload.UrnPrefix, "\\", "\\\\")
		escaped = strings.ReplaceAll(escaped, "%", "\\%")
		escaped = strings.ReplaceAll(escaped, "_", "\\_")
		urnPrefix = pgtype.Text{String: escaped, Valid: true}
	}

	result := &gen.ListToolsResult{
		Tools:      []*types.Tool{},
		NextCursor: nil,
	}

	toolKinds := map[urn.ToolKind]any{}
	for _, t := range payload.ToolTypes {
		switch t {
		case types.ToolType(urn.ToolKindHTTP),
			types.ToolType(urn.ToolKindFunction),
			types.ToolType(urn.ToolKindPrompt),
			types.ToolType(urn.ToolKindExternalMCP),
			types.ToolType(urn.ToolKindPlatform):
			toolKinds[urn.ToolKind(t)] = nil
		default:
			return nil, oops.E(oops.CodeBadRequest, nil, "invalid tool type: %s", t).Log(ctx, s.logger)
		}
	}

	if _, ok := toolKinds[urn.ToolKindHTTP]; ok {
		tools, err := s.getHTTPTools(ctx, getHTTPToolsParams{
			projectID:    *authCtx.ProjectID,
			limit:        limit,
			cursor:       payload.Cursor,
			deploymentID: payload.DeploymentID,
			urnPrefix:    urnPrefix,
		})
		if err != nil {
			return nil, err
		}

		result.Tools = append(result.Tools, tools.Tools...)
		result.NextCursor = tools.NextCursor
	}

	if _, ok := toolKinds[urn.ToolKindFunction]; ok {
		tools, err := s.getFunctionTools(ctx, getFunctionToolsParams{
			projectID: *authCtx.ProjectID,
			limit:     limit,
			urnPrefix: urnPrefix,
		})
		if err != nil {
			return nil, err
		}

		result.Tools = append(result.Tools, tools...)
	}

	if _, ok := toolKinds[urn.ToolKindPrompt]; ok {
		tools, err := s.getPromptTemplates(ctx, getPromptTemplatesParams{
			projectID: *authCtx.ProjectID,
		})
		if err != nil {
			return nil, err
		}

		result.Tools = append(result.Tools, tools...)
	}

	if _, ok := toolKinds[urn.ToolKindPlatform]; ok && payload.Cursor == nil {
		result.Tools = append(result.Tools, platformtools.ListTypedTools(
			ctx,
			authCtx.ActiveOrganizationID,
			*authCtx.ProjectID,
			conv.PtrValOrEmpty(payload.UrnPrefix, ""),
			s.featureChecker,
			s.platformExtras...,
		)...)
	}

	if _, ok := toolKinds[urn.ToolKindExternalMCP]; ok {
		tools, err := s.getExternalMCPTools(ctx, getExternalMCPToolsParams{
			projectID:    *authCtx.ProjectID,
			urnPrefix:    urnPrefix,
			deploymentID: payload.DeploymentID,
		})
		if err != nil {
			return nil, err
		}

		result.Tools = append(result.Tools, tools...)
	}

	err := mv.ApplyVariations(ctx, s.logger, s.db, *authCtx.ProjectID, result.Tools)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to apply variations to tools").Log(ctx, s.logger)
	}

	return result, nil
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

type getHTTPToolsParams struct {
	projectID    uuid.UUID
	limit        int32
	cursor       *string
	deploymentID *string
	urnPrefix    pgtype.Text
}

func (s *Service) getHTTPTools(
	ctx context.Context,
	params getHTTPToolsParams,
) (*gen.ListToolsResult, error) {
	// Get HTTP tools
	toolParams := repo.ListHttpToolsParams{
		ProjectID:    params.projectID,
		Cursor:       uuid.NullUUID{Valid: false, UUID: uuid.Nil},
		DeploymentID: uuid.NullUUID{Valid: false, UUID: uuid.Nil},
		UrnPrefix:    params.urnPrefix,
		Limit:        params.limit + 1,
	}

	if params.cursor != nil {
		cursorUUID, err := uuid.Parse(*params.cursor)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").Log(ctx, s.logger)
		}
		toolParams.Cursor = uuid.NullUUID{UUID: cursorUUID, Valid: true}
	}

	if params.deploymentID != nil {
		deploymentUUID, err := uuid.Parse(*params.deploymentID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid deployment ID").Log(ctx, s.logger)
		}
		toolParams.DeploymentID = uuid.NullUUID{UUID: deploymentUUID, Valid: true}
	}

	tools, err := s.repo.ListHttpTools(ctx, toolParams)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list http tools").Log(ctx, s.logger)
	}
	hasNextPage := len(tools) >= int(params.limit+1)
	var nextCursor *string
	if hasNextPage {
		lastID := tools[len(tools)-1].ID.String()
		nextCursor = &lastID
		tools = tools[:len(tools)-1]
	}

	result := &gen.ListToolsResult{
		Tools:      []*types.Tool{},
		NextCursor: nextCursor,
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
				ProjectID:           params.projectID.String(),
				AssetID:             tool.AssetID.UUID.String(),
				Name:                name,
				CanonicalName:       name,
				Summary:             summary,
				Description:         description,
				Confirm:             new(string(confirm)),
				ConfirmPrompt:       confirmPrompt,
				Summarizer:          conv.FromPGText[string](tool.Summarizer),
				ResponseFilter:      responseFilter,
				HTTPMethod:          tool.HttpMethod,
				Path:                tool.Path,
				Tags:                tags,
				Openapiv3DocumentID: new(tool.Openapiv3DocumentID.UUID.String()),
				Openapiv3Operation:  new(tool.Openapiv3Operation.String),
				SchemaVersion:       new(tool.SchemaVersion),
				Schema:              string(tool.Schema),
				Security:            new(string(tool.Security)),
				DefaultServerURL:    conv.FromPGText[string](tool.DefaultServerUrl),
				PackageName:         pkg,
				CreatedAt:           tool.CreatedAt.Time.Format(time.RFC3339),
				UpdatedAt:           tool.UpdatedAt.Time.Format(time.RFC3339),
				Variation:           nil, // Applied later
				Canonical:           nil,
				Annotations: conv.AnnotationsFromColumns(
					tool.ReadOnlyHint,
					tool.DestructiveHint,
					tool.IdempotentHint,
					tool.OpenWorldHint,
				),
			},
		})
	}

	return result, nil
}

type getFunctionToolsParams struct {
	projectID uuid.UUID
	limit     int32
	urnPrefix pgtype.Text
}

func (s *Service) getFunctionTools(
	ctx context.Context,
	params getFunctionToolsParams,
) ([]*types.Tool, error) {
	tools, err := s.repo.ListFunctionTools(ctx, repo.ListFunctionToolsParams{
		ProjectID:    params.projectID,
		Cursor:       uuid.NullUUID{Valid: false, UUID: uuid.Nil},
		DeploymentID: uuid.NullUUID{Valid: false, UUID: uuid.Nil},
		UrnPrefix:    params.urnPrefix,
		Limit:        params.limit + 1,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list function tools").Log(ctx, s.logger)
	}

	result := []*types.Tool{}

	for _, tool := range tools {
		var meta map[string]any
		if tool.Meta != nil {
			err = json.Unmarshal(tool.Meta, &meta)
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "failed to unmarshal meta tags").Log(ctx, s.logger)
			}
		}
		result = append(result, &types.Tool{
			FunctionToolDefinition: &types.FunctionToolDefinition{
				ID:            tool.ID.String(),
				ToolUrn:       tool.ToolUrn.String(),
				DeploymentID:  tool.DeploymentID.String(),
				ProjectID:     params.projectID.String(),
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
				Annotations: conv.AnnotationsFromColumns(
					tool.ReadOnlyHint,
					tool.DestructiveHint,
					tool.IdempotentHint,
					tool.OpenWorldHint,
				),
			},
		})
	}

	return result, nil
}

type getPromptTemplatesParams struct {
	projectID uuid.UUID
}

func (s *Service) getPromptTemplates(ctx context.Context, params getPromptTemplatesParams) ([]*types.Tool, error) {
	templates, err := s.templateRepo.ListTemplates(ctx, params.projectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list prompt templates").Log(ctx, s.logger)
	}

	result := []*types.Tool{}

	// Process prompt templates
	for _, template := range templates {
		result = append(result, &types.Tool{
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

	return result, nil
}

type getExternalMCPToolsParams struct {
	projectID    uuid.UUID
	urnPrefix    pgtype.Text
	deploymentID *string
}

func (s *Service) getExternalMCPTools(
	ctx context.Context,
	params getExternalMCPToolsParams,
) ([]*types.Tool, error) {
	queryParams := externalmcprepo.ListDirectExternalMCPToolDefinitionsParams{
		UrnPrefix:    params.urnPrefix,
		ProjectID:    params.projectID,
		DeploymentID: uuid.NullUUID{Valid: false, UUID: uuid.Nil},
	}

	if params.deploymentID != nil {
		deploymentUUID, err := uuid.Parse(*params.deploymentID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid deployment ID").Log(ctx, s.logger)
		}
		queryParams.DeploymentID = uuid.NullUUID{UUID: deploymentUUID, Valid: true}
	}

	tools, err := s.externalMcpRepo.ListDirectExternalMCPToolDefinitions(
		ctx,
		queryParams,
	)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list external mcp tools").Log(ctx, s.logger)
	}

	result := []*types.Tool{}
	for _, tool := range tools {
		result = append(result, &types.Tool{
			ExternalMcpToolDefinition: &types.ExternalMCPToolDefinition{
				ID:                         tool.ID.String(),
				ProjectID:                  params.projectID.String(),
				ToolUrn:                    tool.ToolUrn,
				Type:                       &tool.Type,
				Name:                       tool.Name.String,
				CanonicalName:              tool.Name.String,
				Description:                tool.Description.String,
				Schema:                     string(tool.Schema),
				SchemaVersion:              nil,
				DeploymentExternalMcpID:    tool.ExternalMcpAttachmentID.String(),
				DeploymentID:               tool.DeploymentID.String(),
				RegistryID:                 conv.PtrValOrEmpty(conv.FromNullableUUID(tool.RegistryID), ""),
				RegistryServerName:         tool.RegistryServerName,
				RegistrySpecifier:          tool.RegistryServerSpecifier,
				Slug:                       tool.Slug,
				RemoteURL:                  tool.RemoteUrl,
				TransportType:              tool.TransportType.String(),
				RequiresOauth:              tool.RequiresOauth,
				OauthVersion:               tool.OauthVersion,
				OauthAuthorizationEndpoint: conv.FromPGText[string](tool.OauthAuthorizationEndpoint),
				OauthTokenEndpoint:         conv.FromPGText[string](tool.OauthTokenEndpoint),
				OauthRegistrationEndpoint:  conv.FromPGText[string](tool.OauthRegistrationEndpoint),
				OauthScopesSupported:       tool.OauthScopesSupported,
				CreatedAt:                  tool.CreatedAt.Time.Format(time.RFC3339),
				UpdatedAt:                  tool.UpdatedAt.Time.Format(time.RFC3339),
				Confirm:                    nil,
				ConfirmPrompt:              nil,
				Summarizer:                 nil,
				Canonical:                  nil,
				Variation:                  nil,
				Annotations: conv.AnnotationsFromColumns(
					tool.ReadOnlyHint,
					tool.DestructiveHint,
					tool.IdempotentHint,
					tool.OpenWorldHint,
				),
			},
		})
	}

	return result, nil
}
