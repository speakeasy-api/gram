# Storage subscriptions

Storage subscriptions declare a dedicated Go-owned consumer that writes an
analytical representation to Cloud Storage. They are separate from application
subscriptions: an application handler must not compete for messages intended for
storage. Generation emits the bucket topology, explicit Parquet schemas, typed
Go encoders and reviewable schema manifests. A consuming process explicitly
installs its generated runner.

```proto
message EventArchive {
  option (gcp.pubsub.v1.storage_subscription) = {
    topic: "gram.events.v1.Event"
    ack_deadline: { seconds: 60 }
    dead_letter: { max_delivery_attempts: 10 }
  };
}
```

Run `mise run gen:infra` after changing declarations. Commit `infra/gen/`,
`infra/gen_py/` and `infra/pkg/storagebindings/` artifacts. A marker cannot
declare more than one of `topic`, `subscription`, and
`storage_subscription`. Subscription IDs, retention, retries and synthesized DLQs
follow the ordinary subscription conventions and share their collision checks.

## Eligibility

Storage consumers require a topic whose generated schema is **attached**. Topics
with an explicit name override currently have no attached schema and are
ineligible, even when a standalone schema is generated for their payload.

Payloads must be self-contained in their topic proto file, consistent with the
existing single-file Pub/Sub schema contract. Every reachable message and enum is
checked, including repeated elements, map values, and oneof alternatives.
External types such as `google.protobuf.Timestamp`, `Duration`, wrappers, and
imported enums fail generation with the offending field path. Configuration
options may still use Google's descriptor and duration types.

Recursive payload graphs fail generation with a cycle path. Reusing an acyclic
nested message in multiple fields is allowed. In particular, recursive OTEL
`AnyValue` payloads cannot opt in to the first Parquet mapping.

## Codec and directories

The unspecified codec permanently resolves to `STORAGE_CODEC_PARQUET`. No other
codec is supported initially. Unknown enum values fail generation.

The unspecified partitioning permanently resolves to **daily**, following the
ingestion-time layout:

```text
gram.events.v1.EventArchive/part__year=2026/part__month=10/part__day=07/<unique>.parquet
```

`STORAGE_PARTITIONING_HIVE_HOURLY` adds `part__hour=14`. Times are UTC receipt
times, not publisher event times. The prefix is the marker's full proto name,
even when its Pub/Sub subscription ID is overridden. Renaming the marker changes
the prefix.

For publisher-controlled directories, declare the attribute and ordered keys:

```proto
partitioning: STORAGE_PARTITIONING_HIVE_EXTERNAL
partition_attribute: "storage_partition"
partition_keys: "region"
partition_keys: "date"
```

The runner contract is to preserve a suffix such as
`region=eu-west1/date=2026-10-07` verbatim beneath the marker prefix. External
keys are not amended with `part__`. All messages must use the declared key
sequence; changing it after writing files requires a new marker/prefix so Hive
readers do not encounter inconsistent partition schemas.

Keys must match `[a-z][a-z0-9_]{0,62}`; declare between one and eight unique keys.
Keys cannot collide case-insensitively with top-level payload columns. External
configuration fields are rejected for built-in partition modes.

The `partition_attribute` key must be nonempty, at most 256 bytes, and have no
surrounding whitespace. It must not start with `goog`, case-insensitively.

Values must match `[A-Za-z0-9_-][A-Za-z0-9._-]{0,127}`. The full suffix is limited
to 512 ASCII bytes and exactly the declared sequence of keys. There is exactly
one `=` per pair. No percent escapes, whitespace, Unicode, backslashes, empty
components, leading/trailing slashes or additional components are accepted.
`NULL` and `__HIVE_DEFAULT_PARTITION__` are rejected case-insensitively.

Missing or malformed routing metadata is permanently acked and dropped. It
does not retry or enter the DLQ. The counter
`storage_subscription_dropped_messages` carries `proto_message` (the kebab-cased
marker name, independent of transport overrides) and one of:

- `missing_partition_attribute`
- `malformed_partition_attribute`
- `partition_attribute_limit_exceeded`

Partition values never become metric labels or log fields.

## Bucket topology and deployment contract

`bucket` is optional and defaults to the **logical** name `lake`. Set it explicitly
(for example, `bucket: "event-archive"`) to select a different logical bucket.
Names must be 3–40 lowercase letters, digits or hyphens, starting with a letter
and ending with a letter or digit. Multiple storage subscriptions using the
default or explicitly selecting `lake` share one bucket fragment, with separate
marker prefixes. An explicitly empty bucket name is invalid.

The generated Helm values contain a `storage` section only when buckets exist:

