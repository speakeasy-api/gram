package externalmcp

import (
	"context"
	"encoding/json"
	"errors"
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
	if oauthErr := (*OAuthRequiredError)(nil); errors.As(err, &oauthErr) {
		requiresOAuth = true

		oauthDiscovery, err = DiscoverOAuthMetadata(ctx, internalLogger, oauthErr.WWWAuthenticate, serverDetails.RemoteURL)
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
			logger.WarnContext(ctx, fmt.Sprintf("[%s] external MCP server uses legacy OAuth 2.0 which requires static client registration; "+
				"falling back to manual Authorization header", task.MCP.Name))

			// Fall back to manual Authorization header instead of OAuth
			requiresOAuth = false
			oauthDiscovery = nil

			authDescription := "Bearer token for authentication (OAuth 2.0 requires static client registration)"
			serverDetails.Headers = append(serverDetails.Headers, RemoteHeader{
				Name:        "Authorization",
				IsSecret:    true,
				IsRequired:  true,
				Description: &authDescription,
				Placeholder: nil,
			})
		}
	} else if authErr := (*AuthRejectedError)(nil); errors.As(err, &authErr) {
		logger.InfoContext(ctx, "[%s] external MCP server rejected auth probe, continuing without OAuth",
			attr.SlogURL(serverDetails.RemoteURL),
			attr.SlogHTTPResponseStatusCode(authErr.StatusCode),
		)
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

	// Marshal header definitions from registry
	var headerDefinitions []byte
	if len(serverDetails.Headers) > 0 {
		headerDefinitions, err = json.Marshal(serverDetails.Headers)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "[%s] error marshaling header definitions", task.MCP.Name).Log(ctx, logger)
		}
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
				HeaderDefinitions:          headerDefinitions,
				Title:                      extractAnnotationString(tool.Annotations, "title"),
				ReadOnlyHint:               extractAnnotationBool(tool.Annotations, "readOnlyHint"),
				DestructiveHint:            extractAnnotationBool(tool.Annotations, "destructiveHint"),
				IdempotentHint:             extractAnnotationBool(tool.Annotations, "idempotentHint"),
				OpenWorldHint:              extractAnnotationBool(tool.Annotations, "openWorldHint"),
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
			HeaderDefinitions:          headerDefinitions,
			Name:                       conv.PtrToPGTextEmpty(nil),
			Description:                conv.PtrToPGTextEmpty(nil),
			Schema:                     nil,
			Title:                      pgtype.Text{String: "", Valid: false},
			ReadOnlyHint:               pgtype.Bool{Bool: false, Valid: false},
			DestructiveHint:            pgtype.Bool{Bool: false, Valid: false},
			IdempotentHint:             pgtype.Bool{Bool: false, Valid: false},
			OpenWorldHint:              pgtype.Bool{Bool: false, Valid: false},
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

// extractAnnotationBool extracts a boolean annotation value from the MCP tool annotations map.
// Returns an invalid pgtype.Bool when the key is missing or not a bool.
func extractAnnotationBool(annotations map[string]any, key string) pgtype.Bool {
	if annotations == nil {
		return pgtype.Bool{Bool: false, Valid: false}
	}
	v, ok := annotations[key]
	if !ok {
		return pgtype.Bool{Bool: false, Valid: false}
	}
	b, ok := v.(bool)
	if !ok {
		return pgtype.Bool{Bool: false, Valid: false}
	}
	return pgtype.Bool{Bool: b, Valid: true}
}

// extractAnnotationString extracts a string annotation value from the MCP tool annotations map.
// Returns an invalid pgtype.Text when the key is missing, not a string, or empty.
func extractAnnotationString(annotations map[string]any, key string) pgtype.Text {
	if annotations == nil {
		return pgtype.Text{String: "", Valid: false}
	}
	v, ok := annotations[key]
	if !ok {
		return pgtype.Text{String: "", Valid: false}
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return pgtype.Text{String: "", Valid: false}
	}
	return pgtype.Text{String: s, Valid: true}
}
