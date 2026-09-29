# Product metrics

Internal tenant-scoped analytical measurements, independent of product facts,
billing attribution and existing raw agent metrics. Producers register code-owned
definitions before publishing. No customer API, dashboard, exporter or production
producer is supplied here.

## Contract

- Counter inputs are nonnegative monotonic **delta increments**. Histogram inputs
  are individual observations, aggregated as count/sum/min/max, without buckets or
  quantiles. Neither input is an invented OTLP point type. Cumulative counters,
  gauges and preaggregated intervals are unsupported.
- Names follow OTel metric syntax. Gram-owned names use `gram.<domain>.<quantity>`;
  use UCUM units (`s`, `By`, `1`, `{evaluation}`), not units in names. Producers
  are responsible for choosing valid UCUM units and semantic conventions.
- A registry rejects incompatible unit/instrument definitions for a scope/name,
  including across instrumentation versions. Rename incompatible metrics. A scope
  version identifies instrumentation, not automatically a new metric meaning.
- Organization and project are mandatory typed ownership. The canonical resource
  adds custom `gram.organization.id` and `gram.project.id` attributes for future
  export. Producers must omit these reserved resource keys.
- Resource, scope and point attributes have separate namespaces. Sorted keys and
  explicit scalar/array type tags preserve identity, including missing vs empty,
  int64 vs float64, and empty arrays. Duplicate keys and nonfinite values fail.
  Descriptor description is metadata. Integer values never pass through float64.
- Producers own dimensions and cardinality. There is no central dimension allowlist,
  identity enrichment, series catalogue or hard series cap. Do not automatically
  add user IDs, delivery IDs or observation IDs as dimensions.
- Contribution IDs identify **observations**, not series. Allocate distinct IDs
  for distinct measurements, even if values and timestamps coincide. Retry with
  the same ID and unchanged payload. Identity is scoped by tenant and instrumentation
  scope. Best-effort batch suppression is not an exactly-once guarantee.
- Await the publisher result. Product transaction/outbox integration stays with
  each producer; a database commit and direct Pub/Sub publish are not atomic.

## Assessment

Temporal actions/month: **0**. Event-driven ingestion belongs in a Pub/Sub batch
handler, without workflow starts or signals.

Platform MCP: omit. The outcome is internal contribution ingestion for trusted
code-owned producers, targeting tenant-scoped analytical storage. There is no
member/admin operation or public authorization contract to expose, and no existing
tool's customer behavior changes. Contract tests provide synthetic evidence.
Demo seed: no dashboard data consumer, so use deterministic synthetic fixtures.