```yaml
storage:
  apis:
    - storage.googleapis.com
  buckets:
    - name: lake
      annotations:
        cnrm.cloud.google.com/deletion-policy: abandon
        cnrm.cloud.google.com/force-destroy: "false"
      labels:
        managed_by: proto-pubsub-orchestrator
      spec:
        publicAccessPrevention: enforced
        uniformBucketLevelAccess: true
```

The companion chart in `gram-infra/infra/helm/gram` consumes these fragments and:

1. Resolve a globally unique physical bucket name, normally using project number
   plus logical name, and an environment component if environments share a
   project. Validate the complete physical name. Support explicit overrides for
   collisions rather than silently adopting an unrelated bucket.
2. Render `StorageBucket` resources using that physical identity and the supplied
   annotations and privacy settings. Add project, namespace, location and other
   deployment policy.
3. Provision bucket-scoped `roles/storage.objectCreator` and inject the exact same
   logical-to-physical mapping as `GRAM_STORAGE_BUCKETS` JSON into server, worker
   and streams. The runner never creates production buckets.
4. Preserve resources when declarations disappear. `deletion-policy: abandon`
   prevents Config Connector from destroying the bucket when its Kubernetes
   resource is removed; authorized direct GCS deletion remains an out-of-band
   operation. Do not introduce an implicit object-expiration lifecycle rule.

This chart integration is required before any storage consumer is deployed.
Generating or merging values does not itself provision infrastructure. Public
access prevention enforces private IAM access; network restrictions for
authenticated callers are a separate deployment concern.

## Analytical delivery contract

The runner acknowledges only after the Parquet footer and GCS object commit
succeed. Delivery remains at least once: a crash between commit and ack can
produce duplicates. Rows carry Pub/Sub message IDs for transport deduplication;
publisher retries may require payload identity for semantic deduplication.

Parquet is an analytical projection, not lossless protobuf archival. Fields and
types must remain compatible; incompatible changes require a new payload type.
The encoder's generated schema and field mappings are committed for review.

## Install in a Go process

The generated function name is the marker's Pascal-cased full name. Install it
in the process's existing errgroup, sharing that process's broker, GCS client,
logger and OpenTelemetry provider:

```go
buckets, err := storage.ParseBucketMapping(os.Getenv("GRAM_STORAGE_BUCKETS"))
if err != nil {
    return err
}
group.Go(func() error {
    return storage.Run(ctx, storagebindings.GramEventsV1EventArchive(), storage.Config{
        Broker: broker,
        Store: &storage.GCSStore{Client: gcsClient},
        Buckets: buckets,
        Logger: logger,
        MeterProvider: meterProvider,
    })
})
```

Imports are `infra/pkg/storage` and `infra/pkg/storagebindings` under the Gram
Go module. There is no application message handler. A missing bucket mapping,
stale generated binding or invalid lease budget fails startup. Both production
and emulator brokers expose a dedicated storage subscriber path; application
subscriber helpers reject storage markers. Python has no storage runner.

For a runnable, isolated example:

```sh
mise run demo:storage --out /tmp/opencode/storage-demo
```

It uses the real Go Pub/Sub client against an in-process test broker, the generated
fixture binding, and a local create-only file store. The GCS path is exercised
by the runner's HTTP protocol tests; this demo needs no cloud credentials.

## Mapping v1

`infra/pkg/storagebindings/storage_gen.go` contains explicit schemas and typed
accessors; `storage_manifest.json` lists schemas, field numbers and fingerprints.
`parquet-go` is pinned for file framing/compression. No schema inference or
runtime protobuf reflection determines columns.

- Payload fields retain their proto names. Signed/unsigned integers retain
  their widths and signedness; floats retain precision; enums store INT32 numbers,
  including unknown values. Strings are STRING; bytes are BYTE_ARRAY.
- Explicit presence uses nullable columns: absent is NULL and an explicitly set
  zero/empty value stays present. Implicit scalars emit their default values.
- Nested messages are structs. Every nested message group has an always-true
  `__present` witness, so an empty message has a leaf and can later gain fields
  without changing representation. An absent parent remains NULL.
- Repeated fields use standard three-level LIST; maps use standard MAP with
  sorted keys. Empty collections stay empty; message elements/values have the
  same witness and field-presence rules as other nested messages.
- Oneof alternatives are nullable columns at their original level. Nullable
  `__oneof_<name>` stores the selected **protobuf field number**. Unset is NULL;
  selected empty/zero values remain distinguishable. Added alternatives require
  regenerated consumers before their values appear in this projection.
- Reserved prefixes `__` and `part__`, case-insensitive sibling collisions,
  excessive schema depth and excessive expanded column counts fail generation.
