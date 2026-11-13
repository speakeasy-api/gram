# Plugin publish failure signals

The Temporal activity failure monitor and a failed project publish answer different
questions. An activity can fail twice and succeed on its third attempt: that is
two execution failures but zero failed logical publishes.

## Existing activity-execution monitor

Monitor `304557077` is not managed by configuration in this repository. Do not
change it automatically. Have its owner review the query and notification together.
Use the configured production namespace in place of `<TEMPORAL_NAMESPACE>`.

Current query shape:

```text
change(avg(last_5m),last_30m):sum:temporal_activity_execution_failed{namespace:<TEMPORAL_NAMESPACE>} by {workflow_type,activity_type} > 10
```

This is a **change alert**, not a count over 30 minutes. It compares the average
value in the latest five-minute window to the equivalent window shifted back
30 minutes. Correct notification wording if retaining this query:

> Temporal activity execution failures increased by {{value}} in the five-minute
> average compared with the window 30 minutes earlier, for {{workflow_type.name}}
> / {{activity_type.name}}. This includes retry attempts, not unique failed project
> publishes.

If the intended alert really is “more than 10 activity execution failures in the
last 30 minutes”, replace the query (after owner approval) with:

```text
sum(last_30m):sum:temporal_activity_execution_failed{namespace:<TEMPORAL_NAMESPACE>} by {workflow_type,activity_type}.as_count() > 10
```

Corresponding wording:

> Temporal recorded {{value}} activity execution failures in the last 30 minutes
> for {{workflow_type.name}} / {{activity_type.name}}. Retry attempts are included;
> this is not a count of failed project publishes.

## Logical project-publish outcomes

`plugin_project_publish_outcomes` counts once when a project's publish activity
future resolves, **after retries**, in both the debounced publish workflow and the
generator rollout sweep. Its only custom tag is `outcome`:

- `published`: succeeded (including success after retries).
- `skipped`: unchanged fingerprint or another successful no-op.
- `rejected`: permanent rejection because no organization member can publish.
- `canceled`: cancellation, not a failed publish.
- `failed`: terminal error, including retry exhaustion, timeout, or permanent
  repository conflict. Permanent errors need not exhaust three attempts.

Suggested separate monitor, with threshold subject to baseline review:

```text
sum(last_30m):sum:plugin_project_publish_outcomes{namespace:<TEMPORAL_NAMESPACE>,outcome:failed} by {workflow_type}.as_count() > 0
```

> {{value}} logical project publishes ended in failure after their retry policy
> completed in the last 30 minutes, in {{workflow_type.name}}. Recovered retries,
> successful skips, permanent actor rejections, and cancellations are excluded.

Check the exported metric name and namespace/workflow tags in Metrics Explorer
before creating a monitor. These are counts of **publish operations**, not a
30-minute distinct count of project IDs: a later sweep or signal can fail another
publish for the same project. No project, organization, repository, error, or run
identifier is added as a metric tag. The fixed `plugin project publish outcome`
workflow log has the same bounded outcome and the SDK's workflow context for
Temporal drilldown. Workflow SDK metrics and logging suppress normal replay;
this is operational telemetry, not an exactly-once billing/audit ledger. A worker
crash around workflow-task completion can lose or duplicate telemetry. Termination
or workflow timeout before the future resolves cannot emit an outcome; retain
Temporal workflow-failure/timeout monitoring as a backstop.

Rollout candidates without a usable actor are filtered before scheduling a publish
activity and therefore do not increment this counter; rollout summary logs retain
those skip counts. Rollout `Conflicted` results are included in metric
`outcome:failed`, while `Rejected` results use `outcome:rejected`.

## GitHub upload diagnostics

Publishing normally keeps UTF-8 text inline in one tree request and uploads binary
files as blobs. Only when GitHub returns HTTP 422 with its request-timeout message,
it falls back once to uploading all file contents as blobs and submitting a clean,
SHA-only tree. Both paths preserve removal of deleted plugins. Blob uploads are
bounded to four in flight per project. Successful inline publishes do not incur
extra blob requests; the fallback adds one blob request per file and one tree
request. This is not a fleet-wide GitHub rate limiter. Other 422 errors do not
trigger a fallback, and failures in the fallback still use normal activity retries.

Within a sampled publish trace, inspect `github.create_tree` and its
`github.create_blob` children:

- Tree: `github.upload.strategy` identifies `inline` or `blobs`;
  `github.upload.file_count`, `github.upload.total_bytes`, and
  `github.upload.largest_file_bytes` describe the raw input files.
- Tree and blob: `github.upload.request_bytes` is the serialized request size;
  `http.response.status_code` is present when GitHub returned a response.
- Blob: `github.upload.content_bytes` is the raw file size before base64 encoding.
- `github.upload.duration_ms` includes upload preparation and request time. The
  tree span covers one attempt, including its child blob uploads; its duration is not just tree HTTP
  latency. Error span statuses distinguish blob failure, transport failure,
  rejected uploads, and invalid responses without copying upstream response text.

These diagnostics do not add file contents, paths, repository names, or tenant
identifiers as attributes. They depend on trace sampling; use the outcome counter
and fixed outcome logs above for unsampled aggregate monitoring.

## Temporal cost and deployment

This instrumentation adds no activities, timers, signals, workflow starts, history
commands, or schedules: **0 additional Temporal actions/month** at any volume.
No workflow version marker is needed because command order is unchanged. Deploy
with the worker normally; no workflow reset or live-monitor mutation is required.
