---
name: grafana-lgtm
description: Use when inspecting OpenTelemetry traces or metrics that gram-server, gram-worker or pystreams emitted during local development — confirming an endpoint is instrumented, finding the slow or failing span after hitting a local route, proving a request propagated from server into a Temporal activity, or checking a local counter/histogram. Also use when a local trace search comes back empty, a metric looks missing from Prometheus, or results look like they came from the wrong worktree (every worktree now shares one LGTM, so unfiltered queries return everyone's data). Triggers — "check the trace", "did that get instrumented", "why is this slow locally", "what did the worker do", TraceQL, Tempo, Grafana, LGTM, spanmetrics, `mise run open:grafana`, GRAFANA_PORT / TEMPO_HTTP_PORT / PROMETHEUS_PORT, and any local trace or metric lookup that returns 404 or an empty result.
---

# Local traces and metrics

One `lgtm` container (`grafana/otel-lgtm`) runs Grafana, Tempo and Prometheus together. Query it over HTTP with `curl` — the UI is for humans, the APIs are for you.

```
every worktree's gram-server / gram-worker
  → OTLP gRPC localhost:4317 → gram-shared-lgtm-1 ├→ Tempo      (traces)
                                                  └→ Prometheus (metrics)
```

**It is shared across every worktree**, declared in `compose.shared.yml` under the fixed project `gram-shared` — not in the per-worktree `compose.yml`. One copy serves the whole machine, so its ports are the same everywhere and are never remapped. `mise run infra:start` asserts it (idempotently) from whichever worktree runs first.

Logs and profiles are not wired into it — application logs go to stdout, so reach for `pitchfork logs` instead.

## Filter by worktree, always

**Every worktree's signals land in the same Tempo and the same Prometheus.** An unfiltered query returns every worktree's data mixed together, and two worktrees on the same commit produce identical span and series names — so an unfiltered result that looks like yours may not be. This is the single most likely way to report the wrong answer here.

What separates them is the `worktree` resource attribute, set from `OTEL_RESOURCE_ATTRIBUTES` in `mise.local.toml` (written by `git:workinit`; the main working tree uses `worktree=main` from `mise.toml`). Get your own value, then filter every query with it:

```bash
eval "$(mise env)"                       # authority; a stale shell may have none of these
TEMPO="http://localhost:$TEMPO_HTTP_PORT"
PROM="http://localhost:$PROMETHEUS_PORT"
WT="${OTEL_RESOURCE_ATTRIBUTES#worktree=}"   # e.g. gram-infra-ab12
```

In TraceQL that is a `resource.worktree` matcher; in PromQL a `worktree` label:

```bash
curl -s --get "$TEMPO/api/search" \
  --data-urlencode "q={resource.worktree=\"$WT\"}" \
  --data-urlencode "start=$S" --data-urlencode "end=$E"
```

Grafana UI: `mise run open:grafana`, or `http://localhost:$GRAFANA_PORT` (anonymous admin, no login).

Ports no longer identify a stack — they are the same for everyone, and the container behind them is always `gram-shared-lgtm-1`. Daemon state is a separate question — `pitchfork list` tells you whether an emitter is _currently_ running, which explains stale data, but signals nothing about whose data is in Tempo. Traces already ingested stay queryable long after the process that produced them stops, and a stopped daemon prints as `available`, which reads like "fine" at a glance.

If a search returns nothing, check that you are filtering on a worktree that actually reported (see the table) before assuming the code is uninstrumented.

## Quick reference

Several of these take a time window; define it once — `S=$(( $(date +%s) - 3600 )); E=$(( $(date +%s) + 60 ))`.

| Goal                          | Command                                                                                                                                                                 |
| ----------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Which worktrees are reporting | `curl -s --get "$TEMPO/api/v2/search/tag/resource.worktree/values" --data-urlencode "start=$S" --data-urlencode "end=$E"`                                               |
| Which services are reporting  | `curl -s --get "$TEMPO/api/v2/search/tag/resource.service.name/values" --data-urlencode "start=$S" --data-urlencode "end=$E"`                                           |
| Which spans/routes exist      | `curl -s --get "$TEMPO/api/v2/search/tag/name/values" --data-urlencode 'q={resource.service.name="gram-server"}' --data-urlencode "start=$S" --data-urlencode "end=$E"` |
| Search traces                 | `curl -s --get "$TEMPO/api/search" --data-urlencode 'q={…}' --data-urlencode "start=$S" --data-urlencode "end=$E"`                                                      |
| One trace, all spans          | `curl -s "$TEMPO/api/v2/traces/<traceID>"`                                                                                                                              |
| Metric names                  | `curl -s "$PROM/api/v1/label/__name__/values"`                                                                                                                          |
| Labels on a metric            | `curl -s --get "$PROM/api/v1/series" --data-urlencode 'match[]=<metric>'`                                                                                               |
| Run PromQL                    | `curl -s --get "$PROM/api/v1/query" --data-urlencode 'query=…'`                                                                                                         |

## Traces — Tempo

TraceQL goes in `q`. Use `--get --data-urlencode`; braces and quotes get mangled in a bare URL. Always pass `start`/`end` (Unix seconds) — omitting them silently searches a narrow recent window and returns a _subset_, indistinguishable from "not instrumented". Ranges wider than 168h are rejected.

```bash
S=$(( $(date +%s) - 3600 )); E=$(( $(date +%s) + 60 ))
q() { curl -s --get "$TEMPO/api/search" --data-urlencode "q=$1" \
        --data-urlencode "start=$S" --data-urlencode "end=$E" --data-urlencode 'limit=50'; }

q '{resource.service.name="gram-server" && name="POST /v1/mcp/{mcpSlug}"}'
q '{status=error}'
q '{span.http.response.status_code=500}'
q '{duration>1s}'
```

Selector prefixes matter: `resource.` for resource attributes (`service.name`), `span.` for span attributes (`http.route`, `db.statement`), and bare `name`, `duration`, `status` for intrinsics. The tag-value endpoints take both `q=` and `start`/`end`, and need them for the same reasons: without `q=` the answer fills with Docker's own CLI spans, and without a time range they inherit the same narrow default window — a service that has been quiet for a while simply will not be listed.

**Reading results.** `/api/search` returns summaries: `traceID` (hex), `rootServiceName`, `rootTraceName`, `serviceStats`. `durationMs` is _omitted_ for sub-ms traces, so `t["durationMs"]` raises `KeyError` — use `.get()`. `serviceStats` is keyed by service name and is the cheapest way to answer "did the worker participate?" without fetching the trace at all.

Fetch spans with `/api/v2/traces/<traceID>`, which returns OTLP JSON (`resourceSpans[].scopeSpans[].spans[]`, one `resourceSpans` entry per service). **Span IDs there are base64, not hex** — `"spanId": "vKOgqsNgmLI="`. Two services appearing in one trace does not by itself prove context propagated; the proof is a worker span whose `parentSpanId` equals the server span's `spanId`. Compare the base64 strings directly, and only `base64.b64decode(x).hex()` when you need to cross-reference a log line.

Three things reliably cause false conclusions:

- **Too new.** Search lags ingest — Tempo idles the trace out and cuts a block first, anywhere from seconds to about a minute. Lookup by trace ID is immediate, so prefer that when a log line already gave you the ID.
- **Too old.** See the `start`/`end` note above.
- **`compose` and `buildx` are not Gram.** Docker's CLI tooling exports its own telemetry (spans like `cli/up`, `GET /v1.55/containers/json`) because `OTEL_EXPORTER_OTLP_ENDPOINT` is set in the mise env, and it keeps reporting long after Gram goes quiet. Pin `resource.service.name` rather than searching `{}`.

## Metrics — Prometheus

Instrument names do not survive OTLP ingest unchanged, so searching for the Go constant finds nothing. Dots become underscores, the unit is appended, counters gain `_total`, and annotation units like `{document}` are dropped:

| Instrument in Go                         | Series in Prometheus                                    |
| ---------------------------------------- | ------------------------------------------------------- |
| `openapi.processed.count` (`{document}`) | `openapi_processed_count_total`                         |
| `openapi.processed.duration` (`s`)       | `openapi_processed_duration_seconds_{bucket,count,sum}` |

Confirm the real name, then always filter by `service_name` (also duplicated into `job`) so another worktree's series cannot answer your question:

```bash
curl -s "$PROM/api/v1/label/__name__/values" | python3 -c 'import json,sys; print([m for m in json.load(sys.stdin)["data"] if "openapi" in m])'
curl -s --get "$PROM/api/v1/query" --data-urlencode 'query=sum by (outcome) (openapi_processed_count_total{service_name="gram-worker"})'
```

Expect a thin label set. Resource attributes mostly collapse to `service_name`/`job`, and instrument attributes whose value is the empty string vanish entirely — a label you can read in the Go source may simply not be there.

**An empty instant query does not mean the metric is missing.** Prometheus only looks back 5 minutes, and a local worker that exported once and went idle drops out of instant queries while still listed in `__name__`. Re-check with `last_over_time(openapi_processed_count_total[6h])`, or with a range query when you also want to know _when_ the data stopped:

```bash
NOW=$(date +%s)
curl -s --get "$PROM/api/v1/query_range" \
  --data-urlencode 'query=sum by (outcome) (openapi_processed_count_total)' \
  --data-urlencode "start=$((NOW-3600))" --data-urlencode "end=$NOW" --data-urlencode 'step=60'
```

Prometheus carries the last sample forward for 5 minutes, so a series whose final point sits noticeably before `end` means the counter landed and export then stopped — confirm with `pitchfork list`. Inside that 5-minute window the same series still looks current, and `timestamp(last_over_time(…))` will not settle it either: it reports the evaluation time, not the sample time, so `time() - timestamp(…)` evaluates to `0` no matter how stale the data is.

If both the instant query and `last_over_time` come back empty, the third possibility is that you are pointed at a differently-configured stack: an older worktree scraping through the collector labels the same metric `job="otel-collector"` with `gram_outcome` and no `service_name`, so a `service_name` filter can never match. `/api/v1/series` shows what is really there.

Tempo also derives RED metrics from every trace, so rate, errors and latency exist without app instrumentation: `traces_spanmetrics_calls_total`, `traces_spanmetrics_latency_bucket`, `traces_service_graph_request_*`. These label the service as `service`, not `service_name`; grouping by the wrong one yields empty results.

```bash
curl -s --get "$PROM/api/v1/query" --data-urlencode \
  'query=histogram_quantile(0.95, sum by (le, span_name) (rate(traces_spanmetrics_latency_bucket{service="gram-server", span_kind="SPAN_KIND_SERVER"}[1h])))'
```

`le` is in seconds. Two details make or break this query locally: keep the range wide (`[1h]`, not `[5m]`) because `rate()` over a counter that stopped moving yields `NaN` rather than nothing — 58 all-`NaN` series instead of an empty result, which sails straight past any "is it empty?" check — and filter `span_kind` unless you want every client and internal span mixed in with the routes.

## Worked example — find a failing request and whether the worker was involved

```bash
eval "$(mise env)"; TEMPO="http://localhost:$TEMPO_HTTP_PORT"
S=$(( $(date +%s) - 3600 )); E=$(( $(date +%s) + 60 ))
curl -s --get "$TEMPO/api/search" \
  --data-urlencode 'q={resource.service.name="gram-server" && status=error}' \
  --data-urlencode "start=$S" --data-urlencode "end=$E" > /tmp/hits.json
ID=$(python3 -c 'import json; h=json.load(open("/tmp/hits.json")).get("traces") or []; print(h[0]["traceID"] if h else "")')

if [ -z "$ID" ]; then
  echo "no error traces in window — widen start/end, or it is not searchable yet"
else
curl -s "$TEMPO/api/v2/traces/$ID" > /tmp/trace.json

python3 - <<'PY'
import json
def flat(a): return {x["key"]: list(x["value"].values())[0] for x in a}

spans = [(flat(r["resource"].get("attributes", [])).get("service.name", "?"), s)
         for r in json.load(open("/tmp/trace.json"))["trace"]["resourceSpans"]
         for ss in r["scopeSpans"] for s in ss["spans"]]
# Build the id map over ALL spans first: a parent can live in a later service block.
by_id = {s["spanId"]: f"{svc}/{s['name']}" for svc, s in spans}

for svc, s in spans:
    a = flat(s.get("attributes", []))          # plenty of real spans carry none
    st = s.get("status", {})                   # unset status serialises as {}
    print(f"{svc:12} {s['name']:34} {a.get('url.path','')} "
          f"{a.get('http.response.status_code','')} "
          f"{st.get('code','')} {st.get('message','')}")
    for ev in s.get("events", []):
        e = flat(ev.get("attributes", []))
        print(f"{'':12} • {ev['name']}: {e.get('exception.type','')} {e.get('exception.message','')}")
    if parent := s.get("parentSpanId"):
        print(f"{'':12} └─ parent: {by_id.get(parent, '(not in this trace)')}")
PY
fi
```

Every span names its parent, so "did this cross the boundary?" is answered by the `└─ parent:` line rather than by both service names merely appearing. Build `by_id` over all spans _before_ printing — resolving parents in the same pass mislabels any span whose parent lives in a later service block, which on a real 98-span evolve trace turned 62 correct links into `(not in this trace)` and reads exactly like a propagation break.

## Common mistakes

- Hardcoding `3000`, `3200` or `9090`. Those are container-internal; the host ports are the fixed shared ones (`GRAFANA_PORT`, `TEMPO_HTTP_PORT`, `PROMETHEUS_PORT`).
- Treating an empty result as proof of absence — for traces retry and widen the window, for metrics wrap in `last_over_time`.
- Querying a metric by its Go instrument name instead of the translated series name, or without a `service_name` filter.
- Reading `traces_service_graph_request_total` as proof of propagation either way. That graph is built from CLIENT/SERVER span pairs, so an edge appears only where the caller recorded a CLIENT span — real `deployments.evolve` traffic does produce `client="gram-server", server="gram-worker"`, but a path that dispatches without a CLIENT span shows no edge while propagating perfectly well. `parentSpanId` is the authority.
- Assuming the data you get back belongs to the worktree you ran `mise env` in. Several worktrees can have stacks up at once, and every port resolves to the one shared `gram-shared-lgtm-1` — the port tells you nothing. Separation comes only from the `worktree` resource attribute, so filter on it.
- Reading a counter as an all-time total. OTLP counters are cumulative per process, so a local restart starts them over.
