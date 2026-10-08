# Storage subscriptions

Storage subscriptions declare a dedicated Go-owned consumer that writes an
analytical representation to Cloud Storage. They are separate from application
subscriptions: an application handler must not compete for messages intended for
storage. The proto declaration and bucket topology are implemented here; the
generated Parquet encoder and installable Go runner are subsequent layers.

```proto
message EventArchive {
  option (gcp.pubsub.v1.storage_subscription) = {
    topic: "gram.events.v1.Event"
    ack_deadline: { seconds: 60 }
    dead_letter: { max_delivery_attempts: 10 }
  };
}
```

Run `mise run gen:infra` after changing declarations. Commit the generated
artifacts. A marker cannot declare more than one of `topic`, `subscription`, and
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

The runtime layer will enforce the value grammar and size limits and permanently
ack/drop deliveries with missing or malformed routing metadata, incrementing a
finite-reason drop counter. Such deliveries do not retry or enter the DLQ.

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

The deployment chart must consume these fragments and:

1. Resolve a globally unique physical bucket name, normally using project number
   plus logical name, and an environment component if environments share a
   project. Validate the complete physical name. Support explicit overrides for
   collisions rather than silently adopting an unrelated bucket.
2. Render `StorageBucket` resources using that physical identity and the supplied
   annotations and privacy settings. Add project, namespace, location and other
   deployment policy.
3. Provision writer IAM and inject the exact same logical-to-physical mapping
   into consumer processes. The runner will not create production buckets.
4. Preserve resources when declarations disappear. `deletion-policy: abandon`
   prevents Config Connector from destroying the bucket when its Kubernetes
   resource is removed; authorized direct GCS deletion remains an out-of-band
   operation. Do not introduce an implicit object-expiration lifecycle rule.

This chart integration is required before any storage consumer is deployed.
Generating or merging values does not itself provision infrastructure. Public
access prevention enforces private IAM access; network restrictions for
authenticated callers are a separate deployment concern.

## Analytical delivery contract

The runner will acknowledge only after the Parquet footer and GCS object commit
succeed. Delivery remains at least once: a crash between commit and ack can
produce duplicates. Rows carry Pub/Sub message IDs for transport deduplication;
publisher retries may require payload identity for semantic deduplication.

Parquet is an analytical projection, not lossless protobuf archival. Fields and
types must remain compatible; incompatible changes require a new payload type.
The encoder's generated schema and field mappings will be committed for review.
