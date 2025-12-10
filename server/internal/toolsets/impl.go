package toolsets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/toolsets/server"
	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	domainsRepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	deploymentsRepo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	environmentsRepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	oauthRepo "github.com/speakeasy-api/gram/server/internal/oauth/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	tplRepo "github.com/speakeasy-api/gram/server/internal/templates/repo"
	"github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usageRepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
)

var allowedEnabledServers = map[string]int{
	"free": 1,
	"pro":  5,
}

type Service struct {
	tracer          trace.Tracer
	logger          *slog.Logger
	db              *pgxpool.Pool
	repo            *repo.Queries
	environmentRepo *environmentsRepo.Queries
	auth            *auth.Auth
	toolsets        *Toolsets
	domainsRepo     *domainsRepo.Queries
	usageRepo       *usageRepo.Queries
	oauthRepo       *oauthRepo.Queries
	toolsetCache    cache.TypedCacheObject[mv.ToolsetBaseContents]
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager, cacheAdapter cache.Cache) *Service {
	logger = logger.With(attr.SlogComponent("toolsets"))

	return &Service{
		tracer:          otel.Tracer("github.com/speakeasy-api/gram/server/internal/toolsets"),
		logger:          logger,
		db:              db,
		repo:            repo.New(db),
		auth:            auth.New(logger, db, sessions),
		environmentRepo: environmentsRepo.New(db),
		toolsets:        NewToolsets(db),
		domainsRepo:     domainsRepo.New(db),
		usageRepo:       usageRepo.New(db),
		oauthRepo:       oauthRepo.New(db),
		toolsetCache:    cache.NewTypedObjectCache[mv.ToolsetBaseContents](logger.With(attr.SlogCacheNamespace("toolset")), cacheAdapter, cache.SuffixNone),
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

func (s *Service) CreateToolset(ctx context.Context, payload *gen.CreateToolsetPayload) (*types.Toolset, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil || authCtx.OrganizationSlug == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	slugSuffix, err := conv.GenerateRandomSlug(5)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to generate random slug").Log(ctx, s.logger)
	}

	mcpSlug := authCtx.OrganizationSlug + "-" + slugSuffix

	enabledServerCount, err := s.usageRepo.GetEnabledServerCount(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		// don't block the user from creating a toolset
		s.logger.ErrorContext(ctx, "error getting enabled server count", attr.SlogError(err), attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	}

	createToolParams := repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   payload.Name,
		Slug:                   conv.ToSlug(payload.Name),
		Description:            conv.PtrToPGText(payload.Description),
		DefaultEnvironmentSlug: conv.PtrToPGText(nil),
		McpSlug:                conv.ToPGText(mcpSlug),
		McpEnabled:             enabledServerCount == 0, // we automatically enable the first available toolset in an organization as an MCP server
	}

	if payload.DefaultEnvironmentSlug != nil {
		_, err := s.environmentRepo.GetEnvironmentBySlug(ctx, environmentsRepo.GetEnvironmentBySlugParams{
			Slug:      conv.ToLower(*payload.DefaultEnvironmentSlug),
			ProjectID: *authCtx.ProjectID,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error finding environment")
		}
		createToolParams.DefaultEnvironmentSlug = conv.ToPGText(conv.ToLower(*payload.DefaultEnvironmentSlug))
	} else {
		environments, err := s.environmentRepo.ListEnvironments(ctx, *authCtx.ProjectID)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error listing environments")
		}
		for _, environment := range environments {
			if environment.Slug == "default" { // We will autofill the default environment if one is available
				createToolParams.DefaultEnvironmentSlug = conv.ToPGText(environment.Slug)
				break
			}
		}
	}

	createdToolset, err := s.repo.CreateToolset(ctx, createToolParams)
	var pgErr *pgconn.PgError
	if err != nil {
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, nil, "toolset slug already exists")
		}

		return nil, oops.E(oops.CodeUnexpected, err, "failed to create toolset").Log(ctx, s.logger)
	}

	// Create initial toolset version with tool URNs
	err = s.createToolsetVersion(ctx, payload.ToolUrns, payload.ResourceUrns, createdToolset.ID, s.repo)
	if err != nil {
		return nil, err
	}

	toolsetDetails, err := mv.DescribeToolset(ctx, s.logger, s.db, mv.ProjectID(*authCtx.ProjectID), mv.ToolsetSlug(createdToolset.Slug), &s.toolsetCache)
	if err != nil {
		return nil, err
	}

	return toolsetDetails, nil
}

