package plugins

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/speakeasy-api/gram/server/internal/plugins/repo"
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
// violation; any other unique violation (e.g. a pre-existing plugin already
// named/slugged "Default"/"default") is a real conflict and surfaces as an
// error rather than masking it.
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
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation && pgErr.ConstraintName == "plugins_project_id_is_default_key" {
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
		}
		return nil, fmt.Errorf("create default plugin: %w", err)
	}

	if _, err := tx.Exec(ctx, "RELEASE SAVEPOINT "+savepoint); err != nil {
		return nil, fmt.Errorf("release savepoint: %w", err)
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
				// Already attached — expected no-op, not an error.
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
