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

type GenerateNameMapping struct {
	logger            *slog.Logger
	cache             cache.Cache
	completionsClient openrouter.CompletionClient
}

func NewGenerateNameMapping(logger *slog.Logger, cache cache.Cache, completionsClient openrouter.CompletionClient) *GenerateNameMapping {
	return &GenerateNameMapping{
		logger:            logger,
		cache:             cache,
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
	key := nameMappingKey(args.ServerName)
	var cachedName string
	err := g.cache.Get(ctx, key, &cachedName)
	if err == nil && cachedName != "" {
		g.logger.DebugContext(ctx, "name mapping already cached",
			slog.String("server_name", args.ServerName),
			slog.String("mapped_name", cachedName),
		)
		return &GenerateNameMappingResult{
			OriginalName: args.ServerName,
			MappedName:   cachedName,
		}, nil
	}

	// Marshal tool call data for LLM context
	toolCallData, err := json.Marshal(args.ToolCallAttrs)
	if err != nil {
		return nil, fmt.Errorf("marshal tool call data: %w", err)
	}

	// Call LLM to generate mapping
	temperature := 0.0
	completionReq := openrouter.CompletionRequest{
		OrgID:       args.OrgID,
		ProjectID:   args.ProjectID,
		Model:       "openai/gpt-4o-mini",
		UsageSource: billing.ModelUsageSourceGram,
		Messages: []or.Message{
			or.CreateMessageUser(or.UserMessage{
				Content: or.CreateUserMessageContentStr(fmt.Sprintf(`The following is tool call data from a hook event. The name of the server might not be accurate.
				Return a human-readable name based on the available information, for example 'Linear' if the tool call appears to be for Linear.
				You must be 100%% confident in the mapping. If there is not enough information to make a reliable mapping, return an empty string.
				Return the name only, no other text. This will usually be exactly one word.

Original server name: %s

Tool call data: %s`, args.ServerName, toolCallData)),
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

	completion, err := g.completionsClient.GetCompletion(ctx, completionReq)
	if err != nil {
		return nil, fmt.Errorf("get completion: %w", err)
	}

	mappedName := openrouter.GetText(*completion.Message)
	if mappedName == "" {
		g.logger.InfoContext(ctx, "LLM returned empty mapping for server",
			slog.String("server_name", args.ServerName),
		)
		return &GenerateNameMappingResult{
			OriginalName: args.ServerName,
			MappedName:   "",
		}, nil
	}

	// Store mapping in cache
	if err := g.cache.Set(ctx, key, mappedName, nameMappingTTL); err != nil {
		return nil, fmt.Errorf("set name mapping in cache: %w", err)
	}

	g.logger.InfoContext(ctx, "generated name mapping",
		slog.String("server_name", args.ServerName),
		slog.String("mapped_name", mappedName),
	)

	return &GenerateNameMappingResult{
		OriginalName: args.ServerName,
		MappedName:   mappedName,
	}, nil
}

func nameMappingKey(serverName string) string {
	return fmt.Sprintf("%s%s", nameMappingPrefix, serverName)
}