- `__pubsub` contains `message_id`, transport `topic`, and `received_micros`
  (Unix epoch microseconds). Mapping version and schema fingerprint are in file
  metadata and GCS object metadata, not directory names.

Unknown protobuf fields are not projected. Keep fields and types compatible;
abandon obsolete producer fields rather than removing them. A new incompatible
representation needs a new message/marker and therefore a new prefix. Changing
the external key list or built-in partition mode on existing data also requires
a new prefix. DuckDB's `union_by_name=true` handles additive file schemas.

## Batching and failure behavior

Defaults: 10,000 messages, 32 MiB raw input, 30-second maximum accumulation age,
128 partitions, four concurrent encode/upload workers, two-minute **whole-batch**
processing timeout, ten-minute lease extension, 20,000-message/128 MiB admitted
input budget. Set `Config.Settings` to override positive values.

There is one processing batch and one accumulating/pending batch, with at most
one carry message when a new partition exceeds the limit. Permits remain held
through settlement. A larger-than-budget single message acquires the full byte
budget exclusively; a byte-triggered batch may overshoot by one message. These
are raw-input bounds: decoded values, Parquet pages and the SDK's separately
bounded outstanding deliveries add to process memory. No complete encoded file
is buffered. Row groups, column pages and GCS upload buffers are also bounded.

Each batch is split into immutable, uniquely named objects by partition. A
successful object is acked independently of sibling failures. Decode errors are
nacked individually; failed encodes/uploads are nacked and can reach the
subscription's configured DLQ. GCS uses a does-not-exist precondition. An
ambiguous commit is nacked rather than assumed successful; redelivery can create
another object with duplicate rows. Shutdown cancels uncommitted work and settles
messages while the Pub/Sub iterator is still alive. A known committed object
remains acked even if cancellation races with settlement.

Other metrics are `storage_subscription_uploaded_objects`,
`storage_subscription_written_messages`, `storage_subscription_failures`
(`payload_decode` or `object_write`), `storage_subscription_unsettled_messages`,
`storage_subscription_unsettled_bytes`, and
`storage_subscription_object_write_duration`. They share the stable marker label.

## Querying

```sql
SELECT *
FROM read_parquet(
  '/tmp/opencode/storage-demo/fixture.v1.Archive/**/*.parquet',
  hive_partitioning = true,
  union_by_name = true,
  hive_types_autocast = false
);
```

Disabling Hive autocasts preserves identifiers such as `account=001`; otherwise
DuckDB can infer numeric/date types. Explicit `hive_types` is another way to
choose partition column types. Transport deduplication uses
`(__pubsub.topic, __pubsub.message_id)`; publisher retries may need payload IDs.

BigQuery can read compatible files as external Parquet tables:

```sql
CREATE EXTERNAL TABLE `<PROJECT_ID>.<DATASET>.event_archive`
WITH PARTITION COLUMNS
OPTIONS (
  format = 'PARQUET',
  uris = ['gs://<BUCKET>/gram.events.v1.EventArchive/*'],
  hive_partition_uri_prefix = 'gs://<BUCKET>/gram.events.v1.EventArchive',
  enable_list_inference = true
);
```

BigQuery has its own compatibility limits: UINT64 values above 9,223,372,036,854,775,807
cannot load as INT64; nested depth and row-size quotas apply; MAPs are represented
as repeated key/value records rather than a native map type. Wildcard loads also
require matching schemas and column positions. Use a schema-specific subset or
separate external tables for evolving schemas instead of assuming DuckDB's
`union_by_name` behavior. The automated interoperability oracle is pinned DuckDB;
BigQuery guidance follows its documented limits and requires a cloud smoke test
for the actual selected payload.

Sources: [DuckDB Hive partitioning](https://duckdb.org/docs/current/data/partitioning/hive_partitioning.html),
[BigQuery Parquet loading](https://docs.cloud.google.com/bigquery/docs/loading-data-cloud-storage-parquet),
[BigQuery Hive queries](https://docs.cloud.google.com/bigquery/docs/hive-partitioned-queries).

## Rollout and retirement

Land controller permissions and deployment mapping, roll forward the generated
topology, then deploy an explicit consumer registration. The existing
`gram.metering.v1.MeterReading` is a nonrecursive, schema-attached candidate for a
first opt-in; recursive OTEL payloads are ineligible. Select the concrete marker,
filter and consuming process when opting in. This framework ships with isolated
fixtures rather than an automatically enabled production subscription.

Retire the consumer and drain outstanding deliveries before removing its marker.
Bucket/resource deletion is deliberately out of band. Platform/Admin MCP and demo
org data need no changes: this is process-owned infrastructure with no management
API or dashboard workflow.
