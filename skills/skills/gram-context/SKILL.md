---
name: gram-context
description: >-
  Use when working with the Gram CLI or platform. Triggers on "gram", "gram cli",
  "gram help", "deploy to gram". Foundation skill — activate before other Gram skills.
license: Apache-2.0
---

# Gram CLI Context

Foundation reference for all Gram CLI operations. Activate this skill first, then activate domain-specific skills as needed.

## What is Gram?

Gram is a platform for deploying and managing MCP (Model Context Protocol) servers. It lets you turn OpenAPI specs and serverless functions into MCP-compatible tool servers that AI assistants like Claude, Cursor, and Gemini can connect to. Gram handles hosting, authentication, versioning, and observability — so you deploy your API or functions and get an MCP endpoint that any AI client can use.

Key concepts:

- **Projects** — containers for your MCP servers and their configuration
- **Sources** — the assets you deploy (OpenAPI specs, function bundles)
- **Toolsets** — the MCP servers generated from your sources, with tools, authentication, and environment settings
- **Deployments** — versioned releases of your sources to a project

## When to Use

- User mentions "gram", "gram cli", "deploy to gram", or any Gram CLI command
- Before using any other Gram skill — this provides the shared context

## Prerequisites

- Gram CLI installed (`gram --version` to verify, `gram update` to upgrade)
- Authenticated via `gram auth` or `GRAM_API_KEY` environment variable

## CLI Commands Reference

### `gram auth`

Authenticate interactively with Gram. Opens a browser for login.

```
gram auth
```

| Subcommand    | Description                                        |
| ------------- | -------------------------------------------------- |
| `auth switch` | Switch the default project for the current profile |
| `auth clear`  | Clear all authentication profiles                  |

| Flag              | Description                                                          |
| ----------------- | -------------------------------------------------------------------- |
| `--api-url`       | URL of the Gram API server (`$GRAM_API_URL`)                         |
| `--dashboard-url` | URL of the Gram dashboard for authentication (`$GRAM_DASHBOARD_URL`) |

### `gram whoami`

Display information about the current profile.

```
gram whoami [--json]
```

| Flag        | Description                        |
| ----------- | ---------------------------------- |
| `--api-key` | Override API key (`$GRAM_API_KEY`) |
| `--json`    | Output as JSON                     |

### `gram stage openapi`

Stage an OpenAPI document for deployment. Writes to `gram.deploy.json`.

```
gram stage openapi --slug my-api --location ./spec.yaml [--name "My API"]
```

| Flag         | Description                                    | Required |
| ------------ | ---------------------------------------------- | -------- |
| `--slug`     | URL-friendly identifier                        | Yes      |
| `--location` | Path or URL to OpenAPI YAML/JSON               | Yes      |
| `--name`     | Human-readable name                            | No       |
| `--config`   | Config file path (default: `gram.deploy.json`) | No       |

### `gram stage function`

Stage a Gram Functions zip file for deployment.

```
gram stage function --slug my-fn --location ./dist.zip [--runtime nodejs:22]
```

| Flag         | Description                                    | Required |
| ------------ | ---------------------------------------------- | -------- |
| `--slug`     | URL-friendly identifier                        | Yes      |
| `--location` | Path or URL to zip file                        | Yes      |
| `--name`     | Human-readable name                            | No       |
| `--runtime`  | Runtime environment (default: `nodejs:22`)     | No       |
| `--config`   | Config file path (default: `gram.deploy.json`) | No       |

### `gram push`

Push a staged deployment to Gram.

```
gram push --config gram.deploy.json
```

| Flag                | Description                                       |
| ------------------- | ------------------------------------------------- |
| `--config`          | Path to deployment config file                    |
| `--method`          | `merge` (default) or `replace`                    |
| `--skip-poll`       | Return immediately without waiting for completion |
| `--idempotency-key` | Unique key for idempotent deploys                 |
| `--api-key`         | Override API key (`$GRAM_API_KEY`)                |
| `--project`         | Target project slug (`$GRAM_PROJECT`)             |
| `--org`             | Target organization slug (`$GRAM_ORG`)            |

### `gram upload`

Upload an asset directly (one-step alternative to stage + push).

```
gram upload --type openapiv3 --location ./spec.yaml --slug my-api [--name "My API"]
```

