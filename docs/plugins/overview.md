---
cwd: ../..
---

# Plugins

Plugins are distributable bundles of MCP servers (and observability hooks) that org admins create in Gram and publish to AI coding platforms. Once published, team members install the plugin through Claude Code, Cursor, or Codex's native plugin marketplace instead of configuring MCP servers manually.

## What a plugin is

A plugin aggregates one or more Gram toolsets under a single named unit. Admins configure which MCP servers are included, whether each is `required` or `optional`, and which roles/users are assigned to receive it. Gram then generates platform-specific package files and publishes them to a GitHub repo that each platform's marketplace indexes.

```
Org admin                     Gram                        GitHub repo (auto-managed)
  │                             │                                │
  ├─ create plugin "AI Tools"   │                                │
  ├─ add 3 MCP servers          │                                │
  ├─ assign to role:engineers ──► publishPlugins() ─────────────► push .claude-plugin/
  │                             │   mint API keys                   .cursor-plugin/
  │                             │   generate ZIPs                   .codex-plugin/
  │                             │   store marketplace token         marketplace.json
  │                             │                                │
Team member installs from Claude/Cursor/Codex marketplace
```

## Key concepts

**Toolsets as plugin servers.** Each MCP server inside a plugin maps to a Gram toolset with MCP enabled. The toolset's MCP URL is resolved at publish time and embedded in the generated config.

**Server policy.** Each server is `required` (always installed) or `optional` (user can opt out). This is expressed in the per-platform plugin manifest.

**Assignments.** Plugins are assigned to principals using URN strings:

- `*` — all users in the org
- `role:engineers` — all members of a named role
- `user:<uuid>` — a specific user

Assignments control who sees the plugin in their marketplace; RBAC (`mcp:connect` scope) still enforces access at the MCP entrypoint.

**Observability plugin.** Every publish automatically includes a per-org observability plugin (one for Claude, one for Cursor) that bundles hooks forwarding tool-use events back to Gram. This is required for proper audit logging.

**Scoped API keys.** At publish time, Gram mints two API keys and embeds them in the generated configs:

- A `consumer`-scoped key for MCP access
- A `hooks`-scoped key embedded in the hook script

These keys are per-project and rotated on each publish.

## Database model

| Table                       | Purpose                                                                         |
| --------------------------- | ------------------------------------------------------------------------------- |
| `plugins`                   | Core plugin record (name, slug, description, project scope)                     |
| `plugin_servers`            | MCP servers included in a plugin (toolset FK, display name, policy, sort order) |
| `plugin_assignments`        | Role/user assignments for distribution (principal URNs)                         |
| `plugin_github_connections` | GitHub publishing config per project (installation ID, repo, marketplace token) |

`plugins` and `plugin_servers` use soft-delete (`deleted_at` / `deleted` computed column). `plugin_assignments` has no soft-delete — replacements are managed atomically via `setPluginAssignments`.

Slugs must be unique per `(organization_id, project_id)` (filtered index, ignores soft-deleted rows).

## API surface

All endpoints live under `/rpc/plugins.<method>` and require session auth + `Gram-Project` header.

| Endpoint                      | Auth scope | Description                                                      |
| ----------------------------- | ---------- | ---------------------------------------------------------------- |
| `listPlugins`                 | `OrgRead`  | List plugins with server/assignment counts                       |
| `getPlugin`                   | `OrgRead`  | Full plugin detail (nested servers + assignments)                |
| `createPlugin`                | `OrgAdmin` | Create plugin (slug auto-derived from name if omitted)           |
| `updatePlugin`                | `OrgAdmin` | Rename/re-slug/redescribe                                        |
| `deletePlugin`                | `OrgAdmin` | Soft-delete plugin + all its servers                             |
| `addPluginServer`             | `OrgAdmin` | Add toolset to plugin                                            |
| `updatePluginServer`          | `OrgAdmin` | Change display name, policy, sort order                          |
| `removePluginServer`          | `OrgAdmin` | Remove server from plugin                                        |
| `setPluginAssignments`        | `OrgAdmin` | Replace all principal assignments (atomic)                       |
| `downloadPluginPackage`       | `OrgRead`  | Download single-plugin ZIP for `claude`, `cursor`, or `codex`    |
| `downloadObservabilityPlugin` | `OrgAdmin` | Download per-org observability plugin ZIP (mints hooks API key)  |
| `getPublishStatus`            | `OrgRead`  | Check GitHub config + connection status; returns marketplace URL |
| `publishPlugins`              | `OrgAdmin` | Generate all plugins, push to GitHub, mint API keys              |

## Implementation

| Path                                           | Contents                                                   |
| ---------------------------------------------- | ---------------------------------------------------------- |
| `server/design/plugins/design.go`              | Goa service definition (types, endpoints, security)        |
| `server/internal/plugins/impl.go`              | Service logic — all 13 endpoint implementations            |
| `server/internal/plugins/generate.go`          | Package generation — platform-specific file/ZIP generation |
| `server/internal/plugins/marketplace_token.go` | 256-bit base64url token for marketplace proxy              |
| `server/internal/plugins/repo/`                | SQLc-generated database access                             |
| `server/internal/audit/plugins.go`             | 8 audit event definitions                                  |
| `client/dashboard/src/pages/plugins/`          | React UI (list, detail, publish, install instructions)     |

## Audit events

All mutations emit an audit event with actor, action, subject (plugin), and org/project scope.

| Action                   | Fired when                                               |
| ------------------------ | -------------------------------------------------------- |
| `plugin:create`          | New plugin created                                       |
| `plugin:update`          | Name/slug/description changed (before + after snapshots) |
| `plugin:delete`          | Plugin soft-deleted                                      |
| `plugin:server_add`      | MCP server added to plugin                               |
| `plugin:server_update`   | Server display name / policy / sort order changed        |
| `plugin:server_remove`   | Server removed                                           |
| `plugin:assignments_set` | Assignments replaced                                     |
| `plugin:publish`         | Plugins published to GitHub                              |

## Related docs

- [Local development](./local-development.md) — env vars, GitHub App setup, running locally
- [Package format](./package-format.md) — exact file layout Gram generates per platform
- [Publishing](./publishing.md) — end-to-end publish flow, marketplace tokens, GitHub integration
