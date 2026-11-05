package tooldisposition

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ttl = 15 * time.Minute

	// catalogSystemComponent is the audit principal for catalog entries recorded
	// from upstream listings rather than by a person.
	catalogSystemComponent = "tool-catalog"

	// catalogSystemDisplayName names that principal in the audit log.
	catalogSystemDisplayName = "Speakeasy tool catalog"

	// invalidateTimeout bounds cache eviction after a committed catalog write.
	// The write has already committed, so this only limits how long a slow cache
	// holds up the request.
	invalidateTimeout = 5 * time.Second
)

// ToolRecord is a tool to add to the catalog as the upstream described it. The
// json tags name the keys AddMissingMCPServerToolMetadata reads from its jsonb
// payload.
type ToolRecord struct {
	// ToolName is the tool's name on the upstream.
	ToolName string `json:"tool_name"`

	// Title is the upstream's display name for the tool, when it declares one.
	Title *string `json:"title"`

	// ReadOnlyHint is the upstream's readOnlyHint; nil when unset.
	ReadOnlyHint *bool `json:"read_only_hint"`

	// DestructiveHint is the upstream's destructiveHint; nil when unset.
	DestructiveHint *bool `json:"destructive_hint"`

	// IdempotentHint is the upstream's idempotentHint; nil when unset.
	IdempotentHint *bool `json:"idempotent_hint"`

	// OpenWorldHint is the upstream's openWorldHint; nil when unset.
	OpenWorldHint *bool `json:"open_world_hint"`
}

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

// Keys hash onto a fixed set of generations so the registry stays bounded; a
// collision only skips one cache write.
const generationStripes = 256

type cacheGeneration struct {
	mu    sync.Mutex
	value uint64
}

// Cache resolves effective tool annotations and dispositions through a pull-through cache.
type Cache struct {
	logger      *slog.Logger
	db          *pgxpool.Pool
	cache       cache.TypedCacheObject[serverTools]
	audit       *audit.Logger
	generations [generationStripes]cacheGeneration
}

// New creates a tool annotation cache.
func New(logger *slog.Logger, db *pgxpool.Pool, c cache.Cache) *Cache {
	logger = logger.With(attr.SlogComponent("tool-disposition"))
	return &Cache{
		logger:      logger,
		db:          db,
		cache:       cache.NewTypedObjectCache[serverTools](logger.With(attr.SlogCacheNamespace("tool-disposition")), c, cache.SuffixNone),
		audit:       audit.NewLogger(),
		generations: [generationStripes]cacheGeneration{},
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

// ToolCatalog returns every recorded tool mapped to its disposition token, which
// is empty for a recorded tool with no behavior class. A missing key means the
// tool has never been recorded.
func (c *Cache) ToolCatalog(ctx context.Context, mcpServerID, projectID string) (map[string]string, error) {
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

	catalog := make(map[string]string, len(resolved.Annotations))
	for name := range resolved.Annotations {
		catalog[name] = resolved.Dispositions[name]
	}
	return catalog, nil
}

// RecordMissingTools adds tools with no catalog entry and audits the change as a
// system write. Existing entries are left untouched, and nothing is audited when
// every tool already has an entry. An error means the catalog can't be relied on
// to reflect these tools, so callers enforcing access must fail closed.
func (c *Cache) RecordMissingTools(ctx context.Context, organizationID, mcpServerID, projectID string, tools []ToolRecord) error {
	serverUUID, err := uuid.Parse(mcpServerID)
	if err != nil {
		return fmt.Errorf("parse mcp server id: %w", err)
	}
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return fmt.Errorf("parse project id: %w", err)
	}

	payload, err := json.Marshal(tools)
	if err != nil {
		return fmt.Errorf("encode tool records: %w", err)
	}

	dbtx, err := c.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tool catalog write: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	if err := txRepo.LockMCPServerToolMetadataWrite(ctx, mcpServerID); err != nil {
		return fmt.Errorf("lock tool catalog: %w", err)
	}

	server, err := txRepo.GetMCPServerByIDAndProjectID(ctx, repo.GetMCPServerByIDAndProjectIDParams{ID: serverUUID, ProjectID: projectUUID})
	if err != nil {
		return fmt.Errorf("get mcp server: %w", err)
	}

	listParams := repo.ListMCPServerToolMetadataParams{McpServerID: serverUUID, ProjectID: projectUUID, IncludeDeleted: false}
	before, err := txRepo.ListMCPServerToolMetadata(ctx, listParams)
	if err != nil {
		return fmt.Errorf("list tool catalog before write: %w", err)
	}

	inserted, err := txRepo.AddMissingMCPServerToolMetadata(ctx, repo.AddMissingMCPServerToolMetadataParams{
		ProjectID:   projectUUID,
		McpServerID: serverUUID,
		Tools:       payload,
	})
	if err != nil {
		return fmt.Errorf("add missing tools: %w", err)
	}
	if len(inserted) == 0 {
		return nil
	}

	after, err := txRepo.ListMCPServerToolMetadata(ctx, listParams)
	if err != nil {
		return fmt.Errorf("list tool catalog after write: %w", err)
	}

	if err := c.audit.LogMcpServerToolMetadataUpdate(ctx, dbtx, audit.LogMcpServerToolMetadataUpdateEvent{
		OrganizationID:             organizationID,
		ProjectID:                  projectUUID,
		Actor:                      urn.NewPrincipal(urn.PrincipalTypeSystem, catalogSystemComponent),
		ActorDisplayName:           new(catalogSystemDisplayName),
		ActorSlug:                  nil,
		McpServerURN:               urn.NewMcpServer(server.ID),
		McpServerName:              conv.FromPGTextOrEmpty[string](server.Name),
		McpServerSlug:              conv.FromPGTextOrEmpty[string](server.Slug),
		ToolMetadataSnapshotBefore: mv.BuildToolMetadataListView(before),
		ToolMetadataSnapshotAfter:  mv.BuildToolMetadataListView(after),
	}); err != nil {
		return fmt.Errorf("log tool catalog update: %w", err)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tool catalog write: %w", err)
	}

	// Detached: the write has committed, so a caller disconnecting now must not
	// strand a stale view in cache until the TTL lapses.
	invalidateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), invalidateTimeout)
	defer cancel()
	if err := c.Invalidate(invalidateCtx, projectUUID, serverUUID); err != nil {
		return fmt.Errorf("invalidate tool catalog: %w", err)
	}
	return nil
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
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return &c.generations[h.Sum32()%generationStripes]
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
