---
cwd: ../..
---

# Plugins — Local Development

This runbook covers how to set up the Plugins feature for local development, including optional GitHub publishing support.

## Prerequisites

The standard `./zero --agent` setup is sufficient to run the dashboard and create/edit plugins. You only need the GitHub App env vars if you want to test the **publish-to-GitHub** flow.

## Environment variables

All plugin-related env vars live in `mise.toml` (commented out by default) and should be overridden in `mise.local.toml` (gitignored).

### Core server vars (always set)

These are already set by `mise.toml` and don't need changes for basic plugin use:

| Variable          | Default                  | Purpose                                 |
| ----------------- | ------------------------ | --------------------------------------- |
| `GRAM_SERVER_URL` | `https://local.gram.dev` | Base URL embedded in generated MCP URLs |

### GitHub publishing vars (optional)

Leave these unset to run without GitHub publishing. Setting **some but not all** of them will cause the server to fail at startup — it's all-or-nothing.

Add to `mise.local.toml`:

```toml
[env]
GRAM_PLUGINS_GITHUB_APP_ID       = "<int64 app ID>"
GRAM_PLUGINS_GITHUB_PRIVATE_KEY  = "<PEM-encoded private key>"
GRAM_PLUGINS_GITHUB_ORG          = "<GitHub org where plugin repos are created>"
GRAM_PLUGINS_GITHUB_INSTALLATION_ID = "<int64 installation ID>"
```

| Variable                              | Description                                                                       |
| ------------------------------------- | --------------------------------------------------------------------------------- |
| `GRAM_PLUGINS_GITHUB_APP_ID`          | Numeric GitHub App ID (found on the App's settings page)                          |
| `GRAM_PLUGINS_GITHUB_PRIVATE_KEY`     | PEM-encoded private key generated for the App. May contain literal newlines.      |
| `GRAM_PLUGINS_GITHUB_ORG`             | GitHub org name where plugin repos will be created (e.g. `speakeasy-plugins-dev`) |
| `GRAM_PLUGINS_GITHUB_INSTALLATION_ID` | Numeric installation ID for the App installed on the target org                   |

> **Dev/test share one GitHub App.** The non-prod App is installed on the non-prod plugin-host org. Dev and test both point at the same App ID, private key, org, and installation ID. Only prod has its own isolated App. See [Publishing](./publishing.md) for details.

## Getting the GitHub App credentials

For **dev/test** environments, the credentials are stored in GCP Secret Manager. Ask a team member for the project ID and secret names, then pull them:

```bash
gcloud secrets versions access latest \
  --secret=<secret-name> \
  --project=<gcp-project-id>
```

Repeat for each of the four secrets, then paste the values into `mise.local.toml`.

## Running locally

Start all services with:

```bash
madprocs
```

Or just the server:

```bash
mise start:server --dev-single-process
```

The Plugins UI is at `http://local.gram.dev:<port>/plugins` once the dashboard is running.

### Verifying GitHub publishing is enabled

Check server startup logs for a line like:

```
component=plugins github_publishing=enabled org=speakeasy-plugins-dev
```

If you see `github_publishing=disabled`, the env vars aren't set. If the server fails to start with an error mentioning `plugin github publishing requires`, you've set some but not all four vars.

## Working with plugins without GitHub

You can fully exercise plugin creation, editing, and per-platform ZIP downloads without setting up the GitHub App:

1. Navigate to **Plugins** in the dashboard
2. Create a plugin (name required, slug auto-generated)
3. Add MCP servers (toolsets with MCP enabled)
4. Configure assignments
5. Use **Download** on the plugin detail page to get a ZIP for Claude, Cursor, or Codex

The **Publish to GitHub** button will be hidden/disabled when the server has no GitHub config.

## Testing the publish flow locally

With GitHub env vars set:

1. Create one or more plugins with at least one MCP server each
2. Click **Publish to GitHub** in the dashboard
3. Optionally enter a GitHub username to add as a collaborator
4. The server will:
   - Mint a `consumer`-scoped API key (prefix `gsk_`) and a `hooks`-scoped key
   - Generate all plugin ZIPs + marketplace.json files
   - Create or update the repo `<org>/<project-slug>-plugins` in the configured GitHub org
   - Store a marketplace token in `plugin_github_connections.marketplace_token`

After publishing, the dashboard shows a GitHub repo link and marketplace installation instructions.

## Running tests

```bash
mise test:server -- ./server/internal/plugins/...
```

Plugin tests live in `server/internal/plugins/` alongside the implementation. They use `testenv` for a real Postgres instance — no mocks. The GitHub publisher is stubbed for unit tests via the `GitHubPublisher` interface.

## Common issues

**"Plugin github publishing requires client, …; missing: …"**
You set some but not all four `GRAM_PLUGINS_GITHUB_*` vars. Set all four or none.

**Toolset not appearing in "Add server" dropdown**
The toolset must have MCP enabled. Go to the toolset settings and toggle MCP on.

**Publish fails with a GitHub API error**
The GitHub App must be installed on the target org with `Contents: Read & Write` permission. Check the App's installation settings and confirm the installation ID matches.

**Duplicate plugin slug error**
Slugs are unique per `(organization_id, project_id)`. If you deleted a plugin with a given slug and want to reuse it, the soft-deleted record blocks reuse — use a different slug or clear the deleted row in dev.
