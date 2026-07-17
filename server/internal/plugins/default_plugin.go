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
// violation. A project that already has a plugin sitting on the reserved
// "default" slug (e.g. one created manually before this feature shipped) is
// healed by promoting that plugin to is_default instead, since the slug
// collision means CreateDefaultPlugin can never succeed for that project.
//
// Takes the raw transaction (not just *repo.Queries) because the insert
// attempt runs inside a SAVEPOINT: a Postgres transaction is aborted after
// any failed statement, so without a savepoint the fallback SELECT on a lost
// race would itself fail with "current transaction is aborted" instead of
// recovering — every caller here already runs inside an outer transaction,
// so we can't just let a lost race abort the whole thing.
func EnsureDefaultPlugin(ctx context.Context, tx pgx.Tx, organizationID string, projectID uuid.UUID) (*EnsureDefaultPluginResult, error) {
	q := repo.New(tx)

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

	const savepoint = "ensure_default_plugin_insert"
	if _, err := tx.Exec(ctx, "SAVEPOINT "+savepoint); err != nil {
		return nil, fmt.Errorf("begin savepoint: %w", err)
	}

	created, err := q.CreateDefaultPlugin(ctx, repo.CreateDefaultPluginParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			switch pgErr.ConstraintName {
			case "plugins_project_id_is_default_key":
				if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+savepoint); err != nil {
					return nil, fmt.Errorf("rollback savepoint after race: %w", err)
				}
				plugin, err := q.GetDefaultPlugin(ctx, repo.GetDefaultPluginParams{
					OrganizationID: organizationID,
					ProjectID:      projectID,
				})
				if err != nil {
					return nil, fmt.Errorf("get default plugin after race: %w", err)
				}
				return &EnsureDefaultPluginResult{Plugin: plugin, Created: false}, nil
			case "plugins_organization_id_project_id_slug_key":
				if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+savepoint); err != nil {
					return nil, fmt.Errorf("rollback savepoint after slug conflict: %w", err)
				}
				plugin, err := q.PromoteToDefaultPlugin(ctx, repo.PromoteToDefaultPluginParams{
					OrganizationID: organizationID,
					ProjectID:      projectID,
				})
				if err != nil {
					return nil, fmt.Errorf("promote existing default-slug plugin: %w", err)
				}
				return &EnsureDefaultPluginResult{Plugin: plugin, Created: false}, nil
			}
		}
		return nil, fmt.Errorf("create default plugin: %w", err)
	}

	if _, err := tx.Exec(ctx, "RELEASE SAVEPOINT "+savepoint); err != nil {
		return nil, fmt.Errorf("release savepoint: %w", err)
	}

	// Default a freshly-created Default plugin to the org wildcard so it delivers
	// to every member — but only in the org's default project, the org-wide
	// baseline. agent.getPlugins scopes delivery by assignment, and the default
	// project's Default plugin (where enabled servers auto-attach) must reach
	// everyone unless an admin narrows it. A non-default project's Default plugin
	// starts with no assignments so enabling a server there doesn't auto-broadcast
	// org-wide. Only the genuine-creation path seeds this; the race/promote
	// recoveries above leave any existing assignments untouched.
	isDefaultProject, err := q.IsDefaultProject(ctx, repo.IsDefaultProjectParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
	})
	if err != nil {
		return nil, fmt.Errorf("check default project: %w", err)
	}
	if isDefaultProject {
		if _, err := q.AddPluginAssignment(ctx, repo.AddPluginAssignmentParams{
			PluginID:       created.ID,
			OrganizationID: organizationID,
			PrincipalUrn:   urn.PrincipalWildcard,
		}); err != nil {
			return nil, fmt.Errorf("assign default plugin to org: %w", err)
		}
	}

	return &EnsureDefaultPluginResult{Plugin: created, Created: true}, nil
}

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
func AttachToDefaultPlugin(ctx context.Context, tx pgx.Tx, params AttachToDefaultPluginParams) (*AttachToDefaultPluginResult, error) {
	ensured, err := EnsureDefaultPlugin(ctx, tx, params.OrganizationID, params.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("ensure default plugin: %w", err)
	}

	q := repo.New(tx)

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

	server, err := q.AddPluginServer(ctx, repo.AddPluginServerParams{
		PluginID:    ensured.Plugin.ID,
		ToolsetID:   params.ToolsetID,
		McpServerID: params.McpServerID,
		DisplayName: params.DisplayName,
		Policy:      "required",
		SortOrder:   0,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			switch pgErr.ConstraintName {
			case "plugin_servers_plugin_id_toolset_id_key", "plugin_servers_plugin_id_mcp_server_id_key":
				// Concurrent attach race lost after the existence check —
				// already attached, an expected no-op. Note the failed insert
				// has aborted the surrounding transaction, so the caller's
				// commit will still fail; a retry then hits the existence
				// check and no-ops cleanly.
				return nil, nil
			default:
				// display_name collision with a different, already-attached
				// server (or a manually-added one) is a real conflict, not
				// "already attached" — surface it instead of silently
				// dropping the server from the Default plugin.
			}
		}
		return nil, fmt.Errorf("attach server to default plugin: %w", err)
	}

	return &AttachToDefaultPluginResult{
		PluginID:      ensured.Plugin.ID,
		PluginName:    ensured.Plugin.Name,
		PluginSlug:    ensured.Plugin.Slug,
		PluginCreated: ensured.Created,
		Server:        server,
	}, nil
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
	attached, err := AttachToDefaultPlugin(ctx, dbtx, params)
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
