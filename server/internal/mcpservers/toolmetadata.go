package mcpservers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// pgCardinalityViolation is raised by ON CONFLICT DO UPDATE when one statement
// would touch the same conflict target twice, which is how a tool name repeated
// within a single batch payload surfaces.
const pgCardinalityViolation = "21000"

// pgUniqueViolation is raised by the strictly additive insert when a tool in
// the payload already holds a live stored entry.
const pgUniqueViolation = "23505"

// toolMetadataInput is the jsonb element shape the batch writes unpack with
// jsonb_array_elements and ->> accessors. The json tags must track the key
// names those accessors read; see SetMCPServerToolMetadata for why the queries
// unpack the payload that way rather than with jsonb_to_recordset.
type toolMetadataInput struct {
	ToolName        string  `json:"tool_name"`
	Title           *string `json:"title"`
	ReadOnlyHint    *bool   `json:"read_only_hint"`
	DestructiveHint *bool   `json:"destructive_hint"`
	IdempotentHint  *bool   `json:"idempotent_hint"`
	OpenWorldHint   *bool   `json:"open_world_hint"`
}

// normalizeToolMetadataInput converts the wire payload into the jsonb element
// shape, trimming names and rejecting empty ones: tool_name carries no CHECK
// constraint, so emptiness is enforced here. Duplicate names are deliberately
// not handled — the two batch methods have different policies for them.
func normalizeToolMetadataInput(forms []*gen.ToolMetadataForm) ([]toolMetadataInput, error) {
	tools := make([]toolMetadataInput, 0, len(forms))
	for _, tool := range forms {
		name := strings.TrimSpace(tool.ToolName)
		if name == "" {
			return nil, errEmptyToolName
		}
		tools = append(tools, toolMetadataInput{
			ToolName:        name,
			Title:           tool.Title,
			ReadOnlyHint:    tool.ReadOnlyHint,
			DestructiveHint: tool.DestructiveHint,
			IdempotentHint:  tool.IdempotentHint,
			OpenWorldHint:   tool.OpenWorldHint,
		})
	}

	return tools, nil
}

var errEmptyToolName = errors.New("tool name must be non-empty")

func ptrEq[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// toolMetadataCollectionUnchanged reports whether two collection snapshots carry
// the same client-settable state. Timestamps are excluded because every upsert
// bumps updated_at: an authoritative re-sync that changes nothing should not
// produce an audit entry. Both snapshots arrive ordered by tool_name, and the
// partial unique index makes that ordering total over live rows.
func toolMetadataCollectionUnchanged(before, after []*types.ToolMetadata) bool {
	return slices.EqualFunc(before, after, func(a, b *types.ToolMetadata) bool {
		return a.ToolName == b.ToolName &&
			ptrEq(a.Title, b.Title) &&
			ptrEq(a.ReadOnlyHint, b.ReadOnlyHint) &&
			ptrEq(a.DestructiveHint, b.DestructiveHint) &&
			ptrEq(a.IdempotentHint, b.IdempotentHint) &&
			ptrEq(a.OpenWorldHint, b.OpenWorldHint)
	})
}

// toolMetadataAuditEvent assembles the collection-level audit event shared by
// every tool metadata mutation. The subject is the server's metadata collection,
// so one entry covers a write of any size.
func toolMetadataAuditEvent(
	authCtx *contextvalues.AuthContext,
	server repo.McpServer,
	before, after []*types.ToolMetadata,
) audit.LogMcpServerToolMetadataUpdateEvent {
	return audit.LogMcpServerToolMetadataUpdateEvent{
		OrganizationID:           authCtx.ActiveOrganizationID,
		ProjectID:                *authCtx.ProjectID,
		Actor:                    urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:         authCtx.Email,
		ActorSlug:                nil,
		McpServerToolMetadataURN: urn.NewMcpServerToolMetadata(server.ID),
		McpServerName:            conv.FromPGTextOrEmpty[string](server.Name),
		McpServerSlug:            conv.FromPGTextOrEmpty[string](server.Slug),
		SnapshotBefore:           before,
		SnapshotAfter:            after,
	}
}

func (s *Service) SetToolMetadataBatch(ctx context.Context, payload *gen.SetToolMetadataBatchPayload) (*gen.SetToolMetadataBatchResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	serverID, err := uuid.Parse(payload.McpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp server id").LogError(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPWrite, serverID.String(), authCtx.ProjectID.String())); err != nil {
		return nil, err
	}

	// Names repeated within the payload are left to the upsert, which rejects
	// them as a cardinality violation.
	tools, err := normalizeToolMetadataInput(payload.Tools)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid tool metadata payload").LogError(ctx, logger)
	}

	toolsJSON, err := json.Marshal(tools)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "encode tool metadata payload").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	// Taken before the collection is read, so this mutation's before and after
	// snapshots bracket it alone.
	if err := txRepo.LockMCPServerToolMetadataWrite(ctx, serverID.String()); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock tool metadata for write").LogError(ctx, logger)
	}

	server, err := loadMCPServerForToolMetadata(ctx, txRepo, serverID, *authCtx.ProjectID, logger)
	if err != nil {
		return nil, err
	}

	// Tool metadata backs disposition-aware RBAC for remote-backed servers.
	// Toolset-backed servers already persist annotation hints on their tool
	// definition tables, so storing a second copy here is disallowed.
	if !server.RemoteMcpServerID.Valid {
		return nil, oops.E(oops.CodeInvalid, nil, "tool metadata is only supported for MCP servers backed by a remote MCP server").LogError(ctx, logger)
	}

	before, err := listToolMetadataViews(ctx, txRepo, serverID, *authCtx.ProjectID, logger)
	if err != nil {
		return nil, err
	}

	rows, err := txRepo.SetMCPServerToolMetadata(ctx, repo.SetMCPServerToolMetadataParams{
		Tools:       toolsJSON,
		ProjectID:   *authCtx.ProjectID,
		McpServerID: serverID,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgCardinalityViolation {
			return nil, oops.E(oops.CodeBadRequest, err, "duplicate tool name in payload").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "set tool metadata").LogError(ctx, logger)
	}

	after := make([]*types.ToolMetadata, 0, len(rows))
	deleted := 0
	for _, row := range rows {
		if row.WasDeleted {
			deleted++
			continue
		}
		after = append(after, mv.BuildToolMetadataSetView(row))
	}

	if err := s.logToolMetadataChange(ctx, dbtx, authCtx, server, before, after); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log tool metadata change").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return &gen.SetToolMetadataBatchResult{Tools: after, Deleted: deleted}, nil
}

