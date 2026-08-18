# Platform MCP durable authorization monitors

This runbook covers the durable Platform MCP OAuth lifecycle added in AGE-3161.
The application emits low-cardinality OpenTelemetry metrics from `gram-server`;
Datadog monitors are managed in the Datadog UI, not as repository IaC.

## Safety boundary

The metrics use only the fixed `platform_mcp.operation`,
`platform_mcp.outcome`, and `platform_mcp.reason` tags. They must never include
an access token, refresh token, authorization code, token hash, JTI, user,
organization, connection, client identifier, redirect URI, or free-form error.

Durable authorization state and audited revocation actions remain authoritative.
These metrics are operational evidence only.

## Metric contracts

| Metric                                        | Type                | Tags                                      | Meaning                                                                  |
| --------------------------------------------- | ------------------- | ----------------------------------------- | ------------------------------------------------------------------------ |
| `platform_mcp.oauth.events`                   | counter             | `operation`, `outcome`, optional `reason` | One bounded OAuth endpoint result for refresh and code exchange.         |
| `platform_mcp.oauth.refresh_duration`         | histogram (seconds) | `operation:refresh`, `outcome:succeeded`  | Successful refresh latency only.                                         |
| `platform_mcp.oauth.connection_age`           | histogram (seconds) | `operation:refresh`, `outcome:succeeded`  | Current authorization-generation age at a successful refresh.            |
| `platform_mcp.oauth.reauthorization_required` | counter             | `reason`                                  | A committed terminal transition, after the durable transaction succeeds. |

Terminal `reason` values are fixed: `refresh_idle_expired`,
`authorization_expired`, `refresh_reuse`, `connection_revoked`,
`client_revoked`, `authorization_lost`, and `security_reset`.

## Dashboard

Create a **Platform MCP durable auth** dashboard scoped to
`service:gram-server` with these panels:

1. Refresh success ratio: `succeeded / all refresh outcomes`.
2. Refresh `temporarily_unavailable` ratio.
3. Refresh `invalid_grant` outcomes, split by bounded reason; highlight
   `refresh_reuse` terminal transitions.
4. Interactive authorization and authorization-code exchange outcomes.
5. p95 `platform_mcp.oauth.refresh_duration`.
6. Authorization-generation age from `platform_mcp.oauth.connection_age`.
7. Reauthorization-required transitions by reason.

The active dogfood cohort belongs in the dashboard's saved scope/configuration;
do not add organization or client identity as a metric tag.

## Datadog monitors

Thresholds are initial values. Tune after the dogfood baseline is established.
All metric queries below are scoped to `service:gram-server`.

### Refresh availability

```
Monitor type: Metric (ratio)
Query: sum(last_15m):sum:platform_mcp.oauth.events{operation:refresh,outcome:succeeded}.as_count()
       / sum:platform_mcp.oauth.events{operation:refresh}.as_count() * 100
Warn:  < 98
Alert: < 95
```

Also alert separately when `temporarily_unavailable` exceeds 2% of refresh
outcomes in 15 minutes. Check dependency health before asking users to reconnect:
this result is retryable and must not cause a durable reset.

### Invalid grants and refresh reuse

```
Monitor type: Metric
Query: sum(last_15m):sum:platform_mcp.oauth.events{operation:refresh,outcome:invalid_grant}.as_count()
Warn:  > baseline + 10
Alert: > baseline + 25
```

Add a second monitor for any sustained
`platform_mcp.oauth.reauthorization_required{reason:refresh_reuse}` activity.
Treat spikes as a security and client-retry investigation; do not infer token
theft from a single event.

### Interactive authorization rate

Monitor a large rise in
`platform_mcp.oauth.events{operation:interactive_authorization}` relative to
the active internal rollout cohort. This is a leading signal for refresh or
client-storage regressions. Investigate refresh outcomes and terminal reasons
first.

### Revocation propagation probe

The counters do not claim a revocation propagation latency by themselves. Run a
synthetic probe that performs each supported revocation action (connection,
client, generation, JTI, organization gate, and org-admin loss), then makes the
next runtime request with the formerly valid access token. Record action-to-denial
latency in the probe system and alert if it exceeds the published contract bound.

When this monitor fires, verify the affected durable state first, then inspect
the runtime authorization query and dependency health. Do not use production
user identifiers as monitor tags.

## Ownership and response

- **Owner:** Control Plane / Platform MCP on-call.
- **Service:** `gram-server`.
- **Runbook:** this document, linked from every monitor notification.

For a terminal-reason spike, inspect bounded metrics and safe structured terminal
logs only. Use audited management state to diagnose a specific report; never
search for or copy bearer credentials into dashboards, logs, tickets, or comments.