| Flag         | Description                            | Required            |
| ------------ | -------------------------------------- | ------------------- |
| `--type`     | Asset type: `openapiv3` or `function`  | Yes                 |
| `--location` | File path or URL                       | Yes                 |
| `--slug`     | URL-friendly identifier                | Yes                 |
| `--name`     | Human-readable name                    | No                  |
| `--runtime`  | Runtime for functions                  | For `function` type |
| `--api-key`  | Override API key (`$GRAM_API_KEY`)     | No                  |
| `--project`  | Target project slug (`$GRAM_PROJECT`)  | No                  |
| `--org`      | Target organization slug (`$GRAM_ORG`) | No                  |

### `gram status`

Check deployment status.

```
gram status [--id <deployment-id>] [--json]
```

| Flag        | Description                              |
| ----------- | ---------------------------------------- |
| `--id`      | Specific deployment ID (default: latest) |
| `--json`    | Output as JSON                           |
| `--api-key` | Override API key (`$GRAM_API_KEY`)       |
| `--project` | Target project slug (`$GRAM_PROJECT`)    |
| `--org`     | Target organization slug (`$GRAM_ORG`)   |

### `gram install <client>`

Install a Gram toolset as an MCP server. Subcommands: `claude-code`, `claude-desktop`, `cursor`, `gemini-cli`.

See the **install-mcp-server** skill for details.

### `gram update`

Update the Gram CLI to the latest version.

## Global Options

These flags work with all commands:

| Flag           | Env Var           | Description                            |
| -------------- | ----------------- | -------------------------------------- |
| `--api-key`    | `GRAM_API_KEY`    | API key (must be scoped as 'Provider') |
| `--project`    | `GRAM_PROJECT`    | Target project slug                    |
| `--org`        | `GRAM_ORG`        | Target organization slug               |
| `--profile`    | `GRAM_PROFILE`    | Named profile to use                   |
| `--log-level`  | `GRAM_LOG_LEVEL`  | Log level (default: `info`)            |
| `--log-pretty` | `GRAM_LOG_PRETTY` | Pretty-print logs (default: `true`)    |

## Environment Variables

| Variable             | Purpose                                  |
| -------------------- | ---------------------------------------- |
| `GRAM_API_KEY`       | API key for non-interactive auth (CI/CD) |
| `GRAM_API_URL`       | Custom API server URL                    |
| `GRAM_ORG`           | Default organization slug                |
| `GRAM_PROJECT`       | Default project slug                     |
| `GRAM_PROFILE`       | Named profile to use                     |
| `GRAM_DASHBOARD_URL` | Custom dashboard URL for auth            |

## Configuration File

`gram.deploy.json` is the deployment config created by `gram stage` and consumed by `gram push`:

```json
{
  "schema_version": "1.0.0",
  "type": "deployment",
  "sources": [
    {
      "type": "openapiv3",
      "location": "./spec.yaml",
      "name": "My API",
      "slug": "my-api"
    },
    {
      "type": "function",
      "location": "./dist.zip",
      "name": "My Functions",
      "slug": "my-functions",
      "runtime": "nodejs:22"
    }
  ]
}
```

## Slug Rules

Slugs must match: `^[a-z0-9_-]{1,128}$`

- Lowercase letters, numbers, hyphens, underscores only
- 1 to 128 characters
- Must be unique across all sources in a deployment

## Authentication

- **Interactive**: `gram auth` opens a browser flow and saves a profile locally
- **CI/CD**: Set `GRAM_API_KEY` environment variable (must be a Provider-scoped key)
- **Switching projects**: `gram auth switch` changes the default project for the current profile
- **Multiple profiles**: Use `--profile <name>` to manage multiple configurations

## Important Rules

1. **Always verify flags** — run `gram <command> --help` before guessing flag names
2. **Check auth first** — run `gram whoami` to confirm you're targeting the right org/project
3. **Slugs are immutable identifiers** — choose them carefully, they appear in URLs
4. **Stage then push** for repeatable deploys; `upload` for one-offs

## Related Skills

- **deploy-openapi** — Deploy OpenAPI specs to Gram
- **deploy-functions** — Deploy Gram Functions
- **install-mcp-server** — Install MCP servers in AI clients
- **check-deployment-status** — Debug deployment status
- **write-gram-function** — Author functions with the SDK