func (s *Service) ListToolsets(ctx context.Context, payload *gen.ListToolsetsPayload) (*gen.ListToolsetsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	toolsets, err := s.repo.ListToolsetsByProject(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list toolsets").Log(ctx, s.logger)
	}

	result := make([]*types.ToolsetEntry, len(toolsets))
	for i, toolset := range toolsets {
		toolsetDetails, err := mv.DescribeToolsetEntry(ctx, s.logger, s.db, mv.ProjectID(*authCtx.ProjectID), mv.ToolsetSlug(toolset.Slug))
		if err != nil {
			return nil, err
		}
		result[i] = toolsetDetails
	}

	return &gen.ListToolsetsResult{
		Toolsets: result,
	}, nil
}

func (s *Service) UpdateToolset(ctx context.Context, payload *gen.UpdateToolsetPayload) (*types.Toolset, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()), attr.SlogToolsetSlug(string(payload.Slug)))

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to begin transaction").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	tr := s.repo.WithTx(dbtx)

	// First get the existing toolset
	existingToolset, err := tr.GetToolset(ctx, repo.GetToolsetParams{
		Slug:      conv.ToLower(payload.Slug),
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "toolset not found").Log(ctx, logger)
	}

	// Convert update params
	updateParams := repo.UpdateToolsetParams{
		Slug:                   conv.ToLower(payload.Slug),
		Description:            existingToolset.Description,
		Name:                   existingToolset.Name,
		DefaultEnvironmentSlug: existingToolset.DefaultEnvironmentSlug,
		ProjectID:              *authCtx.ProjectID,
		McpSlug:                existingToolset.McpSlug,
		McpEnabled:             existingToolset.McpEnabled,
		ToolSelectionMode:      existingToolset.ToolSelectionMode,
		CustomDomainID:         existingToolset.CustomDomainID,
		McpIsPublic:            existingToolset.McpIsPublic,
	}
	if payload.Name != nil {
		updateParams.Name = *payload.Name
	}
	if payload.Description != nil {
		updateParams.Description = pgtype.Text{String: *payload.Description, Valid: true}
	}

	if payload.DefaultEnvironmentSlug != nil {
		_, err := s.environmentRepo.GetEnvironmentBySlug(ctx, environmentsRepo.GetEnvironmentBySlugParams{
			Slug:      conv.ToLower(*payload.DefaultEnvironmentSlug),
			ProjectID: *authCtx.ProjectID,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error finding environment")
		}
		updateParams.DefaultEnvironmentSlug = conv.ToPGText(conv.ToLower(*payload.DefaultEnvironmentSlug))
	}

	var activeCustomDomainID *uuid.UUID
	toolsetDomainID := conv.FromNullableUUID(existingToolset.CustomDomainID)
	if domain, err := s.domainsRepo.GetCustomDomainByOrganization(ctx, authCtx.ActiveOrganizationID); err == nil && domain.Activated && domain.Verified {
		activeCustomDomainID = &domain.ID
	}

	if payload.CustomDomainID != nil && activeCustomDomainID != nil && *payload.CustomDomainID == activeCustomDomainID.String() {
		updateParams.CustomDomainID = uuid.NullUUID{UUID: *activeCustomDomainID, Valid: true}
		toolsetDomainID = payload.CustomDomainID
	}

	if payload.McpSlug != nil && *payload.McpSlug != "" {
		// For free accounts, enforce that the MCP slug is prefixed with the org slug
		if toolsetDomainID == nil || authCtx.AccountType == "free" {
			if !strings.HasPrefix(conv.ToLower(*payload.McpSlug), authCtx.OrganizationSlug+"-") {
				return nil, oops.E(oops.CodeBadRequest, nil, "mcp slug must be prefixed with the org slug for free accounts")
			}

			mcpToolset, mcpToolsetErr := tr.GetToolsetByMcpSlug(ctx, conv.ToPGText(conv.ToLower(*payload.McpSlug)))
			if mcpToolsetErr == nil && mcpToolset.ID != existingToolset.ID {
				return nil, oops.E(oops.CodeConflict, nil, "this slug is already taken")
			}
			updateParams.McpSlug = conv.ToPGText(conv.ToLower(*payload.McpSlug))
		} else {
			mcpToolset, mcpToolsetErr := tr.GetToolsetByMcpSlugAndCustomDomain(ctx, repo.GetToolsetByMcpSlugAndCustomDomainParams{
				McpSlug:        conv.ToPGText(conv.ToLower(*payload.McpSlug)),
				CustomDomainID: uuid.NullUUID{UUID: uuid.MustParse(*toolsetDomainID), Valid: true},
			})
			if mcpToolsetErr == nil && mcpToolset.ID != existingToolset.ID {
				return nil, oops.E(oops.CodeConflict, nil, "this slug is already taken")
			}
			updateParams.McpSlug = conv.ToPGText(conv.ToLower(*payload.McpSlug))
		}
	}

	if payload.McpIsPublic != nil {
		updateParams.McpIsPublic = *payload.McpIsPublic
	}

	// Server-side enforcement of limit on # of enabled MCP servers by account type
	if payload.McpEnabled != nil {
		if *payload.McpEnabled && !existingToolset.McpSlug.Valid && (payload.McpSlug == nil || *payload.McpSlug == "") {
			// sanity check this should not be able to happens
			return nil, oops.E(oops.CodeBadRequest, nil, "mcp slug is required to set mcp is public")
		}

		enabledServerLimit, ok := allowedEnabledServers[authCtx.AccountType]
		if *payload.McpEnabled && !existingToolset.McpEnabled && ok {
			enabledServers, err := s.repo.ListEnabledToolsetsByOrganization(ctx, authCtx.ActiveOrganizationID)
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "error listing enabled toolsets").Log(ctx, logger)
			}

			if len(enabledServers) >= enabledServerLimit {
				return nil, oops.E(oops.CodeForbidden, nil, "%s", fmt.Sprintf("you have reached the maximum number of public MCP servers for your account type: %d", enabledServerLimit)).Log(ctx, logger)
			}
		}

		updateParams.McpEnabled = *payload.McpEnabled
	}

	if payload.ToolSelectionMode != nil {
		updateParams.ToolSelectionMode = *payload.ToolSelectionMode
	}

	err = s.createToolsetVersion(ctx, payload.ToolUrns, payload.ResourceUrns, existingToolset.ID, tr)
	if err != nil {
		return nil, err
	}

	updatedToolset, err := tr.UpdateToolset(ctx, updateParams)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error updating toolset").Log(ctx, logger)
	}

	if payload.PromptTemplateNames != nil {
		err = s.updatePromptTemplates(ctx, dbtx, *authCtx.ProjectID, existingToolset.ID, payload.PromptTemplateNames, logger)
		if err != nil {
			return nil, err
		}
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving updated toolset").Log(ctx, logger)
	}

	toolsetDetails, err := mv.DescribeToolset(ctx, logger, s.db, mv.ProjectID(*authCtx.ProjectID), mv.ToolsetSlug(updatedToolset.Slug), &s.toolsetCache)
	if err != nil {
		return nil, err
	}

	return toolsetDetails, nil
}

