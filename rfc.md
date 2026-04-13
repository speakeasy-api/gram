# Plugins

# TLDR

- Both Claude Code and Cursor plugins can **bundle MCP servers alongside hooks** in a single installable package
- Both platforms support **org/team marketplace distribution** with auto-install and per-team visibility controls
- Gram should let admins create **Plugins** -- named groups of MCP servers assigned to roles -- and generate platform-specific plugin packages from them
- Generated plugins are pushed to a **GitHub repo** that the admin connects to their org's Claude Code and/or Cursor marketplace
- Each plugin includes **hooks (observability) + MCP servers (tooling)** so teams get everything from a single install
- Plugin assignments reuse **RBAC principal URNs** (`role:engineering`, `user:id`, `*`) for consistency with existing access controls

# Overview

A core pain point for teams adopting AI coding assistants is MCP server distribution. Today, setting up MCP servers is a manual, per-person, per-platform process. A team lead who wants their sales org to have access to CRM and outreach MCP servers -- and their engineering org to have database and CI/CD servers -- has no way to do this at scale. Each person must individually install each server on each platform they use.

Gram already hosts and proxies MCP servers, and our Hooks feature provides observability into MCP usage. But there's no mechanism to say "these 5 servers should be available to everyone in engineering" and have that automatically configured across Claude Code, Cursor, and other platforms.

Both Claude Code and Cursor now support **plugins that bundle MCP servers alongside hooks**. Both also have **team/org marketplace distribution** that lets admins push plugins to their teams with auto-install and per-team visibility controls. This means we can solve the distribution problem by generating platform-specific plugins from a single configuration in Gram.

# Goals

- Let admins select groups of Gram MCP servers and distribute them to specific teams or roles
- Auto-configure current and future employees' AI environments via platform marketplace distribution
- Support different server groups for different teams (e.g. GTM vs Engineering)
- Distribute the same logical configuration across Claude Code and Cursor (Copilot in the future)
- Support required servers (auto-installed) and optional servers (available for one-click install)
- Bundle all Gram capabilities (MCP servers + hooks) into a single plugin per platform
- Interoperate with Gram's existing RBAC system

# Proposal

## How It Works

An admin creates a **Plugin** in Gram's dashboard. A Plugin is a named, distributable configuration that contains:

1. **MCP servers** -- selected from the org's existing Gram toolsets, catalog servers, or external URLs
2. **Hooks** -- Gram's observability hooks (PreToolUse, PostToolUse, etc.), included automatically
3. **Metadata** -- name, description, version, targeting (which roles/teams receive it)

When the admin saves or updates a Plugin, Gram generates platform-specific plugin packages. These are published to a GitHub repository that the admin connects to their org's Claude Code or Cursor team marketplace. From there, the platform handles distribution -- auto-installing for team members, syncing updates, etc.

```
Admin creates Plugin in Gram
  -> selects MCP servers (toolsets, catalog, external)
  -> assigns to roles (e.g. role:engineering, role:gtm)
  -> Gram generates plugin packages

Gram pushes to GitHub repo
  -> Claude Code plugin (plugin.json + .mcp.json + hooks/)
  -> Cursor plugin (plugin.json + hooks/ + mcp config)

Admin connects GitHub repo to platform marketplace
  -> Claude Code: org marketplace with auto-install
  -> Cursor: team marketplace with access groups

Team members get the plugin automatically
  -> MCP servers configured, hooks active
```

## Plugin Composition

A generated plugin contains everything needed for a specific platform:

### Claude Code Plugin Structure

```
gram-plugin/
  .claude-plugin/
    plugin.json        # Plugin metadata
  .mcp.json            # MCP server declarations
  hooks/
    hooks.json         # Hook registrations (PreToolUse, PostToolUse, etc.)
    send_hook.sh       # Hook forwarding script (existing)
```

