package toolsets

import (
	"context"
	"errors"
	"log/slog"
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
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	domainsRepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	environmentsRepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	tplRepo "github.com/speakeasy-api/gram/server/internal/templates/repo"
	"github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

type Service struct {
	tracer          trace.Tracer
	logger          *slog.Logger
	db              *pgxpool.Pool
	repo            *repo.Queries
	environmentRepo *environmentsRepo.Queries
	auth            *auth.Auth
	toolsets        *Toolsets
	domainsRepo     *domainsRepo.Queries
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager) *Service {
	return &Service{
		tracer:          otel.Tracer("github.com/speakeasy-api/gram/server/internal/toolsets"),
		logger:          logger,
		db:              db,
		repo:            repo.New(db),
		auth:            auth.New(logger, db, sessions),
		environmentRepo: environmentsRepo.New(db),
		toolsets:        NewToolsets(db),
		domainsRepo:     domainsRepo.New(db),
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

	createToolParams := repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   payload.Name,
		Slug:                   conv.ToSlug(payload.Name),
		Description:            conv.PtrToPGText(payload.Description),
		DefaultEnvironmentSlug: conv.PtrToPGText(nil),
		HttpToolNames:          payload.HTTPToolNames,
		McpSlug:                conv.ToPGText(mcpSlug),
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

	toolsetDetails, err := mv.DescribeToolset(ctx, s.logger, s.db, mv.ProjectID(*authCtx.ProjectID), mv.ToolsetSlug(createdToolset.Slug))
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

	result := make([]*types.Toolset, len(toolsets))
	for i, toolset := range toolsets {
		toolsetDetails, err := mv.DescribeToolset(ctx, s.logger, s.db, mv.ProjectID(*authCtx.ProjectID), mv.ToolsetSlug(toolset.Slug))
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

	logger := s.logger.With(slog.String("project_id", authCtx.ProjectID.String()), slog.String("toolset_slug", string(payload.Slug)))

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to begin transaction").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	tr := s.repo.WithTx(dbtx)
	tplr := tplRepo.New(dbtx)

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
		HttpToolNames:          existingToolset.HttpToolNames,
		McpSlug:                existingToolset.McpSlug,
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
	if domain, err := s.domainsRepo.GetCustomDomainsByOrganization(ctx, authCtx.ActiveOrganizationID); err == nil && domain.Activated && domain.Verified {
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
				return nil, oops.E(oops.CodeConflict, nil, "this slug is already tken")
			}
			updateParams.McpSlug = conv.ToPGText(conv.ToLower(*payload.McpSlug))
		} else {
			mcpToolset, mcpToolsetErr := tr.GetToolsetByMcpSlugAndCustomDomain(ctx, repo.GetToolsetByMcpSlugAndCustomDomainParams{
				McpSlug:        conv.ToPGText(conv.ToLower(*payload.McpSlug)),
				CustomDomainID: uuid.NullUUID{UUID: uuid.MustParse(*toolsetDomainID), Valid: true},
			})
			if mcpToolsetErr == nil && mcpToolset.ID != existingToolset.ID {
				return nil, oops.E(oops.CodeConflict, nil, "this slug is already tken")
			}
			updateParams.McpSlug = conv.ToPGText(conv.ToLower(*payload.McpSlug))
		}
	}

	if payload.McpIsPublic != nil {
		if *payload.McpIsPublic && !existingToolset.McpSlug.Valid && (payload.McpSlug == nil || *payload.McpSlug == "") {
			// sanity check this should not be able to happens
			return nil, oops.E(oops.CodeBadRequest, nil, "mcp slug is required to set mcp is public")
		}
		updateParams.McpIsPublic = *payload.McpIsPublic
	}

	// Convert set back to slice
	if payload.HTTPToolNames != nil {
		updateParams.HttpToolNames = make([]string, 0, len(payload.HTTPToolNames))
		updateParams.HttpToolNames = append(updateParams.HttpToolNames, payload.HTTPToolNames...)
	}

	updatedToolset, err := tr.UpdateToolset(ctx, updateParams)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error updating toolset").Log(ctx, logger)
	}

	if payload.PromptTemplateNames != nil {
		ptrows, err := tplr.PeekTemplatesByNames(ctx, tplRepo.PeekTemplatesByNamesParams{
			ProjectID: *authCtx.ProjectID,
			Names:     payload.PromptTemplateNames,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error validating prompt templates").Log(ctx, logger)
		}

		err = tr.ClearToolsetPromptTemplates(ctx, repo.ClearToolsetPromptTemplatesParams{
			ProjectID: *authCtx.ProjectID,
			ToolsetID: existingToolset.ID,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error resetting prompt templates for toolset").Log(ctx, logger)
		}

		additions := make([]repo.AddToolsetPromptTemplatesParams, 0, len(ptrows))
		for _, ptrow := range ptrows {
			additions = append(additions, repo.AddToolsetPromptTemplatesParams{
				ProjectID:        *authCtx.ProjectID,
				ToolsetID:        existingToolset.ID,
				PromptHistoryID:  ptrow.HistoryID,
				PromptTemplateID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
				PromptName:       ptrow.Name,
			})
		}

		_, err = tr.AddToolsetPromptTemplates(ctx, additions)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error adding prompt templates to toolset").Log(ctx, logger)
		}
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving updated toolset").Log(ctx, logger)
	}

	toolsetDetails, err := mv.DescribeToolset(ctx, logger, s.db, mv.ProjectID(*authCtx.ProjectID), mv.ToolsetSlug(updatedToolset.Slug))
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

	return mv.DescribeToolset(ctx, s.logger, s.db, mv.ProjectID(*authCtx.ProjectID), mv.ToolsetSlug(payload.Slug))
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) CheckMCPSlugAvailability(ctx context.Context, payload *gen.CheckMCPSlugAvailabilityPayload) (bool, error) {
	//nolint:wrapcheck // Wrapping adds no value here
	return s.repo.CheckMCPSlugAvailability(ctx, conv.ToPGText(conv.ToLower(payload.Slug)))
}