func (s *Service) DeleteToolset(ctx context.Context, payload *gen.DeleteToolsetPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	err := s.repo.DeleteToolset(ctx, repo.DeleteToolsetParams{
		Slug:      conv.ToLower(payload.Slug),
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return oops.E(oops.CodeUnexpected, err, "failed to delete toolset").Log(ctx, s.logger)
	}

	return nil
}

func (s *Service) GetToolset(ctx context.Context, payload *gen.GetToolsetPayload) (*types.Toolset, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	return mv.DescribeToolset(ctx, s.logger, s.db, mv.ProjectID(*authCtx.ProjectID), mv.ToolsetSlug(payload.Slug), &s.toolsetCache)
}

func (s *Service) CloneToolset(ctx context.Context, payload *gen.CloneToolsetPayload) (*types.Toolset, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()), attr.SlogToolsetSlug(string(payload.Slug)))

	// Get the original toolset
	originalToolset, err := s.repo.GetToolset(ctx, repo.GetToolsetParams{
		Slug:      conv.ToLower(payload.Slug),
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "toolset not found").Log(ctx, logger)
	}

	// Generate new slug with _copy suffix
	slugSuffix, err := conv.GenerateRandomSlug(5)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to generate random slug").Log(ctx, logger)
	}

	newName := originalToolset.Name + "_copy"
	newSlug := conv.ToSlug(newName)
	mcpSlug := authCtx.OrganizationSlug + "-" + slugSuffix

	// Prepare base parameters for creating the cloned toolset
	baseParams := repo.CreateToolsetParams{
		Name:                   newName,
		Slug:                   newSlug,
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Description:            originalToolset.Description,
		DefaultEnvironmentSlug: originalToolset.DefaultEnvironmentSlug,
		McpSlug:                conv.ToPGText(mcpSlug),
		McpEnabled:             false, // Don't auto-enable MCP for cloned toolsets
	}

	// Try to create the cloned toolset, handling name conflicts
	var clonedToolset repo.Toolset
	clonedToolset, err = s.repo.CreateToolset(ctx, baseParams)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			// Try with a numbered suffix if _copy already exists
			for i := 2; i <= 10; i++ {
				baseParams.Name = fmt.Sprintf("%s_copy%d", originalToolset.Name, i)
				baseParams.Slug = conv.ToSlug(baseParams.Name)
				clonedToolset, err = s.repo.CreateToolset(ctx, baseParams)
				if err == nil {
					break
				}
				if !errors.As(err, &pgErr) || pgErr.Code != pgerrcode.UniqueViolation {
					return nil, oops.E(oops.CodeUnexpected, err, "failed to clone toolset").Log(ctx, logger)
				}
			}
			if err != nil {
				return nil, oops.E(oops.CodeConflict, nil, "could not create unique toolset name").Log(ctx, logger)
			}
		} else {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to clone toolset").Log(ctx, logger)
		}
	}

	// Clone the latest toolset version
	latestVersion, err := s.repo.GetLatestToolsetVersion(ctx, originalToolset.ID)
	if err != nil {
		logger.WarnContext(ctx, "failed to get latest toolset version", attr.SlogError(err))
	} else {
		_, err = s.repo.CreateToolsetVersion(ctx, repo.CreateToolsetVersionParams{
			ToolsetID:     clonedToolset.ID,
			Version:       1,
			ToolUrns:      latestVersion.ToolUrns,
			ResourceUrns:  latestVersion.ResourceUrns,
			PredecessorID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		})
		if err != nil {
			logger.ErrorContext(ctx, "failed to create toolset version for clone", attr.SlogError(err))
		}
	}

	// Clone prompt templates if any
	originalPromptTemplates, err := s.repo.GetToolsetPromptTemplateNames(ctx, repo.GetToolsetPromptTemplateNamesParams{
		ToolsetID: originalToolset.ID,
		ProjectID: *authCtx.ProjectID,
	})
	if err == nil && len(originalPromptTemplates) > 0 {
		tplr := tplRepo.New(s.db)
		ptrows, err := tplr.PeekTemplatesByNames(ctx, tplRepo.PeekTemplatesByNamesParams{
			ProjectID: *authCtx.ProjectID,
			Names:     originalPromptTemplates,
		})
		if err == nil {
			additions := make([]repo.AddToolsetPromptTemplatesParams, 0, len(ptrows))
			for _, ptrow := range ptrows {
				additions = append(additions, repo.AddToolsetPromptTemplatesParams{
					ProjectID:        *authCtx.ProjectID,
					ToolsetID:        clonedToolset.ID,
					PromptHistoryID:  ptrow.HistoryID,
					PromptTemplateID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
					PromptName:       ptrow.Name,
				})
			}
			_, err = s.repo.AddToolsetPromptTemplates(ctx, additions)
			if err != nil {
				logger.WarnContext(ctx, "failed to clone prompt templates", attr.SlogError(err))
			}
		}
	}

	toolsetDetails, err := mv.DescribeToolset(ctx, logger, s.db, mv.ProjectID(*authCtx.ProjectID), mv.ToolsetSlug(clonedToolset.Slug), &s.toolsetCache)
	if err != nil {
		logger.ErrorContext(ctx, "failed to describe cloned toolset", attr.SlogError(err))
		return nil, oops.E(oops.CodeUnexpected, err, "failed to describe cloned toolset").Log(ctx, logger)
	}

	if toolsetDetails == nil {
		logger.ErrorContext(ctx, "toolsetDetails is nil after successful describe")
		return nil, oops.E(oops.CodeUnexpected, nil, "toolset details is nil").Log(ctx, logger)
	}

	logger.InfoContext(ctx, "successfully cloned toolset",
		attr.SlogToolsetSlug(string(payload.Slug)),
		attr.SlogToolsetSlug(clonedToolset.Slug))

	return toolsetDetails, nil
}

