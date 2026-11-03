# Custom domain DNS observations rollout

The migrations must finish applying in each environment before deploying the application image built from the schema PR (#8394) or any descendant. SQLc generates directly from the declared schema, so even that first image reads the four new columns and its custom-domain queries fail against the pre-migration schema. The `skip-migration-check` label permits the generated readers in the migration PR; it does not enforce deployment ordering or wait for schema readiness. The nullable columns are safe for the previously deployed image. Use the normal release and Atlas Operator process; do not run migrations directly against remote databases.

After that prerequisite, token/API foundations, observation workers, and dashboard/admin surfaces can deploy separately. API fields remain absent before the first observation. DNS configuration is advisory, TXT proves ownership, and infrastructure failures never automatically deactivate a domain. Previously disabled domains still require explicit reverification.

## Automatic refresh and rolling deployments

The existing daily sweep refreshes activated, verified domains. No one-time backfill command or manual check is required for rollout. Allow one full daily interval after the new workers have replaced all old workers.

`dns_checked_at` identifies the DNS observation independently of older workers. Infrastructure and DNS belong to the same check only when `dns_checked_at` and `health_checked_at` match. After the product surfaces in #8411 deploy, the dashboard and MCP diagnostics show infrastructure as not checked separately when they do not match. A non-matching pair is expected temporarily during a rolling deployment.

Old workers can still execute the previous automatic-disable behavior until they drain. Do not enable per-domain-target adoption until the new worker rollout is complete. Reverting to the old worker image restores that behavior; prefer a forward fix when possible. Leave the additive schema in place on application rollback.

## Optional immediate check for one domain

The instructions below require the dashboard/API surfaces from #8411, which expose separate DNS timestamps and **Check again** for every verified, activated domain. Before #8411 deploys, the existing dashboard offers **Check again** only on the unhealthy card and does not show separate DNS timestamps. Use the fleet refresh below to refresh other active domains without waiting for the surfaces.

1. Wait for the new worker rollout and #8411 surfaces to complete in the intended environment.
2. Sign in as an organization administrator and open **Organization → Network Access** (`/<org>/domains`).
3. In the verified, activated custom-domain card, select **Check again**.
4. Wait for the request to finish. Confirm the DNS check time is after the rollout. The DNS result and infrastructure result are separate; `unconfirmed` can describe a working proxied domain.
5. A previously disabled domain uses **Reverify domain** instead. A health check does not reactivate it.

## Optional immediate fleet refresh

Use an already authenticated Temporal CLI profile for the intended environment. The following commands do not contain credentials; replace the profile and queue placeholders with values from that environment. Do not use a preview worker queue.

First inspect the existing schedule to find its action's task queue:

```sh
mise exec -- temporal --profile '<PROFILE>' schedule describe \
  --schedule-id 'v1:custom-domain-health-schedule'
```

Start a new sweep with a unique `CheckDate`. The field is a deduplication label, not a date parser: it becomes part of each child workflow ID. Triggering the schedule again with today's default label can skip domains already checked today.

```sh
domain_refresh_id="dns-observations-$(date -u +%Y%m%dT%H%M%SZ)"
mise exec -- temporal --profile '<PROFILE>' workflow start \
  --type CustomDomainHealthSweepWorkflow \
  --task-queue '<TASK_QUEUE_FROM_SCHEDULE>' \
  --workflow-id "$domain_refresh_id" \
  --execution-timeout 30m \
  --input "{\"CheckDate\":\"$domain_refresh_id\"}"
mise exec -- temporal --profile '<PROFILE>' workflow show \
  --workflow-id "$domain_refresh_id"
```

The sweep starts detached per-domain children; the parent's completion alone does not establish that every check finished. In Temporal, follow the child workflow links and confirm successful completion. Once #8411 is deployed, in the dashboard or staff custom-domain view, confirm DNS timestamps are after the rollout and match health timestamps for checked domains. Inactive and unverified rows are intentionally excluded. The sweep also reports orphaned managed resources; it does not delete them.

The sweep can send the normal infrastructure-unhealthy notification when it detects a new infrastructure failure. DNS findings alone do not send that notification.
