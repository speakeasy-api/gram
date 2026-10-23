# AI client catalog snapshots

API, MCP, and private ingress processes synchronously apply
`catalogseed.EnsureCatalog` and load one database snapshot before opening their
listeners. Startup therefore needs
write access to the reference tables. Existing edits and soft deletions survive
bootstrap. The worker remains responsible for the cross-organization decision
backfill; complete the reconciliation in `catalogseed/README.md` before reader
cutover.

Each process owns an injected `Store`. Its minute timer reads live products and
documents in a read-only, repeatable-read transaction. A full content fingerprint
detects changes, including edits that do not update timestamps. The current
catalog is small, so the reader deliberately fetches its contents instead of
relying on an unreliable timestamp watermark. Refresh errors retain the previous
snapshot. Device-target validation rejects invalid definitions and oversized
catalogs before publication, retaining the previous snapshot on refresh and
refusing startup on the first load. The loader requires the existing target
validator as a dependency so catalog loading does not depend on its consumers.
Shutdown cancels the poller and waits for it before closing the pool.

Snapshots own their data and return copies. Compound operations capture one
snapshot: gateway blocking uses the same view for defaults and client identity,
and target mutations use it for validation, overlays, and returned versions.
Exact URLs win over patterns; patterns prefer more literal path segments, then
URL and product ID to break ties. The database's `is_pattern` discriminator is
authoritative. Invalid patterns are logged and omitted from all projections;
that is a successful partial load, distinct from a database failure. A valid
empty catalog is published, including when staff soft-delete every product.
Soft deletion removes a product's automatic admission and gateway matcher
definitions; an empty catalog applies the same policy to the last product.
Retaining a last good snapshot is reserved for read or validation failures,
not valid staff deletions.

Vendor matchers require exactly one live product. If a refresh makes a blocked
custom vendor matcher ambiguous and no specific matcher resolves the caller,
the gateway returns a retryable unavailable error until the matcher is updated.
The complete organization overlay is also validated on reads: conflicting
matchers and combined target-count or payload-size violations are not served
to devices. Gateway checks fail with the same retryable error while a block is
in force, including when a new default claims a blocked custom target's URL.
Administrators can remove conflicting custom targets one at a time even when
other conflicts remain; serving resumes once the complete overlay is valid.

The device `list_version` is a positive 31-bit content-derived identifier. It is
stable but neither monotonic nor collision-free. Full fingerprints drive refresh
detection and participate in device ETags, so a version-label collision cannot
suppress a configuration update. Historical scan receipts with the same numeric
label are not guaranteed to identify the same catalog.

Platform MCP parity: the existing `list_ai_scan_library` and
`list_shadow_ai_inventory` tools expose the refreshed organization library and
detection attribution to authorized organization admins. Their schemas and
permissions are unchanged. ChatGPT tool visibility resolves the current product
identity on each operation. This internal storage change adds no new tool.
Database parity, refresh, gateway isolation, and Platform MCP recognition tests
cover these contracts. Demo provisioning already seeds the same reference rows.

The compiled registry remains only as a test baseline pending AIM-450. Production
readers receive the database store explicitly and do not fall back to it.
