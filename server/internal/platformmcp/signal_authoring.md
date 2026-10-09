# Signals intelligence authoring

The outcome is a complete, reusable sensor configuration in an explicitly selected
project. Existing risk-policy tools address enforcement, not classification; this
layer adds `find_signals`, `find_sensors`, `get_sensor`, `create_sensor`,
`create_signal`, `update_sensor`, `update_signal`, and `preview_sensor_match`.

External members need `project:read`; mutations additionally need `project:write`.
The owning service checks `signals_intelligence` on every operation, including
reads before receipt replay. External live membership admission remains in the
runtime. Exact project permissions are checked before project lookup so denied
callers receive the same refusal for existing and nonexistent selectors.
Managed assistants are intentionally excluded: the owning audit path
currently uses user actors and must support assistant identity before admission.

Previews run the same authorized mutation path in a rolled-back transaction.
Their creation IDs are provisional. A keyed token binds the exact proposal,
project, actor, and configuration version; all project configuration changes
invalidate the version. Confirmed calls atomically persist the domain rows,
per-row audits, outbox events and identifier-only idempotency receipt. Retries
return the historical receipt plus a fresh live read. A deleted target is reported
as unavailable rather than recreated. If post-commit verification fails, the
successful result retains the receipt and sets `snapshot_scope` to
`verification_unavailable`, with no target data or version. In that state,
`target_available: false` does not establish deletion. Retry with the same key and
inputs or read the configuration; never create again under a new key. Successful
fresh reads use `snapshot_scope: current_configuration`.
Receipt retention follows the existing
Platform MCP receipt lifetime; keys must not be treated as indefinite deduplication.

Snapshot work is bounded to 1000 active sensors and 1000 active signals, with
at most 100 membership entries in one proposal and 64 KiB of proposal JSON.
Searches return at most 100 resources per page. Matching previews use at most
20 caller-supplied examples and the same role-only compiler as the evaluator.
Configuration readiness never asserts subscriber health or reading production.

The demo seed already supplies reusable signals and sensors for discovery and
preview. This layer introduces no new persisted configuration shape or dashboard
data, so it uses those existing fixtures. Atomicity, replay, version conflicts,
permissions, feature gating, typed tool contracts, and byte-identical shipped
workflow content are covered by focused tests.

# Sensor enablement

Existing sensor authoring tools support `proposal.enabled` for authorized external
project writers. Creation defaults to enabled; omitted updates preserve the current
state. Discovery, previews, and live reads include `enabled`. Use the same preview,
confirmation, version, and idempotency workflow to pause or resume a selected sensor.
Verify the committed state with `get_sensor`. Disabled sensors remain configurable
and can be configuration-ready, but are excluded from new evaluations. Evaluations
already in progress may finish. No separate lifecycle tool is needed for this outcome.
