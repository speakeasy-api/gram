---
cwd: ../..
---

# Plugins — GitHub Publishing

This doc covers how the publish flow works end-to-end: what Speakeasy generates, how the GitHub repo is managed, and how the marketplace URL is constructed and served.

## Overview

"Publishing" is the act of generating all plugin package files and pushing them to a GitHub repo that each AI platform's marketplace can index. The GitHub repo is fully managed by Speakeasy — its contents are overwritten on every publish, so edits made on GitHub do not survive.

## Triggering a publish

From the dashboard: **Plugins → Publish to GitHub**. Optionally enter a GitHub username to add as repo collaborator.

Via API:

```
POST /rpc/plugins.publishPlugins
Gram-Session: <session>
Gram-Project: <project-slug>

{}
```

Or with collaborators:

```json
{ "github_usernames": ["octocat"] }
```

## What happens during a publish

1. **Resolve plugin data.** Query all non-deleted plugins for the project, with their servers and toolset metadata (MCP URLs, public/private status, env configs).

2. **Mint API keys.** Two project-scoped keys are created (or the existing pair is rotated):
   - `consumer`-scoped key for MCP access — embedded in server configs
   - `hooks`-scoped key for observability — embedded in the hook script

3. **Generate files.** `GeneratePluginPackages()` builds a `map[string][]byte` with all platform configs, marketplace manifests, README, and hook scripts. See [Package Format](./package-format.md) for the exact file tree.

4. **Push to GitHub.** On first publish, Speakeasy requests a new private repository named `<org-slug>-<project-slug>-plugins`. If GitHub reports that the name is taken, Speakeasy tries a name with the project UUID appended. It records the repository ID from the successful creation and pushes a single commit with a token restricted to that ID. Later publishes use the saved binding; changing a project slug does not move the repository.

5. **Mint marketplace token.** On first publish, a 256-bit base64url token is generated. The token is never stored in plaintext. `plugin_github_connections` stores its SHA-256 hash (`marketplace_token_hash`), which the marketplace proxy looks it up by, and a sealed copy, which the server opens to show the install URL again (see `server/internal/plugins/marketplacetokens`). This token becomes part of the marketplace proxy URL served by Speakeasy:

   ```
   https://app.getgram.ai/m/<token>/marketplace.json
   ```

6. **Store connection.** The `plugin_github_connections` row is upserted with `(project_id, installation_id, repo_owner, repo_name)`.

7. **Add collaborators.** For each GitHub username provided, `AddCollaborator()` grants `admin` permission on the repo. Admin is what the platform marketplaces need from whoever wires the repo up: Cursor only offers **Serve Marketplace From Cursor** — the synced copy that lets teammates install plugins without GitHub access to the source repo — to repository admins. Re-adding an existing collaborator upgrades them to admin, which is how a repo published before this behavior is brought up to date.

> **Keys are minted before the GitHub push.** If the push fails, the keys are discarded and not persisted to the database. On retry the flow starts over.

## Migrating existing repositories in the staff admin app

A legacy publishing connection identifies a repository by name only. To keep serving the same repository, use **Plugin repository migrations** in the staff admin app. Binding the original repository does not move it or change the customer's install URL.

1. Match the project to the immutable GitHub repository ID using independent historical records. For example, correlate the project's original publish with the GitHub App's repository creation event. A current name match or a screenshot of today's repository is not evidence of that link.
2. Enter or import the project ID, GitHub repository ID **as a string**, and a reference to the historical evidence. Preview the batch. The live check confirms that the supplied ID still points to the expected private repository; staff are responsible for verifying the historical link.
3. Apply the verified rows and check the outcome for each project. An existing binding cannot be redirected to another repository through this workflow.
4. When the original identity cannot be established, leave the connection unbound or provision a **new** private repository for that project. An unbound connection cannot publish or serve marketplace installations. This does not recover the old contents, transfer GitHub collaborators, or update direct GitHub install URLs. Publish packages to the replacement before directing users to it.

If GitHub creates a repository but the request fails before the binding is saved, an unused private repository may remain. Before retrying recovery, refresh the migration inventory and check for a binding. If the project is still unbound, a retry provisions a new repository. Use the GitHub organization’s creation audit records to identify any unused repository for manual review; a repository name alone is not proof that it is safe to reuse or remove.

## GitHub App topology

| Environment | GitHub App          | Org                 | Secrets location                                           |
| ----------- | ------------------- | ------------------- | ---------------------------------------------------------- |
| dev         | Non-prod app        | Non-prod org        | Non-prod GCP Secret Manager (`dev_gram_plugins_github_*`)  |
| test        | Non-prod app (same) | Non-prod org (same) | Non-prod GCP Secret Manager (`test_gram_plugins_github_*`) |
| prod        | Prod app            | Prod org            | Prod GCP Secret Manager (`prod_gram_plugins_github_*`)     |

Dev and test share one GitHub App and one org. Repository names can collide within that namespace; a matching name does not identify the owning project.

## Marketplace proxy

The URL `https://app.getgram.ai/m/<token>/marketplace.json` is served by a dedicated marketplace proxy endpoint. The proxy hashes the token and resolves the hash to a `plugin_github_connections` row, which identifies the project whose latest-published `marketplace.json` to serve. A database read alone does not yield a working install URL: the hash cannot be reversed, and opening the sealed copy needs the platform key or the organization's key. Connections published before tokens were hashed keep the token in plaintext until the marketplace token backfill moves them (see `server/cmd/tools/migrations/MARKETPLACE_TOKENS_BACKFILL.md`).

Speakeasy does **not** proxy raw GitHub traffic — it serves the cached published content (or generates it on demand from DB state). The marketplace URL token is opaque: no org or project identity is embedded.

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

Every publish includes one observability plugin per supported platform, forwarding hook events to Speakeasy. Those with a marketplace are automatically added at the top so they appear first.

| Platform | Slug                                |
| -------- | ----------------------------------- |
| Claude   | `<org-slug>-observability`          |
| Cursor   | `<org-slug>-observability-cursor`   |
| Codex    | `<org-slug>-observability-codex`    |
| OpenCode | `<org-slug>-observability-opencode` |
| OpenClaw | `<org-slug>-observability-openclaw` |
| Pi       | `<org-slug>-observability-pi`       |
| Copilot  | `<org-slug>-observability-copilot`  |

OpenCode, OpenClaw and Pi have no marketplace track, so their packages are installed directly rather than listed. Users are shown a notice in the README that the observability plugin is required alongside any MCP server plugins.

See [Package Format — observability plugin](./package-format.md#observability-plugin) for the hook events registered and the hook script contents.
