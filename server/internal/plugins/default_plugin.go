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
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
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

var (
	// ErrDefaultPluginNotFound reports that the requested project has no active
	// default plugin. Callers that must not provision a plugin use this to return
	// a bounded repair action instead of creating one implicitly.
	ErrDefaultPluginNotFound = errors.New("default plugin not found")

	// ErrMcpServerNotFoundForProject reports that the MCP server is not active in
	// the requested project.
	ErrMcpServerNotFoundForProject = errors.New("mcp server not found for project")

	// ErrMcpServerNotPublishable reports that the MCP server is disabled or has
	// no endpoint through which the default plugin can reach it.
	ErrMcpServerNotPublishable = errors.New("mcp server not publishable")
)

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

	return attachToPlugin(ctx, repo.New(tx), ensured.Plugin, ensured.Created, params)
}

// AttachToExistingDefaultPluginAudited attaches one MCP server to a project's
// existing default plugin and writes the matching plugin-server audit event in
// the caller's transaction. It never creates or promotes a plugin.
func AttachToExistingDefaultPluginAudited(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, authCtx *contextvalues.AuthContext, organizationID string, projectID, mcpServerID uuid.UUID, displayName string) (*AttachToDefaultPluginResult, error) {
	q := repo.New(tx)

	plugin, err := q.GetDefaultPlugin(ctx, repo.GetDefaultPluginParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrDefaultPluginNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get existing default plugin: %w", err)
	}

	server, err := q.GetMcpServerForPluginServer(ctx, repo.GetMcpServerForPluginServerParams{
		McpServerID: mcpServerID,
		ProjectID:   projectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMcpServerNotFoundForProject
		}
		return nil, fmt.Errorf("verify mcp server project ownership: %w", err)
	}
	// Unproxied-backed servers are never proxied, so they never gain an
	// mcp_endpoints row; exempt them from the has_endpoint requirement.
	if server.Visibility == visibility.Disabled || (!server.HasEndpoint && !server.IsUnproxied) {
		return nil, ErrMcpServerNotPublishable
	}

	attached, err := attachToPlugin(ctx, q, plugin, false, AttachToDefaultPluginParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		ToolsetID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID:    uuid.NullUUID{UUID: mcpServerID, Valid: true},
		DisplayName:    displayName,
	})
	if err != nil || attached == nil {
		return attached, err
	}

	mcpServerURN := urn.NewMcpServer(mcpServerID)
	if err := auditLogger.LogPluginServerAdd(ctx, tx, audit.LogPluginServerAddEvent{
		OrganizationID:    organizationID,
		ProjectID:         projectID,
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
		ToolsetURN:        nil,
		McpServerURN:      &mcpServerURN,
	}); err != nil {
		return nil, fmt.Errorf("audit existing default plugin server add: %w", err)
	}

	return attached, nil
}

func attachToPlugin(ctx context.Context, q *repo.Queries, plugin repo.Plugin, pluginCreated bool, params AttachToDefaultPluginParams) (*AttachToDefaultPluginResult, error) {
	// Check for an existing attachment before inserting rather than relying
	// on unique-violation classification alone: a duplicate insert of an
	// attached server trips the (plugin_id, display_name) index (created
	// before the backend ones, so Postgres reports it first) and the failed
	// statement aborts the caller's surrounding transaction either way.
	_, err := q.GetPluginServerByBackend(ctx, repo.GetPluginServerByBackendParams{
		PluginID:    plugin.ID,
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

	// A different attached server (a same-named toolset row, or a stale row
	// from a deleted server) may already hold the display name, which the
	// (plugin_id, display_name) unique index spans across backends. Blocking
	// the attach — and with it the triggering action, e.g. enabling a server —
	// over a marketplace display name is disproportionate, so uniquify instead.
	displayName, err := availableDisplayName(ctx, q, plugin.ID, params)
	if err != nil {
		return nil, err
	}

	server, err := q.AddPluginServer(ctx, repo.AddPluginServerParams{
		PluginID:    plugin.ID,
		ToolsetID:   params.ToolsetID,
		McpServerID: params.McpServerID,
		DisplayName: displayName,
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
				// display_name still collided after the availability check —
				// a concurrent attach of a same-named server won the race.
				// The failed insert aborts the surrounding transaction; a
				// retry sees the winner via the check above and uniquifies.
			}
		}
		return nil, fmt.Errorf("attach server to default plugin: %w", err)
	}

	return &AttachToDefaultPluginResult{
		PluginID:      plugin.ID,
		PluginName:    plugin.Name,
		PluginSlug:    plugin.Slug,
		PluginCreated: pluginCreated,
		Server:        server,
	}, nil
}

// displayNameCandidates bounds how many names availableDisplayName probes
// before giving up. Reaching the bound needs every candidate to be occupied,
// which takes deliberately crafted names: the first suffixed candidate is
// already specific to this backend.
const displayNameCandidates = 8

// availableDisplayName returns the first display name free on the plugin,
// starting from the requested one. Display names are unique per plugin across
// backends and are user-editable (UpdatePluginServer), so neither the
// requested name nor any single derived candidate is guaranteed free —
// probing a deterministic ladder keeps a marketplace label from failing the
// caller's triggering action, e.g. enabling a server.
func availableDisplayName(ctx context.Context, q *repo.Queries, pluginID uuid.UUID, params AttachToDefaultPluginParams) (string, error) {
	suffix := backendIDSuffix(params)

	for attempt := range displayNameCandidates {
		candidate := params.DisplayName
		switch {
		case attempt == 1:
			candidate = fmt.Sprintf("%s (%s)", params.DisplayName, suffix)
		case attempt > 1:
			candidate = fmt.Sprintf("%s (%s %d)", params.DisplayName, suffix, attempt)
		}

		taken, err := q.PluginServerDisplayNameExists(ctx, repo.PluginServerDisplayNameExistsParams{
			PluginID:    pluginID,
			ProjectID:   params.ProjectID,
			DisplayName: candidate,
		})
		if err != nil {
			return "", fmt.Errorf("check default plugin display name availability: %w", err)
		}
		if !taken {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("no available display name derived from %q on plugin %s after %d candidates", params.DisplayName, pluginID, displayNameCandidates)
}

// backendIDSuffix returns the last hex characters of the backend id (the
// toolset or mcp_server, exactly one of which is set) used to uniquify a
// colliding display name — the same suffix mcpservers bakes into server slugs,
// so the two stay recognizable as the same server.
func backendIDSuffix(params AttachToDefaultPluginParams) string {
	id := params.McpServerID.UUID
	if params.ToolsetID.Valid {
		id = params.ToolsetID.UUID
	}
	s := id.String()
	return s[len(s)-4:]
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
