package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

const (
	nameMappingPrefix = "hooks:name_mapping:"
	nameMappingTTL    = 365 * 24 * time.Hour // 1 year (effectively permanent for this use case)
)

// NameMapperImpl implements the NameMapper interface using Redis for storage
type NameMapperImpl struct {
	cache             cache.Cache
	completionsClient openrouter.CompletionClient
	logger            *slog.Logger
}

// NewNameMapper creates a new NameMapper instance
func NewNameMapper(cache cache.Cache, completionsClient openrouter.CompletionClient, logger *slog.Logger) *NameMapperImpl {
	return &NameMapperImpl{
		cache:             cache,
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

	// Try to get existing mapping from Redis
	key := nameMappingKey(serverName)
	var mappedName string
	err := n.cache.Get(ctx, key, &mappedName)
	if err == nil && mappedName != "" {
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

	toolCallData, err := json.Marshal(attrs)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal tool call data: %w", err)
	}

	temperature := 0.0
	completionReq := openrouter.CompletionRequest{
		OrgID:       orgID,
		ProjectID:   projectID,
		Model:       "openai/gpt-4o-mini",
		UsageSource: billing.ModelUsageSourceGram,
		Messages: []or.Message{
			or.CreateMessageUser(or.UserMessage{
				Content: or.CreateUserMessageContentStr(fmt.Sprintf(`The following is tool call data from a hook event. The name of the server might not be accurate. 
				Return a human-readable name based on the available information, for example 'Linear' if the tool call appears to be for Linear. 
				You must be 100%% confident in the mapping. If there is not enough information to make a reliable mapping, return an empty string.
				Return the name only, no other text. This will usually be exactly one word.\n\nOriginal server name: %s\n\nTool call data: %s`, serverName, toolCallData)),
				Name: nil,
			}),
		},
		Tools:          []openrouter.Tool{},
		Temperature:    &temperature,
		Stream:         false,
		ChatID:         uuid.Nil,
		UserID:         "",
		ExternalUserID: "",
		APIKeyID:       "",
		HTTPMetadata:   nil,
		JSONSchema:     nil,
	}

	completion, err := n.completionsClient.GetCompletion(ctx, completionReq)
	if err != nil {
		return nil, fmt.Errorf("failed to get completion: %w", err)
	}

	mappedName = openrouter.GetText(*completion.Message)
	if mappedName == "" {
		return nil, nil
	}

	if err := n.cache.Set(ctx, key, mappedName, nameMappingTTL); err != nil {
		return &mappedName, fmt.Errorf("failed to set name mapping in cache: %w", err)
	}

	return &mappedName, nil
}

// nameMappingKey generates the Redis key for a server name mapping
func nameMappingKey(serverName string) string {
	return fmt.Sprintf("%s%s", nameMappingPrefix, serverName)
}