func (s *Service) AddToolMetadataBatch(ctx context.Context, payload *gen.AddToolMetadataBatchPayload) (*gen.AddToolMetadataBatchResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	serverID, err := uuid.Parse(payload.McpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp server id").LogError(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPWrite, serverID.String(), authCtx.ProjectID.String())); err != nil {
		return nil, err
	}

	tools, err := normalizeToolMetadataInput(payload.Tools)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid tool metadata payload").LogError(ctx, logger)
	}

	// Unlike the authoritative path, duplicates within the payload are caught
	// here rather than by the insert. Postgres reports them as the same unique
	// violation as an already-stored tool, and the two mean different things to
	// a caller: a malformed request versus a stale view of stored state.
	seen := make(map[string]bool, len(tools))
	for _, tool := range tools {
		if seen[tool.ToolName] {
			return nil, oops.E(oops.CodeBadRequest, fmt.Errorf("duplicate tool name: %q", tool.ToolName), "duplicate tool name in payload").LogError(ctx, logger)
		}
		seen[tool.ToolName] = true
	}

	toolsJSON, err := json.Marshal(tools)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "encode tool metadata payload").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	// Taken before the collection is read, so this mutation's before and after
	// snapshots bracket it alone.
	if err := txRepo.LockMCPServerToolMetadataWrite(ctx, serverID.String()); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock tool metadata for write").LogError(ctx, logger)
	}

	server, err := loadMCPServerForToolMetadata(ctx, txRepo, serverID, *authCtx.ProjectID, logger)
	if err != nil {
		return nil, err
	}

	// Tool metadata backs disposition-aware RBAC for remote-backed servers.
	// Toolset-backed servers already persist annotation hints on their tool
	// definition tables, so storing a second copy here is disallowed.
	if !server.RemoteMcpServerID.Valid {
		return nil, oops.E(oops.CodeInvalid, nil, "tool metadata is only supported for MCP servers backed by a remote MCP server").LogError(ctx, logger)
	}

	before, err := listToolMetadataViews(ctx, txRepo, serverID, *authCtx.ProjectID, logger)
	if err != nil {
		return nil, err
	}

	rows, err := txRepo.AddMCPServerToolMetadata(ctx, repo.AddMCPServerToolMetadataParams{
		ProjectID:   *authCtx.ProjectID,
		McpServerID: serverID,
		Tools:       toolsJSON,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			// The statement aborted this transaction, so the lookup that names
			// the offending tools runs on a fresh one.
			names := s.storedToolNames(ctx, serverID, *authCtx.ProjectID, seen)
			return nil, oops.E(oops.CodeConflict, err, "tool metadata already stored for: %s", strings.Join(names, ", ")).LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "add tool metadata").LogError(ctx, logger)
	}

	created := make([]*types.ToolMetadata, 0, len(rows))
	for _, row := range rows {
		created = append(created, mv.BuildToolMetadataView(row))
	}

	after, err := listToolMetadataViews(ctx, txRepo, serverID, *authCtx.ProjectID, logger)
	if err != nil {
		return nil, err
	}

	if err := s.logToolMetadataChange(ctx, dbtx, authCtx, server, before, after); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log tool metadata change").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return &gen.AddToolMetadataBatchResult{Tools: created}, nil
}

