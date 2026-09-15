# Platform MCP risk tool monitors

This runbook covers the seven D3 risk-policy and exclusion tools delivered for
AGE-3168. The application emits low-cardinality OpenTelemetry metrics from
`gram-server`; Datadog dashboards and monitors are managed in the Datadog UI,
not as repository IaC.

## Current deployment state

The metric contract and automated allowlist/leak coverage are implemented.
The dashboard and monitors below are **not verified as deployed**. Create and
link them before enabling risk mutations beyond the internal cohort.

## Safety boundary

Both metrics use exactly these fixed tags:

- `platform_mcp.risk.tool`
- `platform_mcp.risk.outcome`
- `platform_mcp.risk.replay`
- `platform_mcp.risk.catalog_version`
- `platform_mcp.risk.reconciliation`

They never include a user, organization, project, connection, client, policy or
exclusion ID, receipt ID, policy name, prompt, exact/regex match value, filter,
CEL, principal, URL, error message, or request/result payload.

## Metric contracts

| Metric                            | Type                | Meaning                                                   |
| --------------------------------- | ------------------- | --------------------------------------------------------- |
| `platform_mcp.risk.tool_calls`    | counter             | One bounded handler result for any of the seven D3 tools. |
| `platform_mcp.risk.tool_duration` | histogram (seconds) | Handler duration for the same result and tags.            |

Transport/schema rejections happen before a typed handler runs and are not
included in these metrics. Protocol and HTTP telemetry remain the source for
malformed transport traffic.

Allowed values:

- `tool`: `list_risk_policies`, `get_risk_policy`, `create_risk_policy`,
  `update_risk_policy`, `list_risk_exclusions`, `create_risk_exclusion`, or
  `update_risk_exclusion`;
- `outcome`: `succeeded`, `feature_unavailable`, `invalid_request`,
  `not_found`, `conflict`, `rate_limited`, `repair_required`, or `unavailable`;
- `replay`: `not_applicable`, `fresh`, `receipt_replay`, or
  `matched_existing`;
- `catalog_version`: `risk-policy-catalog-v1`;
- `reconciliation`: `not_applicable` or `scheduled`.

`receipt_replay` means the same idempotency key returned its stored result.
`matched_existing` means a fresh create converged on an equivalent existing
resource. Neither value identifies the resource.

## Dashboard

Create a **Platform MCP risk tools** dashboard scoped to
`service:gram-server` with:

1. calls and success ratio by fixed tool;
2. mutation refusals by outcome, highlighting `conflict`, `rate_limited`, and
   `unavailable`;
3. p50/p95/p99 duration by tool;
4. create results split by `fresh`, `receipt_replay`, and `matched_existing`;
5. exclusion mutation results split by reconciliation state.

Do not add organization, project, user, client, resource, or receipt identity as
a tag. Keep the active internal cohort in deployment/flag records, not metric
cardinality.

## Datadog monitors

Initial thresholds require dogfood baseline tuning. Scope every query to
`service:gram-server` and link notifications to this runbook.

### Mutation failure spike

Monitor the four mutation tools over 15 minutes. Alert when non-success,
non-client-refusal outcomes rise above the dogfood baseline:

```text
sum(last_15m):sum:platform_mcp.risk.tool_calls{
  service:gram-server AND
  platform_mcp.risk.tool IN (create_risk_policy, update_risk_policy, create_risk_exclusion, update_risk_exclusion) AND
  platform_mcp.risk.outcome NOT IN (succeeded, invalid_request, not_found, conflict, rate_limited, feature_unavailable)
}.as_count()
```

Start with warning `> 5` and alert `> 15`, then tune from internal-cohort
traffic. Investigate dependencies and the bounded outcome first; use audited
domain state for a specific report.

### Conflict spike

Conflicts are actionable concurrency/idempotency signals, not server failures:

```text
sum(last_15m):sum:platform_mcp.risk.tool_calls{
  service:gram-server AND
  platform_mcp.risk.tool IN (create_risk_policy, update_risk_policy, create_risk_exclusion, update_risk_exclusion) AND
  platform_mcp.risk.outcome:conflict
}.as_count()
```

Start with warning `> 10` and alert `> 25`. Compare `receipt_replay` and
`matched_existing` before treating a spike as a defect.

### Latency

Alert on sustained p95 `platform_mcp.risk.tool_duration` above 2.5 seconds for
mutations or 1 second for reads over 15 minutes after a dogfood baseline exists.

### Reconciliation scheduling

For successful exclusion mutations, monitor the ratio of
`platform_mcp.risk.reconciliation:scheduled`. The tool metric proves scheduling
was reported, not that the asynchronous reconciliation completed; use Temporal
workflow/activity telemetry and audited state to verify completion.

## Rollout evidence

Before broader rollout, record links to the deployed dashboard and monitors in
the AGE-3168 rollout record, execute the D3 smoke checklist, and verify the
mutation kill switch restores read-only behavior. Missing or unverified monitors
block cohort expansion, not deployment of the disabled/read-only code.

## Ownership

- **Owner:** Control Plane / Platform MCP on-call
- **Service:** `gram-server`
- **Rollback:** disable `platform-mcp-risk-mutations`; reads remain available
