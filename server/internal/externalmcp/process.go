package externalmcp

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	slogmulti "github.com/samber/slog-multi"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/deployments/events"
	"github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type ToolExtractor struct {
	logger         *slog.Logger
	db             *pgxpool.Pool
	registryClient *RegistryClient
	repo           *repo.Queries
}

func NewToolExtractor(
	logger *slog.Logger,
	db *pgxpool.Pool,
	registryClient *RegistryClient,
) *ToolExtractor {
	return &ToolExtractor{
		logger:         logger,
		db:             db,
		registryClient: registryClient,
		repo:           repo.New(db),
	}
}

type ToolExtractorTaskMCPServer struct {
	AttachmentID            uuid.UUID
	RegistryID              uuid.UUID
	Name                    string
	Slug                    string
	RegistryServerSpecifier string
}

type ToolExtractorTask struct {
	OrgSlug      string
	ProjectSlug  string
	ProjectID    uuid.UUID
	DeploymentID uuid.UUID
	MCP          ToolExtractorTaskMCPServer
}

func (te *ToolExtractor) Do(ctx context.Context, task ToolExtractorTask) error {
	slogArgs := []any{
		attr.SlogProjectID(task.ProjectID.String()),
		attr.SlogDeploymentID(task.DeploymentID.String()),
		attr.SlogProjectSlug(task.ProjectSlug),
		attr.SlogOrganizationSlug(task.OrgSlug),
		attr.SlogExternalMCPID(task.MCP.AttachmentID.String()),
		attr.SlogExternalMCPSlug(task.MCP.Slug),
		attr.SlogExternalMCPName(task.MCP.Name),
		attr.SlogMCPRegistryID(task.MCP.RegistryID.String()),
	}

	internalLogger := te.logger.With(slogArgs...)

	eventsHandler := events.NewLogHandler()
	logger := slog.New(slogmulti.Fanout(
		te.logger.Handler(),
		eventsHandler,
	)).With(slogArgs...)

	defer func() {
		if _, err := eventsHandler.Flush(ctx, te.db); err != nil {
			te.logger.ErrorContext(
				ctx,
				"failed to flush deployment events",
				append(slogArgs, attr.SlogError(err))...,
			)
		}
	}()

	logger.InfoContext(ctx, fmt.Sprintf("[%s] processing external mcp server", task.MCP.Name))

	registry, err := te.repo.GetMCPRegistryByID(ctx, task.MCP.RegistryID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "[%s] error getting registry for mcp server", task.MCP.Name).Log(ctx, logger)
	}

	serverDetails, err := te.registryClient.GetServerDetails(ctx, Registry{
		ID:  registry.ID,
		URL: registry.Url,
	}, task.MCP.RegistryServerSpecifier)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "[%s] error fetching server details from registry", task.MCP.Name).Log(ctx, logger)
	}

	var requiresOAuth bool
	var oauthDiscovery *OAuthDiscoveryResult
	mcpClient, err := NewClient(ctx, internalLogger, serverDetails.RemoteURL, serverDetails.TransportType, nil)
	if authErr, ok := IsAuthRequiredError(err); ok {
		requiresOAuth = true

		oauthDiscovery, err = DiscoverOAuthMetadata(ctx, internalLogger, authErr.WWWAuthenticate, serverDetails.RemoteURL)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "[%s] error discovering OAuth metadata", task.MCP.Name).Log(ctx, logger)
		}

		logger.InfoContext(ctx, "discovered oauth metadata",
			attr.SlogOAuthVersion(oauthDiscovery.Version),
			attr.SlogOAuthAuthorizationEndpoint(oauthDiscovery.AuthorizationEndpoint),
			attr.SlogOAuthTokenEndpoint(oauthDiscovery.TokenEndpoint),
			attr.SlogOAuthRegistrationEndpoint(oauthDiscovery.RegistrationEndpoint),
		)

		if oauthDiscovery.Version == OAuthVersion20 {
			return oops.E(oops.CodeUnexpected, oops.ErrPermanent,
				"[%s] external MCP server uses legacy OAuth 2.0 which requires static client registration; "+
					"dynamic client registration is not supported", task.MCP.Name).Log(ctx, logger)
		}
	} else if err != nil {
		return oops.E(oops.CodeUnexpected, err, "[%s] external mcp server unavailable", task.MCP.Name).Log(ctx, logger)
	} else {
		defer o11y.LogDefer(ctx, logger, mcpClient.Close)
	}

	// Build OAuth metadata params
	oauthVersion := OAuthVersionNone
	var oauthAuthEndpoint, oauthTokenEndpoint, oauthRegEndpoint pgtype.Text
	var oauthScopes []string
	if oauthDiscovery != nil {
		oauthVersion = oauthDiscovery.Version
		if oauthDiscovery.AuthorizationEndpoint != "" {
			oauthAuthEndpoint = conv.ToPGText(oauthDiscovery.AuthorizationEndpoint)
		}
		if oauthDiscovery.TokenEndpoint != "" {
			oauthTokenEndpoint = conv.ToPGText(oauthDiscovery.TokenEndpoint)
		}
		if oauthDiscovery.RegistrationEndpoint != "" {
			oauthRegEndpoint = conv.ToPGText(oauthDiscovery.RegistrationEndpoint)
		}
		oauthScopes = oauthDiscovery.ScopesSupported
	}

	// Create tool definitions based on what's available
	if len(serverDetails.Tools) > 0 {
		// Create individual tool definitions for each tool from the registry
		for _, tool := range serverDetails.Tools {
			toolURN := urn.Tool{
				Kind:   urn.ToolKindExternalMCP,
				Source: task.MCP.Slug,
				Name:   tool.Name,
			}

			_, err = te.repo.CreateExternalMCPToolDefinition(ctx, repo.CreateExternalMCPToolDefinitionParams{
				Type:                       string(types.ExternalMCPToolTypeDirect),
				Name:                       conv.ToPGText(tool.Name),
				Description:                conv.ToPGText(tool.Description),
				Schema:                     tool.InputSchema,
				ExternalMcpAttachmentID:    task.MCP.AttachmentID,
				ToolUrn:                    toolURN.String(),
				RemoteUrl:                  serverDetails.RemoteURL,
				TransportType:              serverDetails.TransportType,
				RequiresOauth:              requiresOAuth,
				OauthVersion:               oauthVersion,
				OauthAuthorizationEndpoint: oauthAuthEndpoint,
				OauthTokenEndpoint:         oauthTokenEndpoint,
				OauthRegistrationEndpoint:  oauthRegEndpoint,
				OauthScopesSupported:       oauthScopes,
			})
			if err != nil {
				return oops.E(oops.CodeUnexpected, err, "[%s] error creating external mcp tool definition for %s", task.MCP.Name, tool.Name).Log(ctx, logger)
			}
		}

		logger.InfoContext(ctx, fmt.Sprintf("[%s] created %d external mcp tool definitions", task.MCP.Name, len(serverDetails.Tools)),
			attr.SlogOAuthRequired(requiresOAuth),
			attr.SlogOAuthVersion(oauthVersion),
		)
	} else {
		// Fallback to proxy tool when no tools are defined in the registry
		toolURN := urn.Tool{
			Kind:   urn.ToolKindExternalMCP,
			Source: task.MCP.Slug,
			Name:   "proxy",
		}

		_, err = te.repo.CreateExternalMCPToolDefinition(ctx, repo.CreateExternalMCPToolDefinitionParams{
			Type:                       string(types.ExternalMCPToolTypeProxy),
			ExternalMcpAttachmentID:    task.MCP.AttachmentID,
			ToolUrn:                    toolURN.String(),
			RemoteUrl:                  serverDetails.RemoteURL,
			TransportType:              serverDetails.TransportType,
			RequiresOauth:              requiresOAuth,
			OauthVersion:               oauthVersion,
			OauthAuthorizationEndpoint: oauthAuthEndpoint,
			OauthTokenEndpoint:         oauthTokenEndpoint,
			OauthRegistrationEndpoint:  oauthRegEndpoint,
			OauthScopesSupported:       oauthScopes,
			Name:                       conv.PtrToPGTextEmpty(nil),
			Description:                conv.PtrToPGTextEmpty(nil),
			Schema:                     nil,
		})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "[%s] error creating external mcp tool definition", task.MCP.Name).Log(ctx, logger)
		}

		logger.InfoContext(ctx, fmt.Sprintf("[%s] created external mcp proxy tool", task.MCP.Name),
			attr.SlogToolURN(toolURN.String()),
			attr.SlogOAuthRequired(requiresOAuth),
			attr.SlogOAuthVersion(oauthVersion),
		)
	}

	return nil
}
