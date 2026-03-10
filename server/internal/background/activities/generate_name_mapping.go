package activities

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

const (
	nameMappingPrefix = "hooks:name_mapping:"
	nameMappingTTL    = 365 * 24 * time.Hour // 1 year
)

// NameMappingCacheImpl implements the NameMappingCache interface
type NameMappingCacheImpl struct {
	cache cache.Cache
}

// NewNameMappingCache creates a new NameMappingCache implementation
func NewNameMappingCache(cache cache.Cache) *NameMappingCacheImpl {
	return &NameMappingCacheImpl{cache: cache}
}

// Get retrieves a name mapping from cache. Returns empty string if not found.
func (n *NameMappingCacheImpl) Get(ctx context.Context, serverName string) (string, error) {
	key := nameMappingKey(serverName)
	var mappedName string
	err := n.cache.Get(ctx, key, &mappedName)
	if err != nil {
		return "", nil // Not found is not an error
	}
	return mappedName, nil
}

// Save stores a name mapping in cache
func (n *NameMappingCacheImpl) Save(ctx context.Context, serverName, mappedName string) error {
	key := nameMappingKey(serverName)
	if err := n.cache.Set(ctx, key, mappedName, nameMappingTTL); err != nil {
		return fmt.Errorf("set cache key: %w", err)
	}
	return nil
}

// nameMappingKey generates the cache key for a server name mapping
func nameMappingKey(serverName string) string {
	return fmt.Sprintf("%s%s", nameMappingPrefix, serverName)
}

// GenerateMappingWithLLM calls the LLM to generate a human-readable name mapping
// for a server based on tool call attributes. Returns empty string if LLM cannot
// confidently generate a mapping.
func GenerateMappingWithLLM(
	ctx context.Context,
	completionsClient openrouter.CompletionClient,
	serverName string,
	toolCallAttrs map[string]any,
	orgID string,
	projectID string,
) (string, error) {
	// Marshal tool call data for LLM context
	toolCallData, err := json.Marshal(toolCallAttrs)
	if err != nil {
		return "", fmt.Errorf("marshal tool call data: %w", err)
	}

	// Call LLM to generate mapping
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
				Return the name only, no other text. This will usually be exactly one word.

Original server name: %s

Tool call data: %s`, serverName, toolCallData)),
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

	completion, err := completionsClient.GetCompletion(ctx, completionReq)
	if err != nil {
		return "", fmt.Errorf("get completion: %w", err)
	}

	mappedName := openrouter.GetText(*completion.Message)
	return mappedName, nil
}

type GenerateNameMapping struct {
	logger            *slog.Logger
	nameMappingCache  *NameMappingCacheImpl
	completionsClient openrouter.CompletionClient
}

func NewGenerateNameMapping(logger *slog.Logger, cache cache.Cache, completionsClient openrouter.CompletionClient) *GenerateNameMapping {
	return &GenerateNameMapping{
		logger:            logger,
		nameMappingCache:  NewNameMappingCache(cache),
		completionsClient: completionsClient,
	}
}

type GenerateNameMappingArgs struct {
	ServerName    string
	ToolCallAttrs map[string]any
	OrgID         string
	ProjectID     string
}

type GenerateNameMappingResult struct {
	OriginalName string
	MappedName   string
}

func (g *GenerateNameMapping) Do(ctx context.Context, args GenerateNameMappingArgs) (*GenerateNameMappingResult, error) {
	// Check cache first to avoid redundant LLM calls
	cachedName, err := g.nameMappingCache.Get(ctx, args.ServerName)
	if err != nil {
		return nil, fmt.Errorf("get from cache: %w", err)
	}
	if cachedName != "" {
		g.logger.DebugContext(ctx, fmt.Sprintf("name mapping already cached: server_name=%s mapped_name=%s", args.ServerName, cachedName))
		return &GenerateNameMappingResult{
			OriginalName: args.ServerName,
			MappedName:   cachedName,
		}, nil
	}

	// Call LLM to generate mapping
	mappedName, err := GenerateMappingWithLLM(
		ctx,
		g.completionsClient,
		args.ServerName,
		args.ToolCallAttrs,
		args.OrgID,
		args.ProjectID,
	)
	if err != nil {
		return nil, fmt.Errorf("generate mapping with LLM: %w", err)
	}

	if mappedName == "" {
		g.logger.InfoContext(ctx, fmt.Sprintf("LLM returned empty mapping for server: server_name=%s", args.ServerName))
		return &GenerateNameMappingResult{
			OriginalName: args.ServerName,
			MappedName:   "",
		}, nil
	}

	// Store mapping in cache
	if err := g.nameMappingCache.Save(ctx, args.ServerName, mappedName); err != nil {
		return nil, fmt.Errorf("save to cache: %w", err)
	}

	g.logger.InfoContext(ctx, fmt.Sprintf("generated name mapping: server_name=%s mapped_name=%s", args.ServerName, mappedName))

	return &GenerateNameMappingResult{
		OriginalName: args.ServerName,
		MappedName:   mappedName,
	}, nil
}