// storedToolNames returns the payload names that already hold a live stored
// entry, sorted, so a conflict can name what actually collided. A failure to
// look them up is not worth failing the request over — the caller is already
// returning a conflict — so it yields an empty list.
func (s *Service) storedToolNames(ctx context.Context, serverID, projectID uuid.UUID, payloadNames map[string]bool) []string {
	rows, err := repo.New(s.db).ListMCPServerToolMetadata(ctx, repo.ListMCPServerToolMetadataParams{
		McpServerID:    serverID,
		ProjectID:      projectID,
		IncludeDeleted: false,
	})
	if err != nil {
		return nil
	}

	var names []string
	for _, row := range rows {
		if payloadNames[row.ToolName] {
			names = append(names, row.ToolName)
		}
	}
	slices.Sort(names)

	return names
}

func (s *Service) ListToolMetadata(ctx context.Context, payload *gen.ListToolMetadataPayload) (*gen.ListToolMetadataResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	serverID, err := uuid.Parse(payload.McpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp server id").LogError(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPRead, serverID.String(), authCtx.ProjectID.String())); err != nil {
		return nil, err
	}

	r := repo.New(s.db)

	server, err := loadMCPServerForToolMetadata(ctx, r, serverID, *authCtx.ProjectID, logger)
	if err != nil {
		return nil, err
	}

	// Rejected on the same terms as the writes. A toolset-backed server can
	// never hold tool metadata, so answering 200 with an empty list would read
	// as "none recorded yet" and invite a write that is itself rejected.
	if !server.RemoteMcpServerID.Valid {
		return nil, oops.E(oops.CodeInvalid, nil, "tool metadata is only supported for MCP servers backed by a remote MCP server").LogError(ctx, logger)
	}

	rows, err := r.ListMCPServerToolMetadata(ctx, repo.ListMCPServerToolMetadataParams{
		McpServerID:    serverID,
		ProjectID:      *authCtx.ProjectID,
		IncludeDeleted: payload.IncludeDeleted != nil && *payload.IncludeDeleted,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list tool metadata").LogError(ctx, logger)
	}

	return &gen.ListToolMetadataResult{Tools: mv.BuildToolMetadataListView(rows)}, nil
}

func (s *Service) SetToolMetadata(ctx context.Context, payload *gen.SetToolMetadataPayload) (*types.ToolMetadata, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	serverID, err := uuid.Parse(payload.McpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp server id").LogError(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPWrite, serverID.String(), authCtx.ProjectID.String())); err != nil {
		return nil, err
	}

	name := strings.TrimSpace(payload.ToolName)
	if name == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "tool name must be non-empty").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	// Taken before the collection is read, so this mutation's before and after
	// snapshots bracket it alone.
	if err := txRepo.LockMCPServerToolMetadataWrite(ctx, serverID.String()); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock tool metadata for write").LogError(ctx, logger)
	}

	server, err := loadMCPServerForToolMetadata(ctx, txRepo, serverID, *authCtx.ProjectID, logger)
	if err != nil {
		return nil, err
	}

	before, err := listToolMetadataViews(ctx, txRepo, serverID, *authCtx.ProjectID, logger)
	if err != nil {
		return nil, err
	}

	updated, err := txRepo.UpdateMCPServerToolMetadata(ctx, repo.UpdateMCPServerToolMetadataParams{
		Title:           conv.PtrToPGText(payload.Title),
		ReadOnlyHint:    conv.PtrToPGBool(payload.ReadOnlyHint),
		DestructiveHint: conv.PtrToPGBool(payload.DestructiveHint),
		IdempotentHint:  conv.PtrToPGBool(payload.IdempotentHint),
		OpenWorldHint:   conv.PtrToPGBool(payload.OpenWorldHint),
		McpServerID:     serverID,
		ProjectID:       *authCtx.ProjectID,
		ToolName:        name,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "tool metadata not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update tool metadata").LogError(ctx, logger)
	}

	after, err := listToolMetadataViews(ctx, txRepo, serverID, *authCtx.ProjectID, logger)
	if err != nil {
		return nil, err
	}

	if err := s.logToolMetadataChange(ctx, dbtx, authCtx, server, before, after); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log tool metadata change").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return mv.BuildToolMetadataView(updated), nil
}