func (s *Service) CheckMCPSlugAvailability(ctx context.Context, payload *gen.CheckMCPSlugAvailabilityPayload) (bool, error) {
	//nolint:wrapcheck // Wrapping adds no value here
	return s.repo.CheckMCPSlugAvailability(ctx, conv.ToPGText(conv.ToLower(payload.Slug)))
}

func (s *Service) AddExternalOAuthServer(ctx context.Context, payload *gen.AddExternalOAuthServerPayload) (*types.Toolset, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if authCtx.AccountType == "free" {
		return nil, oops.E(oops.CodeForbidden, nil, "free accounts cannot add external OAuth servers").Log(ctx, s.logger)
	}

	toolsetDetails, err := mv.DescribeToolset(ctx, s.logger, s.db, mv.ProjectID(*authCtx.ProjectID), mv.ToolsetSlug(payload.Slug), &s.toolsetCache)
	if err != nil {
		return nil, err
	}

	if toolsetDetails.McpIsPublic == nil || !*toolsetDetails.McpIsPublic {
		return nil, oops.E(oops.CodeBadRequest, nil, "private MCP servers cannot have external OAuth servers").Log(ctx, s.logger)
	}

	if toolsetDetails.ExternalOauthServer != nil || toolsetDetails.OauthProxyServer != nil {
		return nil, oops.E(oops.CodeConflict, nil, "external OAuth server already exists").Log(ctx, s.logger)
	}

	if toolsetDetails.OauthEnablementMetadata != nil && toolsetDetails.OauthEnablementMetadata.Oauth2SecurityCount > 1 {
		return nil, oops.E(oops.CodeBadRequest, nil, "multiple OAuth2 security schemes detected").Log(ctx, s.logger)
	}

	// Marshal metadata to JSON bytes
	metadataBytes, err := json.Marshal(payload.ExternalOauthServer.Metadata)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid metadata format").Log(ctx, s.logger)
	}

	// Create the external OAuth server metadata entry
	externalOAuthServer, err := s.oauthRepo.CreateExternalOAuthServerMetadata(ctx, oauthRepo.CreateExternalOAuthServerMetadataParams{
		ProjectID: *authCtx.ProjectID,
		Slug:      conv.ToLower(payload.ExternalOauthServer.Slug),
		Metadata:  metadataBytes,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to create external OAuth server").Log(ctx, s.logger)
	}

	// Associate it with the toolset
	_, err = s.repo.UpdateToolsetExternalOAuthServer(ctx, repo.UpdateToolsetExternalOAuthServerParams{
		Slug:                  conv.ToLower(payload.Slug),
		ProjectID:             *authCtx.ProjectID,
		ExternalOauthServerID: uuid.NullUUID{UUID: externalOAuthServer.ID, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "toolset not found").Log(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "failed to associate external OAuth server with toolset").Log(ctx, s.logger)
	}

	toolsetDetails, err = mv.DescribeToolset(ctx, s.logger, s.db, mv.ProjectID(*authCtx.ProjectID), mv.ToolsetSlug(payload.Slug), &s.toolsetCache)
	if err != nil {
		return nil, err
	}

	return toolsetDetails, nil
}

func (s *Service) RemoveOAuthServer(ctx context.Context, payload *gen.RemoveOAuthServerPayload) (*types.Toolset, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	// Get the current toolset to find which OAuth server to remove
	existingToolset, err := s.repo.GetToolset(ctx, repo.GetToolsetParams{
		Slug:      conv.ToLower(payload.Slug),
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "toolset not found").Log(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get toolset").Log(ctx, s.logger)
	}

	// Remove external OAuth server metadata if it exists
	if existingToolset.ExternalOauthServerID.Valid {
		err = s.oauthRepo.DeleteExternalOAuthServerMetadata(ctx, oauthRepo.DeleteExternalOAuthServerMetadataParams{
			ProjectID: *authCtx.ProjectID,
			ID:        existingToolset.ExternalOauthServerID.UUID,
		})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to delete external OAuth server metadata").Log(ctx, s.logger)
		}
	}

	// Clear OAuth server associations from toolset
	_, err = s.repo.ClearToolsetOAuthServers(ctx, repo.ClearToolsetOAuthServersParams{
		Slug:      conv.ToLower(payload.Slug),
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "toolset not found").Log(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "failed to remove OAuth server from toolset").Log(ctx, s.logger)
	}

	toolsetDetails, err := mv.DescribeToolset(ctx, s.logger, s.db, mv.ProjectID(*authCtx.ProjectID), mv.ToolsetSlug(payload.Slug), &s.toolsetCache)
	if err != nil {
		return nil, err
	}

	return toolsetDetails, nil
}

func (s *Service) AddOAuthProxyServer(ctx context.Context, payload *gen.AddOAuthProxyServerPayload) (*types.Toolset, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	toolsetDetails, err := mv.DescribeToolset(ctx, s.logger, s.db, mv.ProjectID(*authCtx.ProjectID), mv.ToolsetSlug(payload.Slug), &s.toolsetCache)
	if err != nil {
		return nil, err
	}

	if toolsetDetails.OauthProxyServer != nil || toolsetDetails.ExternalOauthServer != nil {
		return nil, oops.E(oops.CodeConflict, nil, "OAuth server already exists").Log(ctx, s.logger)
	}

	oauth2AuthCodeSecurityCount := 0
	for _, securityVariable := range toolsetDetails.SecurityVariables {
		isAuthorizationCode := securityVariable.Type != nil && *securityVariable.Type == "oauth2" && securityVariable.OauthTypes != nil && slices.Contains(securityVariable.OauthTypes, "authorization_code")
		isOpenIdConnect := securityVariable.Type != nil && *securityVariable.Type == "openIdConnect"
		if isAuthorizationCode || isOpenIdConnect {
			oauth2AuthCodeSecurityCount++
		}
	}

	if oauth2AuthCodeSecurityCount > 1 {
		return nil, oops.E(oops.CodeBadRequest, nil, "multiple OAuth2 security schemes detected").Log(ctx, s.logger)
	}

	// Validate token_endpoint_auth_methods_supported
	validAuthMethods := map[string]bool{
		"client_secret_basic": true,
		"client_secret_post":  true,
		"none":                true,
	}

	for _, method := range payload.OauthProxyServer.TokenEndpointAuthMethodsSupported {
		if !validAuthMethods[method] {
			return nil, oops.E(oops.CodeBadRequest, nil, "invalid token_endpoint_auth_methods_supported value: %s (must be client_secret_basic or client_secret_post)", method).Log(ctx, s.logger)
		}
	}

	// Validate provider_type
	validProviderTypes := map[string]bool{
		"custom": true,
		"gram":   true,
	}

	if !validProviderTypes[payload.OauthProxyServer.ProviderType] {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid provider_type value: %s (must be 'custom' or 'gram')", payload.OauthProxyServer.ProviderType).Log(ctx, s.logger)
	}

	// Validate provider_type against public/private status
	isPublic := toolsetDetails.McpIsPublic != nil && *toolsetDetails.McpIsPublic
	if payload.OauthProxyServer.ProviderType == "gram" && isPublic {
		return nil, oops.E(oops.CodeBadRequest, nil, "gram provider type can only be used with private MCP servers").Log(ctx, s.logger)
	}
	if payload.OauthProxyServer.ProviderType == "custom" && !isPublic {
		return nil, oops.E(oops.CodeBadRequest, nil, "custom provider type can only be used with public MCP servers").Log(ctx, s.logger)
	}

	// Validate required fields for custom provider type
	if payload.OauthProxyServer.ProviderType == "custom" {
		if payload.OauthProxyServer.AuthorizationEndpoint == nil || *payload.OauthProxyServer.AuthorizationEndpoint == "" {
			return nil, oops.E(oops.CodeBadRequest, nil, "authorization_endpoint is required for custom provider type").Log(ctx, s.logger)
		}
		if payload.OauthProxyServer.TokenEndpoint == nil || *payload.OauthProxyServer.TokenEndpoint == "" {
			return nil, oops.E(oops.CodeBadRequest, nil, "token_endpoint is required for custom provider type").Log(ctx, s.logger)
		}
		if len(payload.OauthProxyServer.ScopesSupported) == 0 {
			return nil, oops.E(oops.CodeBadRequest, nil, "scopes_supported is required for custom provider type").Log(ctx, s.logger)
		}
		if len(payload.OauthProxyServer.TokenEndpointAuthMethodsSupported) == 0 {
			return nil, oops.E(oops.CodeBadRequest, nil, "token_endpoint_auth_methods_supported is required for custom provider type").Log(ctx, s.logger)
		}
	}

	// Create the OAuth proxy server
	// Validate that the environment exists for this project
	_, err = s.environmentRepo.GetEnvironmentBySlug(ctx, environmentsRepo.GetEnvironmentBySlugParams{
		Slug:      string(payload.OauthProxyServer.EnvironmentSlug),
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "environment not found").Log(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get environment").Log(ctx, s.logger)
	}

	oauthProxyServer, err := s.oauthRepo.UpsertOAuthProxyServer(ctx, oauthRepo.UpsertOAuthProxyServerParams{
		ProjectID: *authCtx.ProjectID,
		Slug:      conv.ToLower(payload.OauthProxyServer.Slug),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to create OAuth proxy server").Log(ctx, s.logger)
	}

	// Create the OAuth proxy provider with the secrets containing environment_slug
	secretsJSON, err := json.Marshal(map[string]string{
		"environment_slug": string(payload.OauthProxyServer.EnvironmentSlug),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to marshal secrets").Log(ctx, s.logger)
	}

	_, err = s.oauthRepo.UpsertOAuthProxyProvider(ctx, oauthRepo.UpsertOAuthProxyProviderParams{
		ProjectID:                         *authCtx.ProjectID,
		OauthProxyServerID:                oauthProxyServer.ID,
		Slug:                              conv.ToLower(payload.OauthProxyServer.Slug),
		ProviderType:                      payload.OauthProxyServer.ProviderType,
		AuthorizationEndpoint:             conv.PtrToPGTextEmpty(payload.OauthProxyServer.AuthorizationEndpoint),
		TokenEndpoint:                     conv.PtrToPGTextEmpty(payload.OauthProxyServer.TokenEndpoint),
		RegistrationEndpoint:              conv.PtrToPGText(nil),
		ScopesSupported:                   payload.OauthProxyServer.ScopesSupported,
		ResponseTypesSupported:            []string{"code"},
		ResponseModesSupported:            []string{},
		GrantTypesSupported:               []string{"authorization_code"},
		TokenEndpointAuthMethodsSupported: payload.OauthProxyServer.TokenEndpointAuthMethodsSupported,
		SecurityKeyNames:                  []string{},
		Secrets:                           secretsJSON,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to create OAuth proxy provider").Log(ctx, s.logger)
	}

	// Associate the OAuth proxy server with the toolset
	_, err = s.repo.UpdateToolsetOAuthProxyServer(ctx, repo.UpdateToolsetOAuthProxyServerParams{
		Slug:               conv.ToLower(payload.Slug),
		ProjectID:          *authCtx.ProjectID,
		OauthProxyServerID: uuid.NullUUID{UUID: oauthProxyServer.ID, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "toolset not found").Log(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "failed to associate OAuth proxy server with toolset").Log(ctx, s.logger)
	}

	toolsetDetails, err = mv.DescribeToolset(ctx, s.logger, s.db, mv.ProjectID(*authCtx.ProjectID), mv.ToolsetSlug(payload.Slug), &s.toolsetCache)
	if err != nil {
		return nil, err
	}

	return toolsetDetails, nil
}

// createToolsetVersion creates a toolset version using the tool URNs and resource URNs from the payload
func (s *Service) createToolsetVersion(ctx context.Context, toolUrnStrings []string, resourceUrnStrings []string, toolsetID uuid.UUID, tr *repo.Queries) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}
	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()), attr.SlogToolsetID(toolsetID.String()))

	// Only create a version if URNs are provided (indicating a change). Check nil (not len==0) so that toolsets can be made empty.
	if toolUrnStrings == nil && resourceUrnStrings == nil {
		return nil
	}

	// Parse tool URNs from payload
	allToolUrns := []urn.Tool{}
	for _, urnStr := range toolUrnStrings {
		var toolUrn urn.Tool
		if err := toolUrn.UnmarshalText([]byte(urnStr)); err != nil {
			logger.WarnContext(ctx, "invalid tool URN", attr.SlogError(err), attr.SlogToolURN(urnStr))
			continue
		}
		allToolUrns = append(allToolUrns, toolUrn)
	}

	// Parse resource URNs from payload
	allResourceUrns := []urn.Resource{}
	for _, urnStr := range resourceUrnStrings {
		var resourceUrn urn.Resource
		if err := resourceUrn.UnmarshalText([]byte(urnStr)); err != nil {
			logger.WarnContext(ctx, "invalid resource URN", attr.SlogError(err), attr.SlogResourceURN(urnStr))
			continue
		}
		allResourceUrns = append(allResourceUrns, resourceUrn)
	}

	// Get the latest version to set as predecessor
	latestVersion, err := tr.GetLatestToolsetVersion(ctx, toolsetID)
	latestVersionNumber := int64(0)
	var predecessorID uuid.NullUUID
	if err == nil {
		predecessorID = uuid.NullUUID{UUID: latestVersion.ID, Valid: true}
		latestVersionNumber = latestVersion.Version
	}

	if toolUrnStrings == nil && len(latestVersion.ToolUrns) > 0 {
		allToolUrns = append(allToolUrns, latestVersion.ToolUrns...)
	}

	if resourceUrnStrings == nil && len(latestVersion.ResourceUrns) > 0 {
		allResourceUrns = append(allResourceUrns, latestVersion.ResourceUrns...)
	}

	// Check if URNs are different from latest version
	if err == nil {
		toolsUnchanged := len(latestVersion.ToolUrns) == len(allToolUrns)
		if toolsUnchanged {
			existingToolUrnSet := make(map[string]bool)
			for _, existingUrn := range latestVersion.ToolUrns {
				existingToolUrnSet[existingUrn.String()] = true
			}
			for _, newUrn := range allToolUrns {
				if !existingToolUrnSet[newUrn.String()] {
					toolsUnchanged = false
					break
				}
			}
		}

		resourcesUnchanged := len(latestVersion.ResourceUrns) == len(allResourceUrns)
		if resourcesUnchanged {
			existingResourceUrnSet := make(map[string]bool)
			for _, existingUrn := range latestVersion.ResourceUrns {
				existingResourceUrnSet[existingUrn.String()] = true
			}
			for _, newUrn := range allResourceUrns {
				if !existingResourceUrnSet[newUrn.String()] {
					resourcesUnchanged = false
					break
				}
			}
		}

		if toolsUnchanged && resourcesUnchanged {
			return nil // No change needed
		}
	}

	_, err = tr.CreateToolsetVersion(ctx, repo.CreateToolsetVersionParams{
		ToolsetID:     toolsetID,
		Version:       latestVersionNumber + 1,
		ToolUrns:      allToolUrns,
		ResourceUrns:  allResourceUrns,
		PredecessorID: predecessorID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to create toolset version").Log(ctx, logger)
	}

	return nil
}

// updatePromptTemplates updates the prompt templates for a toolset. NOTE: promptTemplates are NOT tools! These correspond to actual "prompts" in MCP
func (s *Service) updatePromptTemplates(ctx context.Context, dbtx pgx.Tx, projectID uuid.UUID, toolsetID uuid.UUID, promptTemplateNames []string, logger *slog.Logger) error {
	tr := repo.New(dbtx)
	tplr := tplRepo.New(dbtx)

	ptrows, err := tplr.PeekTemplatesByNames(ctx, tplRepo.PeekTemplatesByNamesParams{
		ProjectID: projectID,
		Names:     promptTemplateNames,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error validating prompt templates").Log(ctx, logger)
	}

	err = tr.ClearToolsetPromptTemplates(ctx, repo.ClearToolsetPromptTemplatesParams{
		ProjectID: projectID,
		ToolsetID: toolsetID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error resetting prompt templates for toolset").Log(ctx, logger)
	}

	additions := make([]repo.AddToolsetPromptTemplatesParams, 0, len(ptrows))
	for _, ptrow := range ptrows {
		additions = append(additions, repo.AddToolsetPromptTemplatesParams{
			ProjectID:        projectID,
			ToolsetID:        toolsetID,
			PromptHistoryID:  ptrow.HistoryID,
			PromptTemplateID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			PromptName:       ptrow.Name,
		})
	}

	_, err = tr.AddToolsetPromptTemplates(ctx, additions)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error adding prompt templates to toolset").Log(ctx, logger)
	}

	return nil
}

// InvalidateCacheByTool invalidates cache entries for all toolsets that contain the specified tool in their latest version
func (s *Service) InvalidateCacheByTool(ctx context.Context, toolURN urn.Tool, projectID uuid.UUID) error {
	logger := s.logger.With(attr.SlogProjectID(projectID.String()), attr.SlogToolURN(toolURN.String()))

	dr := deploymentsRepo.New(s.db)

	// Get the latest deployment ID for this project
	deploymentID, err := dr.GetActiveDeploymentID(ctx, projectID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to get latest deployment").Log(ctx, logger)
	}

	// Look up all toolsets that contain this tool in their latest version
	toolsets, err := s.repo.GetToolsetsByToolURN(ctx, repo.GetToolsetsByToolURNParams{
		ProjectID: projectID,
		ToolUrn:   toolURN.String(),
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to get toolsets by tool URN").Log(ctx, logger)
	}

	// For each toolset, invalidate its cache entry using the version from the query result
	for _, toolset := range toolsets {
		cacheKey := mv.ToolsetCacheKey(toolset.ID.String(), deploymentID.String(), toolset.LatestVersion)
		if err := s.toolsetCache.DeleteByKey(ctx, cacheKey); err != nil {
			logger.WarnContext(ctx, "failed to invalidate cache entry",
				attr.SlogError(err),
				attr.SlogToolsetID(toolset.ID.String()),
				attr.SlogCacheKey(cacheKey))
		} else {
			logger.InfoContext(ctx, "invalidated toolset cache entry",
				attr.SlogToolsetID(toolset.ID.String()),
				attr.SlogCacheKey(cacheKey))
		}
	}

	return nil
}
