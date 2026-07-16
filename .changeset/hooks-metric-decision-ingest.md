---
"server": patch
---

Expand the `hooks.event.duration` metric for DNO-539 dashboard coverage: the unified `/rpc/hooks.ingest` endpoint now records it (it previously emitted no duration/throughput metric at all, leaving the plugin ingest path invisible to the hooks monitors), and every hooks endpoint now tags the metric with a `gram.hook.decision` attribute (allow/deny/ask, or none when the endpoint errored before producing a verdict) so allow/deny rates can be charted independently of the processing outcome. Ingest also distinguishes a new `unauthenticated` outcome (keyless requests acknowledged without processing) from the hard-401 `unauthorized` one.
