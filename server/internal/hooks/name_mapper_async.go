package hooks

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/cache"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

// AsyncNameMapper implements the NameMapper interface using Temporal workflows
// for asynchronous processing. It checks the cache for existing mappings and
// triggers background workflows to generate new mappings.
type AsyncNameMapper struct {
	nameMappingCache NameMappingCache
	temporalEnv      *tenv.Environment
	logger           *slog.Logger
}

// NewAsyncNameMapper creates a new AsyncNameMapper instance
func NewAsyncNameMapper(cache cache.Cache, temporalEnv *tenv.Environment, logger *slog.Logger) *AsyncNameMapper {
	return &AsyncNameMapper{
		nameMappingCache: activities.NewNameMappingCache(cache),
		temporalEnv:      temporalEnv,
		logger:           logger,
	}
}

// GetMappedName retrieves a name mapping from cache if it exists.
// If no mapping exists, it returns nil and triggers an async workflow
// to generate the mapping in the background.
//
// This is necessary because:
// - in Cowork, the source shows up as a UUID, so we need to map it to a human-readable name
// - in other environments, the source might show up like "claude_ai_Linear"
// - people might use an MCP server they've named "my_mcp_server" which is actually Linear
func (n *AsyncNameMapper) GetMappedName(attrs map[attr.Key]any) (*string, error) {
	ctx := context.Background()

	serverName, ok := attrs[attr.ToolCallSourceKey].(string)
	if !ok {
		return nil, fmt.Errorf("tool call source is not a string")
	}

	// Try to get existing mapping from cache
	mappedName, err := n.nameMappingCache.Get(ctx, serverName)
	if err != nil {
		return nil, fmt.Errorf("get from cache: %w", err)
	}
	if mappedName != "" {
		// Mapping exists, return it
		return &mappedName, nil
	}

	// No mapping exists - trigger async workflow to generate it
	// This is fire-and-forget; future requests will benefit from the cached result
	if n.temporalEnv != nil {
		orgID, ok := attrs[attr.OrganizationIDKey].(string)
		if !ok {
			return nil, fmt.Errorf("organization ID is not a string")
		}
		projectID, ok := attrs[attr.ProjectIDKey].(string)
		if !ok {
			return nil, fmt.Errorf("project ID is not a string")
		}

		// Launch workflow asynchronously (don't block on result)
		toolCallAttrs := convertAttrsToMap(attrs)
		go func() {
			bgCtx := context.Background()
			params := background.ProcessNameMappingWorkflowParams{
				ServerName:    serverName,
				OrgID:         orgID,
				ProjectID:     projectID,
				ToolCallAttrs: toolCallAttrs,
			}

			_, err := background.ExecuteProcessNameMappingWorkflow(bgCtx, n.temporalEnv, params)
			if err != nil {
				n.logger.WarnContext(bgCtx, fmt.Sprintf("failed to start name mapping workflow: server_name=%s err=%v", serverName, err))
			}
		}()
	}

	// Return nil - the mapping will be available on future requests
	return nil, nil
}