**`.mcp.json`** (generated from admin's server selections):

```json
{
  "mcpServers": {
    "crm-tools": {
      "type": "http",
      "url": "https://app.getgram.ai/mcp/acme-corp/crm-tools/prod",
      "headers": {
        "Authorization": "Bearer ${GRAM_API_KEY}"
      }
    },
    "outreach-tools": {
      "type": "http",
      "url": "https://app.getgram.ai/mcp/acme-corp/outreach/prod",
      "headers": {
        "Authorization": "Bearer ${GRAM_API_KEY}"
      }
    },
    "external-analytics": {
      "type": "http",
      "url": "https://analytics.example.com/mcp",
      "headers": {
        "Authorization": "Bearer ${ANALYTICS_API_KEY}"
      }
    }
  }
}
```

### Cursor Plugin Structure

```
gram-plugin/
  .cursor-plugin/
    plugin.json        # Plugin metadata
  mcp.json             # MCP server declarations
  hooks/
    hooks.json         # Hook registrations (preToolUse, postToolUse, etc.)
    send_hook.sh       # Hook forwarding script (existing)
```

**`mcp.json`**:

```json
{
  "mcpServers": {
    "crm-tools": {
      "url": "https://app.getgram.ai/mcp/acme-corp/crm-tools/prod",
      "headers": {
        "Authorization": "Bearer ${env:GRAM_API_KEY}"
      }
    }
  }
}
```

Note the differences: Cursor uses `${env:VAR}` syntax for environment variables (vs Claude Code's `${VAR}`), and does not require a `type` field for HTTP transport.

## Data Model

### `plugins`

A Plugin is an org-scoped configuration that represents a distributable bundle. It is the entity admins create and manage.

| Column                             | Type        | Notes                                   |
| ---------------------------------- | ----------- | --------------------------------------- |
| id                                 | uuid        | PK                                      |
| organization_id                    | text        | FK -> organization_metadata             |
| name                               | text        | Display name (e.g. "Engineering Tools") |
| slug                               | text        | URL-safe identifier, unique per org     |
| description                        | text        | Optional                                |
| created_at, updated_at, deleted_at | timestamptz | Soft delete pattern                     |

### `plugin_servers`

Links a Plugin to MCP servers. Each row represents one server included in the plugin. Exactly one source type column should be set per row.

| Column                    | Type | Notes                                                    |
| ------------------------- | ---- | -------------------------------------------------------- |
| id                        | uuid | PK                                                       |
| plugin_id                 | uuid | FK -> plugins                                            |
| toolset_id                | uuid | Nullable -- first-party Gram MCP server                  |
| registry_id               | uuid | Nullable -- catalog server (FK -> mcp_registries)        |
| registry_server_specifier | text | Nullable -- e.g. `io.modelcontextprotocol.anonymous/exa` |
| external_url              | text | Nullable -- arbitrary external MCP URL                   |
| display_name              | text | Name shown in generated plugin config                    |
| policy                    | text | `required` or `optional` (default: `required`)           |
| sort_order                | int  | Ordering within the plugin                               |

### `plugin_assignments`

Controls who receives the plugin. Reuses the same principal URN pattern as Gram's RBAC system (`role:slug`, `user:id`, or `*` for all org members).

| Column            | Type | Notes                                       |
| ----------------- | ---- | ------------------------------------------- |
| id                | uuid | PK                                          |
| plugin_id         | uuid | FK -> plugins                               |
| organization_id   | text | FK -> organization_metadata                 |
| principal_urn     | text | `role:engineering`, `user:abc`, or `*`      |
| Unique constraint |      | (plugin_id, organization_id, principal_urn) |

### Relationship to Existing Entities

- **Plugins are org-scoped**, not project-scoped. A single plugin can reference toolsets across multiple projects.
- **Plugin assignments reuse RBAC's principal URN pattern**. If an admin assigns a plugin to `role:engineering`, the same WorkOS role governs both "who gets the plugin" and "who can use these MCP servers" (via `mcp:connect` grants).
- **RBAC remains the enforcement layer.** Plugin assignment controls distribution/discovery. RBAC grants (`mcp:connect`) control runtime access. If a server is in a plugin but the user lacks `mcp:connect` for it, it should be excluded from the generated plugin.

## Distribution Flow

Both Claude Code and Cursor marketplaces consume plugins from GitHub repositories. This means we need a way to get generated plugin packages into a GitHub repo. There are several approaches, each with different tradeoffs.

### Option A: Gram-Managed GitHub Repo (Recommended)

Gram creates and manages the GitHub repo on behalf of the org via a GitHub App.

**Admin experience:**

1. Admin clicks "Set up distribution" in Gram's Plugin dashboard
2. Gram prompts the admin to install the Gram GitHub App on their GitHub org (standard OAuth flow)
3. Admin selects which GitHub org to use, or lets Gram create a new repo (e.g. `acme-corp/gram-plugins`)
4. Gram creates the repo (private by default), generates plugin packages, and pushes them
5. Gram provides a link/instructions to connect the repo to Claude Code and/or Cursor marketplace
6. For Claude Code: admin pastes the repo URL into their org's marketplace settings (Anthropic admin console)
7. For Cursor: admin goes to Cursor dashboard -> Settings -> Plugins -> Import, pastes the repo URL

**Ongoing updates are fully automatic:**

- Admin changes a Plugin in Gram -> Gram pushes updated packages to the repo -> platform picks up the change

**What Gram does behind the scenes:**

- Creates the repo via GitHub API (using the installed GitHub App)
- Generates platform-specific plugin directories (Claude Code + Cursor variants)
- Generates the `marketplace.json` manifest
- Commits and pushes on every Plugin change
- Manages repo structure, branch, and commit history

**Pros:** Smoothest experience, fully automated updates, admin only touches GitHub once (App install)
**Cons:** Requires GitHub App permissions, admin must have GitHub org admin access

### Option B: Admin-Provided GitHub Repo

Admin provides an existing repo and grants Gram push access.

**Admin experience:**

1. Admin creates a GitHub repo (or uses an existing one)
2. In Gram's dashboard, admin enters the repo URL
3. Gram prompts the admin to install the Gram GitHub App (or add a deploy key) for push access
4. Gram pushes generated plugin packages to the repo
5. Admin connects the repo to their platform marketplace(s)

**Pros:** Admin controls the repo (naming, visibility, org placement)
**Cons:** More setup steps, admin must manage repo permissions

### Option C: Manual Download (No GitHub Required)

For orgs that don't use GitHub, or where GitHub App installation isn't feasible.

**Admin experience:**

1. Admin creates Plugins in Gram's dashboard
2. Admin clicks "Download plugin package" -> Gram generates a `.zip` containing all plugin directories
3. Admin manually commits the contents to whatever Git hosting they use (GitHub, GitLab, Bitbucket, etc.)
4. Admin connects the repo to their platform marketplace(s)
5. When Plugins change, admin downloads the updated package and re-commits

**Pros:** No GitHub dependency, works with any Git host
**Cons:** Manual process for updates, risk of drift between Gram config and distributed plugin

### Option D: Gram-Hosted Marketplace (Future)

If Claude Code or Cursor eventually support marketplace sources other than GitHub (e.g. a URL-based manifest), Gram could host the marketplace directly -- eliminating the GitHub middleman entirely. This is speculative but worth tracking as platform capabilities evolve.

### Recommendation

Option A (Gram-managed repo) should be the primary path. It's the smoothest admin experience and the only one that keeps plugins automatically in sync with Gram. Option C (manual download) should be available as a fallback for orgs without GitHub access.

### Per-Platform Distribution Details

**Claude Code:**

- Org marketplace supports auto-install, per-team visibility, and strict marketplace controls
- Enterprise admins can push plugins automatically to specific teams
- Supports GitHub Enterprise Server for private hosting
- Plugin settings can be configured in `.claude/settings.json` for per-project defaults
- Admin connects the repo via the Anthropic admin console (org settings -> plugin marketplaces)

**Cursor:**

- Team marketplaces via GitHub repo import with per-team access groups
- Teams plan: 1 marketplace; Enterprise plan: unlimited marketplaces
- Currently requires public GitHub repos (private/GHE support may be limited)
- Admin connects the repo via Cursor dashboard -> Settings -> Plugins -> Import
- **Important caveat**: if Cursor requires public repos, this conflicts with orgs that want private plugin configs. Options: make the repo public (plugin configs don't contain secrets -- API keys are env var references), or advocate for Cursor to support private repos

### Repository Structure

For an org with separate engineering and GTM plugins:

```
acme-corp/gram-plugins/
  marketplace.json              # Marketplace metadata
  engineering-tools/
    .claude-plugin/
      plugin.json
    .mcp.json
    hooks/
      hooks.json
      send_hook.sh
  gtm-tools/
    .claude-plugin/
      plugin.json
    .mcp.json
    hooks/
      hooks.json
      send_hook.sh
  engineering-tools-cursor/
    .cursor-plugin/
      plugin.json
    mcp.json
    hooks/
      hooks.json
      send_hook.sh
  gtm-tools-cursor/
    .cursor-plugin/
      plugin.json
    mcp.json
    hooks/
      hooks.json
      send_hook.sh
```

The marketplace manifest (Claude Code format):

```json
{
  "name": "acme-corp-gram",
  "owner": { "name": "Acme Corp", "email": "admin@acme.com" },
  "plugins": [
    {
      "name": "engineering-tools",
      "source": "./engineering-tools",
      "description": "MCP servers for the engineering team"
    },
    {
      "name": "gtm-tools",
      "source": "./gtm-tools",
      "description": "MCP servers for the GTM team"
    }
  ]
}
```

## Required vs Optional Servers

The `policy` field on `plugin_servers` controls behavior:

- **`required`**: Server is always included in the generated plugin. When distributed via auto-install, the user gets this server with no action needed.
- **`optional`**: Server is included in the plugin but may be presented differently depending on platform capabilities. On platforms that support toggling individual MCP servers within a plugin, optional servers could start disabled. On platforms that don't, optional servers would need a separate plugin.

For MVP, we may treat all servers as required within a given plugin, and use separate plugins to model "required engineering tools" vs "optional engineering tools."

## Hooks Integration

Every generated plugin includes Gram's observability hooks. This means installing a single plugin gives the user both their MCP servers and hook-based monitoring -- no separate setup required.

The hooks are identical to what we ship today in the `gram-hooks` plugin:

- **Claude Code**: PreToolUse, PostToolUse, PostToolUseFailure hooks forwarding to `/rpc/hooks.claude`
- **Cursor**: preToolUse, postToolUse, postToolUseFailure hooks forwarding to `/rpc/hooks.cursor`

This consolidation means we evolve from distributing a standalone `gram-hooks` plugin to distributing a unified plugin per team that includes hooks + MCP servers.

## RBAC Integration

Plugin assignment and RBAC are complementary:

| Concern                                   | Mechanism                          |
| ----------------------------------------- | ---------------------------------- |
| "Who should have this plugin?"            | Plugin assignments (principal URN) |
| "Who can use this MCP server at runtime?" | RBAC grants (`mcp:connect` scope)  |

When generating a plugin for a specific role, Gram should cross-reference the RBAC grants to ensure the plugin only includes servers that the role actually has `mcp:connect` access to. This prevents a mismatch where a user receives a plugin containing servers they can't actually use.

In practice, admins will likely configure both in tandem: "role:engineering gets the Engineering Tools plugin" and "role:engineering has mcp:connect for project X, Y, Z."

## Reconciliation

When an admin updates a Plugin (adds or removes servers), the regenerated plugin package replaces the previous version in the GitHub repo. The platform marketplace picks up the change, and users receive the updated configuration.

This is a **reconcile** model, not additive-only:

- New servers are added to the user's environment
- Removed servers are removed from the user's environment
- The plugin is the source of truth for which servers should be present

This is the correct behavior because the admin's intent is declarative ("this team should have exactly these servers"). However, it means removing a server from a Plugin will remove it from all assigned users. The dashboard should surface clear confirmation when removing servers.

# User Experience

## Admin Flow

1. Navigate to **Plugins** in the Gram dashboard
2. Click **Create Plugin** -> enter name, description
3. **Add servers**: browse org's toolsets, search the catalog, or paste an external URL
4. For each server, optionally set policy (required/optional) and display name
5. **Assign to roles**: select which roles or users should receive this plugin
6. **Connect repository**: link a GitHub repo where Gram will push generated plugins
7. **Publish**: Gram generates Claude Code and Cursor plugin packages, pushes to the repo
8. Admin connects the repo to their org's Claude Code and/or Cursor marketplace (one-time setup per platform)

## End-User Experience

1. User joins the org or is assigned a role
2. Platform marketplace auto-installs the plugin (or prompts the user to install)
3. User opens Claude Code or Cursor -- MCP servers are configured, hooks are active
4. No manual setup required

# Alternative Approaches

## Meta-Proxy MCP Server

Instead of generating static plugin packages, Gram could expose a single MCP endpoint (`/plugin/mcp/{org_slug}`) that dynamically aggregates tools from all servers assigned to a user. The plugin would declare this one endpoint, and Gram would handle routing tool calls to the correct upstream server.

**Pros:**

- Truly dynamic -- changes take effect immediately without GitHub pushes
- Single MCP connection regardless of how many servers are assigned
- Simplifies the plugin package (just one MCP server declaration)

**Cons:**

- Adds latency (extra hop for every tool call, especially for external servers)
- Single point of failure -- if Gram is down, all tool calls fail
- More complex server-side implementation
- Doesn't leverage platform marketplace features designed for multi-server plugins

This approach may be worth revisiting if real-time configuration changes become a priority.

## SessionStart Hook Config Writing

The plugin's SessionStart hook could call Gram's API to resolve the user's assigned servers, then write MCP configs directly to local files (`.mcp.json`, `~/.cursor/mcp.json`).

**Pros:**

- No extra latency on tool calls (configs are local)
- Dynamic -- re-resolves on each session start

**Cons:**

- Requires file system write access from a hook script
- Mixes plugin-managed and user-managed MCP configs in the same files
- Harder to reconcile (what if the user manually edited the config?)
- Platform-specific file paths and formats

# Open Questions

- **GitHub App scope**: What permissions does the Gram GitHub App need? Likely just repo creation + write access to the specific plugins repo. Should we request org-level install or per-repo?
- **Plugin-per-role vs plugin-per-org**: Should each role get its own plugin (allowing marketplace-level visibility controls), or should one plugin contain all servers with role-based filtering? Platform marketplace features may influence this.
- **Environment variables**: How should API keys and other secrets be distributed? Env vars in the plugin config use `${GRAM_API_KEY}` syntax, but the actual key value needs to come from somewhere. Options: admin sets it via platform's env var management, or Gram provides a setup script.
- **Existing `gram-hooks` plugin migration**: Users who already have the standalone `gram-hooks` plugin installed will need a migration path to the unified plugin. Should we keep backward compatibility or require a clean switch?
- **Cursor private repo support**: Cursor team marketplaces currently only support public GitHub repos. For orgs that need private plugin configs, we may need an alternative distribution method for Cursor.
- **Optional server UX**: How do users discover and enable optional servers? This likely requires platform-specific solutions, as neither Claude Code nor Cursor has a standardized "optional MCP server" concept within plugins.
