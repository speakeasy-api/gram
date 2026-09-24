package tooldisposition

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
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
	ProjectID    string                            `json:"project_id"`
	MCPServerID  string                            `json:"mcp_server_id"`
	Dispositions map[string]string                 `json:"dispositions"`
	Annotations  map[string]*types.ToolAnnotations `json:"annotations"`
}

var _ cache.CacheableObject[serverTools] = (*serverTools)(nil)

func cacheKey(projectID, mcpServerID string) string {
	return fmt.Sprintf("mcpservers:tool_disposition:v3:%s:%s", projectID, mcpServerID)
}

func (s serverTools) CacheKey() string {
	return cacheKey(s.ProjectID, s.MCPServerID)
}

func (s serverTools) AdditionalCacheKeys() []string {
	return []string{}
}

func (s serverTools) TTL() time.Duration {
	return ttl
}

type cacheGeneration struct {
	mu    sync.Mutex
	value uint64
}

// Cache resolves effective tool annotations and dispositions through a pull-through cache.
type Cache struct {
	logger        *slog.Logger
	db            *pgxpool.Pool
	cache         cache.TypedCacheObject[serverTools]
	generationsMu sync.Mutex
	generations   map[string]*cacheGeneration
}

// New creates a tool annotation cache.
func New(logger *slog.Logger, db *pgxpool.Pool, c cache.Cache) *Cache {
	logger = logger.With(attr.SlogComponent("tool-disposition"))
	return &Cache{
		logger:        logger,
		db:            db,
		cache:         cache.NewTypedObjectCache[serverTools](logger.With(attr.SlogCacheNamespace("tool-disposition")), c, cache.SuffixNone),
		generationsMu: sync.Mutex{},
		generations:   make(map[string]*cacheGeneration),
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
	projectIDString := projectID.String()
	key := cacheKey(projectIDString, serverID)
	generation := c.generationFor(key)

	generation.mu.Lock()
	if cached, err := c.cache.Get(ctx, key); err == nil {
		generation.mu.Unlock()
		return cached, nil
	}
	resolveGeneration := generation.value
	generation.mu.Unlock()

	rows, err := repo.New(c.db).ListEffectiveMCPServerToolAnnotations(ctx, repo.ListEffectiveMCPServerToolAnnotationsParams{
		McpServerID: mcpServerID,
		ProjectID:   projectID,
	})
	if err != nil {
		return serverTools{}, fmt.Errorf("list effective MCP tool annotations: %w", err)
	}

	resolved := serverTools{
		ProjectID:    projectIDString,
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

	c.storeResolved(ctx, generation, resolveGeneration, resolved)

	return resolved, nil
}
func (c *Cache) storeResolved(
	ctx context.Context,
	generation *cacheGeneration,
	resolveGeneration uint64,
	resolved serverTools,
) {
	generation.mu.Lock()
	defer generation.mu.Unlock()

	if generation.value != resolveGeneration {
		return
	}
	if err := c.cache.Store(ctx, resolved); err != nil {
		c.logger.WarnContext(ctx, "cache MCP tool annotations",
			attr.SlogError(err),
			attr.SlogMcpServerID(resolved.MCPServerID),
		)
	}
}

func (c *Cache) generationFor(key string) *cacheGeneration {
	c.generationsMu.Lock()
	defer c.generationsMu.Unlock()

	if generation, ok := c.generations[key]; ok {
		return generation
	}
	generation := &cacheGeneration{mu: sync.Mutex{}, value: 0}
	c.generations[key] = generation
	return generation
}

// Invalidate evicts one project's cached annotations for a server.
func (c *Cache) Invalidate(ctx context.Context, projectID, mcpServerID uuid.UUID) error {
	key := cacheKey(projectID.String(), mcpServerID.String())
	generation := c.generationFor(key)
	generation.mu.Lock()
	defer generation.mu.Unlock()

	generation.value++
	if err := c.cache.DeleteByKey(ctx, key); err != nil {
		return fmt.Errorf("invalidate tool dispositions: %w", err)
	}
	return nil
}
