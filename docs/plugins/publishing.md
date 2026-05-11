---
cwd: ../..
---

# Plugins — GitHub Publishing

This doc covers how the publish flow works end-to-end: what Gram generates, how the GitHub repo is managed, and how the marketplace URL is constructed and served.

## Overview

"Publishing" is the act of generating all plugin package files and pushing them to a GitHub repo that each AI platform's marketplace can index. The GitHub repo is fully managed by Gram — it is read-only to end users and overwritten on every publish.

## Triggering a publish

From the dashboard: **Plugins → Publish to GitHub**. Optionally enter a GitHub username to add as repo collaborator.

Via API:

```
POST /rpc/plugins.publishPlugins
Gram-Session: <session>
Gram-Project: <project-slug>

{}
```

Or with a collaborator:

```json
{ "github_collaborator": "octocat" }
```

## What happens during a publish

1. **Resolve plugin data.** Query all non-deleted plugins for the project, with their servers and toolset metadata (MCP URLs, public/private status, env configs).

2. **Mint API keys.** Two project-scoped keys are created (or the existing pair is rotated):
   - `consumer`-scoped key for MCP access — embedded in server configs
   - `hooks`-scoped key for observability — embedded in the hook script

3. **Generate files.** `GeneratePluginPackages()` builds a `map[string][]byte` with all platform configs, marketplace manifests, README, and hook scripts. See [Package Format](./package-format.md) for the exact file tree.

4. **Push to GitHub.** The server calls `CreateRepo()` (no-op if repo exists) then `PushFiles()` with a single commit. The repo name is `<project-slug>-plugins` under the configured org.

5. **Mint marketplace token.** On first publish, a 256-bit base64url token is generated and stored in `plugin_github_connections.marketplace_token`. This token becomes part of the marketplace proxy URL served by Gram:

   ```
   https://app.getgram.ai/m/<token>/marketplace.json
   ```

6. **Store connection.** The `plugin_github_connections` row is upserted with `(project_id, installation_id, repo_owner, repo_name)`.

7. **Add collaborator.** If a GitHub username was provided, `AddCollaborator()` grants `push` permission to the repo.

> **Keys are minted before the GitHub push.** If the push fails, the keys are discarded and not persisted to the database. On retry the flow starts over.

## GitHub App topology

| Environment | GitHub App          | Org                 | Secrets location                                           |
| ----------- | ------------------- | ------------------- | ---------------------------------------------------------- |
| dev         | Non-prod app        | Non-prod org        | Non-prod GCP Secret Manager (`dev_gram_plugins_github_*`)  |
| test        | Non-prod app (same) | Non-prod org (same) | Non-prod GCP Secret Manager (`test_gram_plugins_github_*`) |
| prod        | Prod app            | Prod org            | Prod GCP Secret Manager (`prod_gram_plugins_github_*`)     |

Dev and test share one GitHub App and one org. This means plugin repos published from dev and test land in the same org — no namespace collision risk because project slugs are globally unique across environments.

## Marketplace proxy

The URL `https://app.getgram.ai/m/<token>/marketplace.json` is served by a dedicated marketplace proxy endpoint. The token resolves to a `plugin_github_connections` row, which identifies the project whose latest-published `marketplace.json` to serve.

Gram does **not** proxy raw GitHub traffic — it serves the cached published content (or generates it on demand from DB state). The marketplace URL token is opaque: no org or project identity is embedded.

## Re-publishing

Every publish is a full overwrite of the GitHub repo. There is no incremental update. This means:

- Adding/removing servers from a plugin takes effect on the next publish
- Renamed plugins get new directory entries; old directories are removed
- API keys are rotated on every publish

## `getPublishStatus`

Use this to check the current state before showing publish UI:

```
GET /rpc/plugins.getPublishStatus
```

Response:

```json
{
  "configured": true,
  "connected": true,
  "repo_owner": "speakeasy-plugins",
  "repo_name": "my-project-plugins",
  "repo_url": "https://github.com/speakeasy-plugins/my-project-plugins",
  "marketplace_url": "https://app.getgram.ai/m/abc123def456.../marketplace.json"
}
```

| Field             | Meaning                                                                                 |
| ----------------- | --------------------------------------------------------------------------------------- |
| `configured`      | Server has GitHub env vars set (publishing is possible)                                 |
| `connected`       | This project has a `plugin_github_connections` row (publish has happened at least once) |
| `repo_url`        | Direct link to the GitHub repo                                                          |
| `marketplace_url` | Marketplace proxy URL (non-null once connected)                                         |

If `configured` is false, the Publish button should be hidden (the server isn't set up for GitHub publishing).

## Observability plugin

Every publish includes two observability plugins (one for Claude, one for Cursor) that forward hook events to Gram. These are automatically added at the top of the marketplace so they appear first.

The observability plugin slug is `<org-slug>-observability` (Claude) or `<org-slug>-observability-cursor` (Cursor). Users are shown a notice in the README that this plugin is required alongside any MCP server plugins.

See [Package Format — observability plugin](./package-format.md#observability-plugin) for the hook events registered and the hook script contents.
