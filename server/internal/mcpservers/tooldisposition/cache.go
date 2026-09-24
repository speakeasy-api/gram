package tooldisposition

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
)

const ttl = 15 * time.Minute

type serverTools struct {
	MCPServerID  string                            `json:"mcp_server_id"`
	Dispositions map[string]string                 `json:"dispositions"`
	Annotations  map[string]*types.ToolAnnotations `json:"annotations"`
}

var _ cache.CacheableObject[serverTools] = (*serverTools)(nil)

func cacheKey(mcpServerID string) string {
	return fmt.Sprintf("mcpservers:tool_disposition:v2:%s", mcpServerID)
}

func (s serverTools) CacheKey() string {
	return cacheKey(s.MCPServerID)
}

func (s serverTools) AdditionalCacheKeys() []string {
	return []string{}
}

func (s serverTools) TTL() time.Duration {
	return ttl
}

// Cache resolves effective tool annotations and dispositions through a pull-through cache.
type Cache struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	cache  cache.TypedCacheObject[serverTools]
}

// New creates a tool annotation cache.
func New(logger *slog.Logger, db *pgxpool.Pool, c cache.Cache) *Cache {
	logger = logger.With(attr.SlogComponent("tool-disposition"))
	return &Cache{
		logger: logger,
		db:     db,
		cache:  cache.NewTypedObjectCache[serverTools](logger.With(attr.SlogCacheNamespace("tool-disposition")), c, cache.SuffixNone),
	}
}

// Dispositions returns the disposition token for every classified tool on a server.
func (c *Cache) Dispositions(ctx context.Context, mcpServerID, projectID string) (map[string]string, error) {
	serverUUID, err := uuid.Parse(mcpServerID)
	if err != nil {
		return nil, fmt.Errorf("parse mcp server id: %w", err)
	}
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("parse project id: %w", err)
	}

	resolved, err := c.resolve(ctx, serverUUID, projectUUID)
	if err != nil {
		return nil, err
	}
	return resolved.Dispositions, nil
}

// ToolAnnotations returns the effective hints for one called tool.
func (c *Cache) ToolAnnotations(ctx context.Context, mcpServerID, projectID uuid.UUID, toolName string) (*types.ToolAnnotations, error) {
	resolved, err := c.resolve(ctx, mcpServerID, projectID)
	if err != nil {
		return nil, err
	}
	return resolved.Annotations[toolName], nil
}

func (c *Cache) resolve(ctx context.Context, mcpServerID, projectID uuid.UUID) (serverTools, error) {
	serverID := mcpServerID.String()
	if cached, err := c.cache.Get(ctx, cacheKey(serverID)); err == nil {
		return cached, nil
	}

	rows, err := repo.New(c.db).ListEffectiveMCPServerToolAnnotations(ctx, repo.ListEffectiveMCPServerToolAnnotationsParams{
		McpServerID: mcpServerID,
		ProjectID:   projectID,
	})
	if err != nil {
		return serverTools{}, fmt.Errorf("list effective MCP tool annotations: %w", err)
	}

	resolved := serverTools{
		MCPServerID:  serverID,
		Dispositions: make(map[string]string, len(rows)),
		Annotations:  make(map[string]*types.ToolAnnotations, len(rows)),
	}
	for _, row := range rows {
		annotations := conv.AnnotationsFromColumns(
			row.ReadOnlyHint,
			row.DestructiveHint,
			row.IdempotentHint,
			row.OpenWorldHint,
		)
		resolved.Annotations[row.ToolName] = annotations
		if disposition := conv.DispositionFromAnnotations(annotations); disposition != "" {
			resolved.Dispositions[row.ToolName] = disposition
		}
	}

	if err := c.cache.Store(ctx, resolved); err != nil {
		c.logger.WarnContext(ctx, "cache MCP tool annotations",
			attr.SlogError(err),
			attr.SlogMcpServerID(serverID),
		)
	}

	return resolved, nil
}

// Invalidate evicts a server's cached annotations.
func (c *Cache) Invalidate(ctx context.Context, mcpServerID string) error {
	if err := c.cache.DeleteByKey(ctx, cacheKey(mcpServerID)); err != nil {
		return fmt.Errorf("invalidate tool dispositions: %w", err)
	}
	return nil
}