func (s *Service) DeleteToolMetadata(ctx context.Context, payload *gen.DeleteToolMetadataPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	serverID, err := uuid.Parse(payload.McpServerID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid mcp server id").LogError(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPWrite, serverID.String(), authCtx.ProjectID.String())); err != nil {
		return err
	}

	// Trimmed on the same terms as SetToolMetadata: names are stored trimmed,
	// so a padded name would otherwise miss and report a misleading 404.
	name := strings.TrimSpace(payload.ToolName)
	if name == "" {
		return oops.E(oops.CodeBadRequest, errEmptyToolName, "invalid tool name").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	// Taken before the collection is read, so this mutation's before and after
	// snapshots bracket it alone.
	if err := txRepo.LockMCPServerToolMetadataWrite(ctx, serverID.String()); err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock tool metadata for write").LogError(ctx, logger)
	}

	server, err := loadMCPServerForToolMetadata(ctx, txRepo, serverID, *authCtx.ProjectID, logger)
	if err != nil {
		return err
	}

	before, err := listToolMetadataViews(ctx, txRepo, serverID, *authCtx.ProjectID, logger)
	if err != nil {
		return err
	}

	if _, err := txRepo.DeleteMCPServerToolMetadata(ctx, repo.DeleteMCPServerToolMetadataParams{
		McpServerID: serverID,
		ProjectID:   *authCtx.ProjectID,
		ToolName:    name,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "tool metadata not found").LogError(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "delete tool metadata").LogError(ctx, logger)
	}

	after, err := listToolMetadataViews(ctx, txRepo, serverID, *authCtx.ProjectID, logger)
	if err != nil {
		return err
	}

	if err := s.logToolMetadataChange(ctx, dbtx, authCtx, server, before, after); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log tool metadata change").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return nil
}

// logToolMetadataChange records one collection-level audit entry, skipping
// writes that left the collection as they found it.
func (s *Service) logToolMetadataChange(
	ctx context.Context,
	dbtx repo.DBTX,
	authCtx *contextvalues.AuthContext,
	server repo.McpServer,
	before, after []*types.ToolMetadata,
) error {
	if toolMetadataCollectionUnchanged(before, after) {
		return nil
	}

	if err := s.audit.LogMcpServerToolMetadataUpdate(ctx, dbtx, toolMetadataAuditEvent(authCtx, server, before, after)); err != nil {
		return fmt.Errorf("log tool metadata update: %w", err)
	}

	return nil
}

func loadMCPServerForToolMetadata(
	ctx context.Context,
	r *repo.Queries,
	serverID uuid.UUID,
	projectID uuid.UUID,
	logger *slog.Logger,
) (repo.McpServer, error) {
	server, err := r.GetMCPServerByIDAndProjectID(ctx, repo.GetMCPServerByIDAndProjectIDParams{
		ID:        serverID,
		ProjectID: projectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return repo.McpServer{}, oops.E(oops.CodeNotFound, err, "mcp server not found").LogError(ctx, logger)
		}
		return repo.McpServer{}, oops.E(oops.CodeUnexpected, err, "get mcp server").LogError(ctx, logger)
	}

	return server, nil
}

func listToolMetadataViews(
	ctx context.Context,
	r *repo.Queries,
	serverID uuid.UUID,
	projectID uuid.UUID,
	logger *slog.Logger,
) ([]*types.ToolMetadata, error) {
	rows, err := r.ListMCPServerToolMetadata(ctx, repo.ListMCPServerToolMetadataParams{
		McpServerID:    serverID,
		ProjectID:      projectID,
		IncludeDeleted: false,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list tool metadata").LogError(ctx, logger)
	}

	return mv.BuildToolMetadataListView(rows), nil
}
