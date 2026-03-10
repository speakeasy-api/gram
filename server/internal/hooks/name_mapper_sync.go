package hooks

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// NameMapperImpl implements the NameMapper interface using Redis for storage
type NameMapperImpl struct {
	nameMappingCache  NameMappingCache
	completionsClient openrouter.CompletionClient
	logger            *slog.Logger
}

// NewNameMapper creates a new NameMapper instance
func NewNameMapper(cache cache.Cache, completionsClient openrouter.CompletionClient, logger *slog.Logger) *NameMapperImpl {
	return &NameMapperImpl{
		nameMappingCache:  activities.NewNameMappingCache(cache),
		completionsClient: completionsClient,
		logger:            logger,
	}
}

// GetMappedName retrieves or creates a name mapping for the server
// This is necessary because:
// - in Cowork, the source shows up as a UUID, so we need to map it to a human-readable name
// - in other environments, the source might show up like "claude_ai_Linear"
// - people might use an MCP server they've named "my_mcp_server" which is actually Linear
func (n *NameMapperImpl) GetMappedName(attrs map[attr.Key]any) (*string, error) {
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

	orgID, ok := attrs[attr.OrganizationIDKey].(string)
	if !ok {
		return nil, fmt.Errorf("organization ID is not a string")
	}
	projectID, ok := attrs[attr.ProjectIDKey].(string)
	if !ok {
		return nil, fmt.Errorf("project ID is not a string")
	}

	// Convert attrs to map[string]any for the shared function
	toolCallAttrs := convertAttrsToMap(attrs)

	// Call shared LLM generation function
	mappedName, err = activities.GenerateMappingWithLLM(
		ctx,
		n.completionsClient,
		serverName,
		toolCallAttrs,
		orgID,
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("generate mapping with LLM: %w", err)
	}

	if mappedName == "" {
		return nil, nil
	}

	if err := n.nameMappingCache.Save(ctx, serverName, mappedName); err != nil {
		return &mappedName, fmt.Errorf("failed to save name mapping to cache: %w", err)
	}

	return &mappedName, nil
}
