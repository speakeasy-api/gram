---
name: deploy-openapi
description: >-
  Use when deploying an OpenAPI spec to Gram. Triggers on "deploy openapi",
  "deploy api", "push spec to gram", "upload openapi".
license: Apache-2.0
---

# Deploy OpenAPI Spec to Gram

Guide for deploying OpenAPI YAML/JSON documents to the Gram platform.

## When to Use

- Deploying an OpenAPI v3 spec to Gram
- Setting up repeatable API deployments
- Quick one-off spec uploads

## Prerequisites

- Gram CLI installed and authenticated (`gram whoami` to verify)
- A valid OpenAPI v3 document (YAML or JSON)
- Target project configured via `--project` flag, `GRAM_PROJECT`, or profile

## Inputs

| Input | Description | Required |
|---|---|---|
| OpenAPI spec | Path or URL to YAML/JSON document | Yes |
| Slug | URL-friendly identifier (`^[a-z0-9_-]{1,128}$`) | Yes |
| Name | Human-readable display name | No |
| Method | `merge` (default) or `replace` | No |

## Outputs

| Output | Description |
|---|---|
| `gram.deploy.json` | Deployment config (stage+push flow) |
| Deployment ID | Returned by `gram push` or `gram upload` |
| Status | `completed`, `processing`, or `failed` |

## Decision Framework

| Scenario | Approach |
|---|---|
| Repeatable deploys (CI/CD, scripts) | Stage + Push |
| Quick one-off upload | Upload |
| Multiple sources in one deploy | Stage each, then push once |
| CI/CD pipeline | Add `--skip-poll` to push |

## Command: Stage + Push (Recommended)

**Step 1 — Stage the spec:**

```bash
gram stage openapi \
  --slug my-api \
  --location ./openapi.yaml \
  --name "My API"
```

This writes to `gram.deploy.json`. You can stage multiple sources before pushing.

**Step 2 — Push the deployment:**

```bash
gram push --config gram.deploy.json
```

Push with options:

```bash
# Replace all existing artifacts instead of merging
gram push --config gram.deploy.json --method replace

# CI/CD: don't wait for completion
gram push --config gram.deploy.json --skip-poll

# Idempotent deploy (safe to retry)
gram push --config gram.deploy.json --idempotency-key "deploy-$(git rev-parse HEAD)"
```

## Command: Upload (One-Step)

```bash
gram upload \
  --type openapiv3 \
  --location ./openapi.yaml \
  --slug my-api \
  --name "My API"
```

Upload also supports URLs:

```bash
gram upload \
  --type openapiv3 \
  --location https://raw.githubusercontent.com/org/repo/main/spec.yaml \
  --slug my-api
```

## Example: Full Workflow

```bash
# 1. Verify auth and target
gram whoami

# 2. Stage the OpenAPI spec
gram stage openapi --slug pet-store --location ./petstore.yaml --name "Pet Store API"

# 3. Review the config
cat gram.deploy.json

# 4. Push to Gram
gram push --config gram.deploy.json

# 5. Check status if needed
gram status
```

## Merge vs Replace

| Method | Behavior |
|---|---|
| `merge` (default) | Adds/updates sources without removing existing ones in the project |
| `replace` | Removes all existing deployment artifacts and replaces with this deployment |

Use `replace` when you want a clean slate. Use `merge` (default) when adding to an existing project.

## What NOT to Do

- Do not guess flag names — run `gram stage openapi --help` to verify
- Do not use invalid slug characters (no uppercase, spaces, or special chars)
- Do not skip `gram whoami` — deploying to the wrong project is hard to undo with `replace`
- Do not commit `gram.deploy.json` with hardcoded absolute paths — use relative paths

## Troubleshooting

| Problem | Solution |
|---|---|
| "unauthorized" or auth error | Run `gram auth` or check `GRAM_API_KEY` is valid and Provider-scoped |
| "invalid slug" | Slug must match `^[a-z0-9_-]{1,128}$}` |
| Config parse failure | Check `gram.deploy.json` is valid JSON with `schema_version: "1.0.0"` |
| Deployment stuck on "processing" | Run `gram status --json` to check. If stuck > 5 min, contact support |
| Wrong project | Run `gram whoami` to verify, use `gram auth switch` to change |
| Spec validation errors | Fix the OpenAPI spec — Gram validates on deploy |

## Related Skills

- **gram-context** — CLI reference and authentication
- **deploy-functions** — Deploy Gram Functions alongside OpenAPI specs
- **check-deployment-status** — Monitor and debug deployments
