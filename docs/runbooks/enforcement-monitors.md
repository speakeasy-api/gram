# Realtime enforcement monitors

Realtime risk enforcement scans a hook's message before the agent acts on it.
Some detectors run in `gram-server`; the rest run as Pub/Sub consumers that the
server dispatches to and waits on (`server/internal/risk/enforcereply`). A lane
that produces no usable reply is **degraded**: the legacy lanes then fail open
(the event is allowed) and the LLM analyzer lane fails closed (covered policies
deny).

Monitors for these signals live in the Terraform monitor set in `gram-infra`
(`infra/terraform/k8s/datadog-monitors-enforcement.tf`), which is the only
monitor set for enforcement. The HCL below is the source this repo keeps in
sync with the metric contract; the runbook exists so the reason taxonomy and
the application code stay reviewable together.

## Metric contract

| Metric                             | Type                | Tags                         | Meaning                                                    |
| ---------------------------------- | ------------------- | ---------------------------- | ---------------------------------------------------------- |
| `risk.enforcement.pubsub_degraded` | counter             | `lane`, `reason`, `fail_mode` | One enforcement lane that produced no usable reply.        |
| `risk.enforcement.scan_duration`   | histogram (seconds) | `project_id`, `outcome`      | End-to-end duration of one realtime enforcement scan.      |
| `risk.enforcement.scan_results`    | counter             | `project_id`, `outcome`      | One realtime enforcement scan, counted under its outcome.  |

`scan_duration` and `scan_results` are recorded together for every scan, so the
counter is an exact sample count for the histogram over the same window. That
is what makes the latency monitor count-aware without a second instrument.

`outcome` is one of `success`, `blocked`, `warned`, `quarantined`, `failure`,
or `skipped`. `skipped` is the fast path taken when a project has no enforcing
policy at all: it does no detector work, so it is excluded from the latency
monitor and from its volume floor.

`fail_mode` says what the scan did after the lane degraded: `open` (the legacy
lanes allowed the event), `closed` (the enforcing LLM lane denied it), or
`shadow` (the lane was only compared, so its outage changed nothing).

## Degradation reasons

Only some reasons say anything about consumer health. Alerting keeps the rest
apart so caller-side pressure and content size do not page as an outage.

| `reason`                    | Consumer failure? | Meaning                                                                                        |
| --------------------------- | ----------------- | ---------------------------------------------------------------------------------------------- |
| `unavailable`               | yes               | No dispatcher is configured, so the lane was never published.                                  |
| `dispatch_error`            | yes               | Publication itself failed.                                                                     |
| `request_error`             | yes               | The request/reply transport failed.                                                            |
| `deadline`                  | yes               | The lane spent its own wait budget without a reply.                                            |
| `incomplete`                | yes               | The lane produced no reply and no error.                                                       |
| `invalid_reply`             | yes               | The reply did not correlate to the lane it answered for.                                       |
| `reply_<status>`            | yes               | The consumer answered with a non-OK status. The LLM lane appends its own classification token. |
| `invalid_finding`           | yes               | The reply carried a finding the server could not convert.                                      |
| `budget_exhausted`          | **no**            | The caller's own deadline cut the wait short before the lane had spent its budget.             |
| `truncated`                 | **no**            | The dispatcher size-limited the content, so the enforcing LLM lane judged a prefix only.       |

`budget_exhausted` is the one that used to page. An inference hook gives risk
evaluation a nine-second verdict budget (`verdictBudget` in
`server/internal/anthropicinference`), and a transcript that runs it out
cancels every in-flight lane. Before the split, those cut-off lanes were
counted as `deadline` — indistinguishable from a consumer that had its full
budget and still did not answer. `enforcereply.Outcome.CallerBudget` now names
the lanes whose deadline came from the caller, and `laneFindings` counts them
under `budget_exhausted`. They are also logged at `warn` rather than `error`,
since a caller running out of time is not a dependency fault.

A burst of `budget_exhausted` is real but is a capacity or latency story:
consumers are slow enough, or transcripts large enough, that callers give up
first. It gets its own monitor with a much higher threshold.

## Monitors

### 1. Enforcement lane degraded (consumer failures)

Pages when the consumers behind a fail-open lane stop answering. Grouped by
lane so the notification names the broken consumer.

```text
sum(last_15m):sum:risk.enforcement.pubsub_degraded{
  service:gram-server,fail_mode:open,!reason:budget_exhausted,!reason:truncated
} by {lane}.as_count()
```

Warn `> 10`, alert `> 25`. Tune against the production baseline.

### 2. Budget-exhausted fail-opens

Separate signal, separate — much higher — threshold. Not grouped by lane: the
caller's deadline cuts every lane of the same scan at once, so the lane
breakdown says nothing.

```text
sum(last_15m):sum:risk.enforcement.pubsub_degraded{
  service:gram-server,fail_mode:open,reason:budget_exhausted
}.as_count()
```

Warn `> 100`, alert `> 250`.

### 3. Scan latency, gated on volume

p95 over a handful of scans in a quiet hour is noise. The latency monitor is a
composite: it only fires when the same window also carries enough scans for the
percentile to mean anything.

```text
latency:  avg(last_15m):p95:risk.enforcement.scan_duration{service:gram-server,!outcome:skipped}
volume:   sum(last_15m):sum:risk.enforcement.scan_results{service:gram-server,!outcome:skipped}.as_count() > 500
composite: latency && volume
```

Latency warn `> 1.5` seconds, alert `> 3`. Only the composite notifies; the two
sub-monitors are silent inputs.

