package mcpmetadata

import (
	"context"
	"fmt"
	"maps"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

// providedByUser marks an environment variable each person supplies as a
// header. Plugin packages list these for public servers and refuse to render a
// server that needs one into the shared Agent Plugins package, so they are the
// only metadata a package reflects.
const providedByUser = "user"

// userHeadersChanged reports whether a metadata write changed the user-supplied
// header variables, keyed by variable name with their display names.
func userHeadersChanged(before, after *types.McpMetadata) bool {
	return !maps.Equal(userHeaders(before), userHeaders(after))
}

func userHeaders(metadata *types.McpMetadata) map[string]string {
	headers := map[string]string{}
	if metadata == nil {
		return headers
	}
	for _, config := range metadata.EnvironmentConfigs {
		if config == nil || config.ProvidedBy != providedByUser {
			continue
		}
		displayName := ""
		if config.HeaderDisplayName != nil {
			displayName = *config.HeaderDisplayName
		}
		headers[config.VariableName] = displayName
	}
	return headers
}

func hasPluginMembershipForBackend(ctx context.Context, queries *pluginsrepo.Queries, authCtx *contextvalues.AuthContext, backend *resolvedMetadataBackend) (bool, error) {
	if backend.toolset != nil {
		attached, err := queries.HasPluginMembershipForToolset(ctx, pluginsrepo.HasPluginMembershipForToolsetParams{ProjectID: *authCtx.ProjectID, ToolsetID: backend.toolset.ID})
		if err != nil {
			return false, fmt.Errorf("check toolset plugin membership: %w", err)
		}
		return attached, nil
	}
	attached, err := queries.HasPluginMembershipForMCPServer(ctx, pluginsrepo.HasPluginMembershipForMCPServerParams{ProjectID: *authCtx.ProjectID, McpServerID: backend.mcpServer.ID})
	if err != nil {
		return false, fmt.Errorf("check MCP server plugin membership: %w", err)
	}
	return attached, nil
}

// requestPluginPublicationForBackend records a durable package refresh in the
// metadata transaction when a plugin carries the metadata's server.
func (s *Service) requestPluginPublicationForBackend(ctx context.Context, tx pgx.Tx, authCtx *contextvalues.AuthContext, backend *resolvedMetadataBackend) error {
	if !s.publicationRequests.Enabled {
		return nil
	}
	attached, err := hasPluginMembershipForBackend(ctx, pluginsrepo.New(tx), authCtx, backend)
	if err != nil {
		return err
	}
	if !attached {
		return nil
	}
	if err := s.publicationRequests.Project(ctx, tx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID); err != nil {
		return fmt.Errorf("request plugin publication: %w", err)
	}
	return nil
}

// publishPluginsForBackend signals the debounced project publisher after a
// committed metadata write that changes what a package carrying the server
// renders. Projects without a marketplace and servers no plugin carries signal
// nothing. Best-effort: failures are logged, and the periodic publisher sweep
// remains the safety net.
func (s *Service) publishPluginsForBackend(ctx context.Context, authCtx *contextvalues.AuthContext, backend *resolvedMetadataBackend) {
	if s.publisher == nil {
		return
	}
	queries := pluginsrepo.New(s.db)
	connected, err := queries.HasPluginGithubConnectionForProject(ctx, *authCtx.ProjectID)
	if err != nil {
		s.logger.WarnContext(ctx, "check marketplace connection after MCP metadata update", attr.SlogError(err))
		return
	}
	if !connected {
		return
	}
	attached, err := hasPluginMembershipForBackend(ctx, queries, authCtx, backend)
	if err != nil {
		s.logger.WarnContext(ctx, "check plugin membership after MCP metadata update", attr.SlogError(err))
		return
	}
	if !attached {
		return
	}
	// The request returning shouldn't drop the enqueue.
	if err := s.publisher.SignalPluginPublish(context.WithoutCancel(ctx), *authCtx.ProjectID, authCtx.UserID); err != nil {
		s.logger.WarnContext(ctx, "signal plugin publish after MCP metadata update",
			attr.SlogProjectID(authCtx.ProjectID.String()), attr.SlogError(err))
	}
}
