package plugins

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// EnsureDefaultPluginResult reports whether the Default plugin already
// existed or was just created, so callers can decide whether to audit-log a
// plugin creation event.
type EnsureDefaultPluginResult struct {
	Plugin  repo.Plugin
	Created bool
}

// EnsureDefaultPlugin returns a project's Default plugin, creating it if
// missing — covers projects that predate this feature (created before
// CreateProject started provisioning one). Concurrent callers racing to
// create it are resolved by re-fetching on the is_default unique-index
// violation. A pre-existing plugin already slugged "default" (created
// manually before is_default existed) is adopted — promoted to is_default —
// rather than surfaced as a conflict, since failing here fails every attach
// in the project, which fails every endpoint create and server enable.
func EnsureDefaultPlugin(ctx context.Context, q *repo.Queries, organizationID string, projectID uuid.UUID) (*EnsureDefaultPluginResult, error) {
	plugin, err := q.GetDefaultPlugin(ctx, repo.GetDefaultPluginParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
	})
	switch {
	case err == nil:
		return &EnsureDefaultPluginResult{Plugin: plugin, Created: false}, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, fmt.Errorf("get default plugin: %w", err)
	}

	// Adopt before creating: a project may already hold a plugin slugged
	// "default" from before auto-provisioning existed. Promoting it must
	// happen before the INSERT below, not in recovery from its unique
	// violation — a failed statement aborts the caller's surrounding
	// transaction, leaving nothing to recover into.
	promoted, err := q.PromotePluginToDefault(ctx, repo.PromotePluginToDefaultParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
	})
	switch {
	case err == nil:
		return &EnsureDefaultPluginResult{Plugin: promoted, Created: false}, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, fmt.Errorf("promote existing default plugin: %w", err)
	}

	created, err := q.CreateDefaultPlugin(ctx, repo.CreateDefaultPluginParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation && pgErr.ConstraintName == "plugins_project_id_is_default_key" {
			plugin, err := q.GetDefaultPlugin(ctx, repo.GetDefaultPluginParams{
				OrganizationID: organizationID,
				ProjectID:      projectID,
			})
			if err != nil {
				return nil, fmt.Errorf("get default plugin after race: %w", err)
			}
			return &EnsureDefaultPluginResult{Plugin: plugin, Created: false}, nil
		}
		return nil, fmt.Errorf("create default plugin: %w", err)
	}

	return &EnsureDefaultPluginResult{Plugin: created, Created: true}, nil
}

// maxAttachNameAttempts bounds AttachToDefaultPlugin's de-conflict-and-insert
// loop. Each retry means a concurrent attach took the chosen display name
// between the check and the insert; contention that deep is effectively
// impossible, so exhausting this is treated as an error rather than looping
// forever inside the caller's transaction.
const maxAttachNameAttempts = 5

// AttachToDefaultPluginParams identifies the server to attach — exactly one
// of ToolsetID / McpServerID must be Valid, mirroring the plugin_servers
// backend-exclusivity constraint.
type AttachToDefaultPluginParams struct {
	OrganizationID string
	ProjectID      uuid.UUID
	ToolsetID      uuid.NullUUID
	McpServerID    uuid.NullUUID
	DisplayName    string
}

// AttachToDefaultPluginResult is nil when AttachToDefaultPlugin no-ops
// (the server is already attached).
type AttachToDefaultPluginResult struct {
	PluginID      uuid.UUID
	PluginName    string
	PluginSlug    string
	PluginCreated bool
	Server        repo.PluginServer
}