## Terraform

The monitor set below belongs in `gram-infra` at
`infra/terraform/k8s/datadog-monitors-enforcement.tf`.

```hcl
locals {
  enforcement_runbook = "https://github.com/speakeasy-api/gram/blob/main/docs/runbooks/enforcement-monitors.md"
  enforcement_notify  = "@slack-Speakeasy-gram-oncall"
}

# A fail-open lane whose consumers stopped answering. budget_exhausted and
# truncated are excluded: neither says the consumer is unhealthy. See the
# reason table in the runbook.
resource "datadog_monitor" "enforcement_lane_degraded" {
  name    = "[Risk] Enforcement lane degraded (consumer failures)"
  type    = "query alert"
  message = <<-EOT
    Enforcement lanes are failing open because their consumers are not
    answering. Covered policies are not enforcing for the affected lane.

    Runbook: ${local.enforcement_runbook}
    ${local.enforcement_notify}
  EOT

  query = "sum(last_15m):sum:risk.enforcement.pubsub_degraded{service:gram-server,fail_mode:open,!reason:budget_exhausted,!reason:truncated} by {lane}.as_count() > 25"

  monitor_thresholds {
    critical = 25
    warning  = 10
  }

  notify_no_data    = false
  renotify_interval = 60
  tags              = ["service:gram-server", "team:gram", "feature:risk-enforcement"]
}

# Callers giving up before the lane spent its budget. Real, but a capacity and
# latency story rather than an outage, so it gets a much higher threshold.
resource "datadog_monitor" "enforcement_budget_exhausted" {
  name    = "[Risk] Enforcement fail-opens from exhausted caller budget"
  type    = "query alert"
  message = <<-EOT
    Enforcement scans are failing open because the caller ran out of budget
    before the lanes replied - most often an inference hook exhausting its
    nine-second verdict budget. Check consumer latency and transcript size
    before treating this as a consumer outage.

    Runbook: ${local.enforcement_runbook}
    ${local.enforcement_notify}
  EOT

  query = "sum(last_15m):sum:risk.enforcement.pubsub_degraded{service:gram-server,fail_mode:open,reason:budget_exhausted}.as_count() > 250"

  monitor_thresholds {
    critical = 250
    warning  = 100
  }

  notify_no_data    = false
  renotify_interval = 120
  tags              = ["service:gram-server", "team:gram", "feature:risk-enforcement"]
}

# Silent input: p95 scan latency, excluding the no-policy fast path.
resource "datadog_monitor" "enforcement_scan_latency_p95" {
  name    = "[Risk] Enforcement scan p95 latency (composite input)"
  type    = "query alert"
  message = "Composite input for ${local.enforcement_runbook}. Do not notify directly."

  query = "avg(last_15m):p95:risk.enforcement.scan_duration{service:gram-server,!outcome:skipped} > 3"

  monitor_thresholds {
    critical = 3
    warning  = 1.5
  }

  notify_no_data = false
  tags           = ["service:gram-server", "team:gram", "feature:risk-enforcement", "composite-input"]
}

# Silent input: the volume floor that makes the percentile meaningful. Without
# it, a couple of slow scans in a quiet hour are enough to warn.
resource "datadog_monitor" "enforcement_scan_volume_floor" {
  name    = "[Risk] Enforcement scan volume floor (composite input)"
  type    = "query alert"
  message = "Composite input for ${local.enforcement_runbook}. Do not notify directly."

  query = "sum(last_15m):sum:risk.enforcement.scan_results{service:gram-server,!outcome:skipped}.as_count() > 500"

  monitor_thresholds {
    critical = 500
  }

  notify_no_data = false
  tags           = ["service:gram-server", "team:gram", "feature:risk-enforcement", "composite-input"]
}

resource "datadog_monitor" "enforcement_scan_latency" {
  name    = "[Risk] Enforcement scan latency regression"
  type    = "composite"
  message = <<-EOT
    Realtime enforcement scan p95 is elevated over a window that carried enough
    scans for the percentile to be meaningful.

    Runbook: ${local.enforcement_runbook}
    ${local.enforcement_notify}
  EOT

  query = "${datadog_monitor.enforcement_scan_latency_p95.id} && ${datadog_monitor.enforcement_scan_volume_floor.id}"

  notify_no_data    = false
  renotify_interval = 60
  tags              = ["service:gram-server", "team:gram", "feature:risk-enforcement"]
}
```

## Response

**Lane degraded.** Identify the lane from the notification group. Check the
consumer's own health and the Pub/Sub subscription's backlog and ack deadline.
`reason:reply_<status>` means the consumer answered and refused, so read its
logs rather than the transport's. While a legacy lane is degraded, its policies
are not enforcing - treat sustained degradation as a security gap, not only an
availability one.

**Budget exhausted.** Look at consumer p95 latency and at transcript size
before anything else. A caller that gives up is downstream of a slow consumer
often enough that this monitor firing alone should send you to the lane latency
panels, not to the caller. If consumers are healthy, the callers are being
handed larger transcripts than the budget allows.

**Scan latency.** Compare the per-lane consumer latency against the in-process
detectors. The composite only fires with volume behind it, so a single slow
tenant will not trip it; scope `risk.enforcement.scan_duration` by `project_id`
when a specific report needs attribution.

## Ownership

- **Owner:** Risk / enforcement on-call
- **Service:** `gram-server`
- **Monitor set:** `gram-infra`, `infra/terraform/k8s/datadog-monitors-enforcement.tf`
