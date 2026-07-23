package mcpservers

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
)

// toolDispositionTTL bounds staleness of the disposition cache. Writes evict
// eagerly (see ToolDispositionCache.Invalidate), so this is only the ceiling
// for a change that slips past invalidation — e.g. an eviction that failed
// against a briefly-unavailable Redis.
const toolDispositionTTL = 15 * time.Minute

// serverToolDispositions is the cached per-server view the cache serves: a tool
// name -> disposition token map, already derived from the stored annotation
// hints. Only tools with a non-empty disposition are present; a missing key
// reads as the empty disposition, so an empty (but non-nil) map is a valid
// negative-cache entry for a server with no classifying metadata and still
// costs a single Redis GET on later calls.
type serverToolDispositions struct {
	McpServerID  string            `json:"mcp_server_id"`
	Dispositions map[string]string `json:"dispositions"`
}

var _ cache.CacheableObject[serverToolDispositions] = (*serverToolDispositions)(nil)

func serverToolDispositionsCacheKey(mcpServerID string) string {
	return fmt.Sprintf("mcpservers:tool_disposition:%s", mcpServerID)
}

func (s serverToolDispositions) CacheKey() string {
	return serverToolDispositionsCacheKey(s.McpServerID)
}

func (s serverToolDispositions) AdditionalCacheKeys() []string {
	return []string{}
}

func (s serverToolDispositions) TTL() time.Duration {
	return toolDispositionTTL
}

// ToolDispositionCache resolves the RBAC `disposition` dimension for a
// remote-MCP tool from admin-authored metadata (mcp_server_tool_metadata),
// through a Redis pull-through cache over Postgres. It is the read path of the
// Tool Metadata Materialization design (RFC §4.2): no upstream fetch and no
// writes on the hot path. It lives beside the metadata write methods so those
// evict it directly; the remote-MCP proxy consumes it read-only. One instance
// is constructed per server process and shared by both.
type ToolDispositionCache struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	cache  cache.TypedCacheObject[serverToolDispositions]
}

// NewToolDispositionCache builds the cache over the given database and cache
// backends.
func NewToolDispositionCache(logger *slog.Logger, db *pgxpool.Pool, c cache.Cache) *ToolDispositionCache {
	logger = logger.With(attr.SlogComponent("tool-disposition"))
	return &ToolDispositionCache{
		logger: logger,
		db:     db,
		cache:  cache.NewTypedObjectCache[serverToolDispositions](logger.With(attr.SlogCacheNamespace("tool-disposition")), c, cache.SuffixNone),
	}
}

// Dispositions returns the tool-name -> disposition token map for a server,
// through the cache. An empty (non-nil) map means the server has no classifying
// metadata; a missing tool key reads as the empty disposition.
//
// A non-nil error means resolution itself failed (bad id, cache miss then DB
// error). Callers gating a tool call MUST fail closed on it — disposition is a
// security dimension, and silently substituting the empty disposition would
// relax an annotation-scoped policy exactly when the store is unavailable.
func (c *ToolDispositionCache) Dispositions(ctx context.Context, mcpServerID, projectID string) (map[string]string, error) {
	if cached, err := c.cache.Get(ctx, serverToolDispositionsCacheKey(mcpServerID)); err == nil {
		return cached.Dispositions, nil
	}

	serverUUID, err := uuid.Parse(mcpServerID)
	if err != nil {
		return nil, fmt.Errorf("parse mcp server id: %w", err)
	}
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("parse project id: %w", err)
	}

	rows, err := repo.New(c.db).ListMCPServerToolMetadata(ctx, repo.ListMCPServerToolMetadataParams{
		McpServerID:    serverUUID,
		ProjectID:      projectUUID,
		IncludeDeleted: false,
	})
	if err != nil {
		return nil, fmt.Errorf("list tool metadata for disposition: %w", err)
	}

	dispositions := make(map[string]string, len(rows))
	for _, row := range rows {
		disposition := conv.DispositionFromAnnotations(conv.AnnotationsFromColumns(
			row.ReadOnlyHint,
			row.DestructiveHint,
			row.IdempotentHint,
			row.OpenWorldHint,
		))
		// Tools reviewed with no behavior class are omitted: absent reads as the
		// empty disposition, which is what an empty token would mean anyway.
		if disposition == "" {
			continue
		}
		dispositions[row.ToolName] = disposition
	}

	entry := serverToolDispositions{McpServerID: mcpServerID, Dispositions: dispositions}
	if err := c.cache.Store(ctx, entry); err != nil {
		c.logger.WarnContext(ctx, "cache remote MCP tool dispositions",
			attr.SlogError(err),
			attr.SlogMcpServerID(mcpServerID),
		)
	}

	return dispositions, nil
}

// Invalidate evicts a server's cached disposition set so an admin metadata
// write takes effect before the TTL lapses.
func (c *ToolDispositionCache) Invalidate(ctx context.Context, mcpServerID string) error {
	if err := c.cache.DeleteByKey(ctx, serverToolDispositionsCacheKey(mcpServerID)); err != nil {
		return fmt.Errorf("invalidate tool dispositions: %w", err)
	}
	return nil
}