// AttachToDefaultPlugin idempotently adds a server (toolset- or mcp_server-
// backed) to a project's Default plugin — creating the plugin first if the
// project predates this feature — so it shows up in the auto-published
// marketplace without a human visiting the Plugins page. Callers (toolsets,
// on MCP-enable; mcpendpoints, on first endpoint) run this in the same
// transaction as the triggering write. A server that's already attached is
// an expected no-op, not an error — reported by a nil result.
func AttachToDefaultPlugin(ctx context.Context, q *repo.Queries, params AttachToDefaultPluginParams) (*AttachToDefaultPluginResult, error) {
	ensured, err := EnsureDefaultPlugin(ctx, q, params.OrganizationID, params.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("ensure default plugin: %w", err)
	}

	// Check for an existing attachment before inserting rather than relying
	// on unique-violation classification alone: a duplicate insert of an
	// attached server trips the (plugin_id, display_name) index (created
	// before the backend ones, so Postgres reports it first) and the failed
	// statement aborts the caller's surrounding transaction either way.
	_, err = q.GetPluginServerByBackend(ctx, repo.GetPluginServerByBackendParams{
		PluginID:    ensured.Plugin.ID,
		ToolsetID:   params.ToolsetID,
		McpServerID: params.McpServerID,
	})
	switch {
	case err == nil:
		// Already attached — expected no-op, not an error.
		return nil, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, fmt.Errorf("check existing default plugin server: %w", err)
	}

	// De-conflict the display name against the plugin's live rows before each
	// insert attempt: collisions are legitimate (a toolset and an mcp_server
	// sharing a name, or a same-named server that was deleted and recreated),
	// and a unique violation would otherwise fail the caller's whole
	// transaction — aborting an endpoint create or a server enable — over a
	// cosmetic label. The insert itself is ON CONFLICT DO NOTHING, so even a
	// concurrent attach taking the chosen name between the check and the
	// insert can't raise. A skipped insert re-checks whether our backend got
	// attached concurrently (expected no-op) or only the name was taken
	// (retry with a fresh suffix — converging, since the loser's next list
	// sees the winner's committed row).
	for range maxAttachNameAttempts {
		existing, err := q.ListPluginServers(ctx, ensured.Plugin.ID)
		if err != nil {
			return nil, fmt.Errorf("list default plugin servers: %w", err)
		}
		taken := make(map[string]struct{}, len(existing))
		for _, row := range existing {
			taken[row.DisplayName] = struct{}{}
		}
		displayName := params.DisplayName
		for i := 2; ; i++ {
			if _, ok := taken[displayName]; !ok {
				break
			}
			displayName = fmt.Sprintf("%s %d", params.DisplayName, i)
		}

		server, err := q.AddPluginServerIfAbsent(ctx, repo.AddPluginServerIfAbsentParams{
			PluginID:    ensured.Plugin.ID,
			ToolsetID:   params.ToolsetID,
			McpServerID: params.McpServerID,
			DisplayName: displayName,
			Policy:      "required",
			SortOrder:   0,
		})
		switch {
		case err == nil:
			return &AttachToDefaultPluginResult{
				PluginID:      ensured.Plugin.ID,
				PluginName:    ensured.Plugin.Name,
				PluginSlug:    ensured.Plugin.Slug,
				PluginCreated: ensured.Created,
				Server:        server,
			}, nil
		case !errors.Is(err, pgx.ErrNoRows):
			return nil, fmt.Errorf("attach server to default plugin: %w", err)
		}

		_, err = q.GetPluginServerByBackend(ctx, repo.GetPluginServerByBackendParams{
			PluginID:    ensured.Plugin.ID,
			ToolsetID:   params.ToolsetID,
			McpServerID: params.McpServerID,
		})
		switch {
		case err == nil:
			// A concurrent attach of the same server won — expected no-op.
			return nil, nil
		case !errors.Is(err, pgx.ErrNoRows):
			return nil, fmt.Errorf("recheck default plugin server after conflict: %w", err)
		}
	}

	return nil, fmt.Errorf("attach server to default plugin: display name still contended after %d attempts", maxAttachNameAttempts)
}

// AttachToDefaultPluginAudited runs AttachToDefaultPlugin and records the
// same audit trail a manual "add server to plugin" produces: a plugin
// creation event when the Default plugin was lazily provisioned, and a
// plugin-server add event for the attached server. Callers (toolsets on
// MCP-enable, mcpendpoints on first endpoint, mcpservers on visibility
// enable) run this inside the same transaction as the triggering write.
// Both audit events are scoped to params' organization/project — the same
// values the plugin rows are written with — while authCtx supplies only the
// acting user. Returns pluginCreated=true when this call created the Default
// plugin (project predates the feature) — callers should enqueue an initial
// marketplace publish for it, but only after their own transaction commits,
// since this runs pre-commit and the DB writes could still roll back.
func AttachToDefaultPluginAudited(ctx context.Context, dbtx pgx.Tx, auditLogger *audit.Logger, authCtx *contextvalues.AuthContext, params AttachToDefaultPluginParams) (bool, error) {
	attached, err := AttachToDefaultPlugin(ctx, repo.New(dbtx), params)
	if err != nil {
		return false, fmt.Errorf("attach server to default plugin: %w", err)
	}
	if attached == nil {
		return false, nil
	}

	if attached.PluginCreated {
		if err := auditLogger.LogPluginCreate(ctx, dbtx, audit.LogPluginCreateEvent{
			OrganizationID:   params.OrganizationID,
			ProjectID:        params.ProjectID,
			Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName: authCtx.Email,
			ActorSlug:        nil,
			PluginID:         attached.PluginID,
			PluginName:       attached.PluginName,
			PluginSlug:       attached.PluginSlug,
		}); err != nil {
			return false, fmt.Errorf("audit log default plugin create: %w", err)
		}
	}

	// Exactly one of the URNs is set, mirroring params' toolset_id XOR
	// mcp_server_id contract.
	var toolsetURN *urn.Toolset
	var mcpServerURN *urn.McpServer
	if params.ToolsetID.Valid {
		u := urn.NewToolset(params.ToolsetID.UUID)
		toolsetURN = &u
	}
	if params.McpServerID.Valid {
		u := urn.NewMcpServer(params.McpServerID.UUID)
		mcpServerURN = &u
	}

	if err := auditLogger.LogPluginServerAdd(ctx, dbtx, audit.LogPluginServerAddEvent{
		OrganizationID:    params.OrganizationID,
		ProjectID:         params.ProjectID,
		Actor:             urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:  authCtx.Email,
		ActorSlug:         nil,
		PluginID:          attached.PluginID,
		PluginName:        attached.PluginName,
		PluginSlug:        attached.PluginSlug,
		ServerID:          attached.Server.ID,
		ServerDisplayName: attached.Server.DisplayName,
		ServerPolicy:      attached.Server.Policy,
		ServerSortOrder:   attached.Server.SortOrder,
		ToolsetURN:        toolsetURN,
		McpServerURN:      mcpServerURN,
	}); err != nil {
		return false, fmt.Errorf("audit log default plugin server add: %w", err)
	}

	return attached.PluginCreated, nil
}
