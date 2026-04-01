---
name: check-deployment-status
description: >-
  Use when checking or debugging Gram deployment status. Triggers on "deployment status",
  "gram status", "deployment failed", "check deployment", "deployment error".
license: Apache-2.0
---

# Check Deployment Status

Guide for monitoring, inspecting, and debugging Gram deployments.

## When to Use

- Checking if a deployment completed successfully
- Debugging a failed deployment
- Monitoring a deployment in progress
- Verifying auth context before deploying

## Prerequisites

- Gram CLI installed and authenticated (`gram whoami` to verify)
- An active deployment to check (or checking latest deployment)

## Inputs

| Input | Description | Required |
|---|---|---|
| Deployment ID | Specific deployment to check | No (defaults to latest) |
| Output format | Human-readable or JSON | No |

## Outputs

| Output | Description |
|---|---|
| Status | `completed`, `processing`, or `failed` |
| Deployment details | Sources, timestamps, error info |
| Logs URL | Link to deployment logs in the dashboard |

## Command: Check Status

### Latest Deployment

```bash
gram status
```

### Specific Deployment

```bash
gram status --id <deployment-id>
```

### JSON Output (for scripting)

```bash
gram status --json
gram status --id <deployment-id> --json
```

## Command: Verify Auth Context

Before debugging, confirm you're looking at the right project:

```bash
gram whoami
gram whoami --json
```

This shows: current profile, organization, project, and API key info.

## Status Values

| Status | Meaning | Action |
|---|---|---|
| `completed` | Deployment succeeded | No action needed |
| `processing` | Deployment is in progress | Wait and re-check |
| `failed` | Deployment encountered an error | Check error details, fix, redeploy |

## Dashboard Logs

View detailed deployment logs in the browser:

```
https://app.getgram.ai/<org>/<project>/deployments/<deployment-id>
```

Replace `<org>`, `<project>`, and `<deployment-id>` with actual values from `gram whoami` and `gram status`.

## Example: Debug a Failed Deployment

```bash
# 1. Verify you're in the right project
gram whoami

# 2. Check latest deployment status
gram status

# 3. Get detailed JSON output
gram status --json

# 4. If failed, check logs in the dashboard
# URL: https://app.getgram.ai/<org>/<project>/deployments/<id>

# 5. Fix the issue and redeploy
gram push --config gram.deploy.json
```

## Decision Framework: Troubleshooting

| Symptom | Likely Cause | Solution |
|---|---|---|
| "unauthorized" | Auth expired or wrong key | `gram auth` or check `GRAM_API_KEY` |
| Status shows wrong project | Profile pointing elsewhere | `gram whoami` then `gram auth switch` |
| "processing" for > 5 min | Deployment may be stuck | Check dashboard logs, contact support if persistent |
| "failed" with no details | Server-side error | Check dashboard logs for full error trace |
| `gram status` shows "no deployments" | Wrong project or no deploys yet | Verify with `gram whoami`, check project slug |
| Deployment succeeded but tools missing | Merge didn't include all sources | Redeploy with all sources staged, or check `--method` |
| Deployment succeeded but stale tools | Old deployment cached | Wait a few seconds for propagation, then retry |

## CI/CD Usage

In CI/CD pipelines, use `--skip-poll` with push, then check status separately:

```bash
# Push without waiting
gram push --config gram.deploy.json --skip-poll

# ... other CI steps ...

# Check status later
gram status --json
```

Parse JSON output for automation:

```bash
STATUS=$(gram status --json | jq -r '.status')
if [ "$STATUS" = "failed" ]; then
  echo "Deployment failed!"
  exit 1
fi
```

## What NOT to Do

- Do not assume `gram status` shows the right project without running `gram whoami` first
- Do not retry a failed deployment without reading the error — fix the root cause
- Do not poll `gram status` in a tight loop — use `gram push` without `--skip-poll` for built-in polling

## Related Skills

- **gram-context** — CLI reference and authentication
- **deploy-openapi** — Deploy OpenAPI specs
- **deploy-functions** — Deploy Gram Functions
