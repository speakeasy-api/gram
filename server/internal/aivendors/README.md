# AI client catalog snapshots

`Load` reads one database snapshot of the AI client catalog. The API, MCP
gateway and private ingress processes load it before serving. If the database
cannot be read, the process does not start. If the catalog is empty or invalid,
`Load` logs an error and returns the built-in catalog minus what the database
removed: a soft-deleted product is dropped, a URL whose only live rows are
disabled is disabled, and a URL with no live row is dropped. A live row with the
same id or URL is not a removal. If the removals would leave no product at all,
they are ignored and the full built-in catalog is served, so recorded blocks
keep applying. Other edits, such as a renamed product or
changed signatures, are not carried into the fallback. Reading the removals is
part of the read, so if it fails the process does not start. A process still
on the fallback applies later removals on its next refresh; a process serving a
database snapshot keeps that snapshot while the table is invalid. A fresh
environment that has not run the seed has nothing removed and serves
`CompiledSnapshot` itself. Only the `ai-client-catalog` startup seed and the
demo seed write catalog rows.

`CompiledSnapshot` lists the built-in registry in database order, so it gives
the same answers in the same order as a freshly seeded database.

`Store.Run` refreshes the store on a minute timer. Each refresh reads live
products and documents in a read-only, repeatable-read transaction. A full
content fingerprint detects changes, including edits that do not update
timestamps. A refresh that fails, panics, or finds an empty or invalid catalog
keeps the current snapshot. The first refresh that validates replaces the
built-in catalog. Each pod refreshes on its own timer, so for up to a minute
after an edit pods can serve different snapshots.

While the table is invalid, pods can disagree for longer than that. A running
pod keeps its last database snapshot, and a pod that boots meanwhile serves the
built-in catalog. If the two differ in a served field, a device that moves
between pods sees the scan list version change and refetches the list on each
poll until the table is fixed. Alert on `ai_catalog.built_in` being 1 on any
pod of a seeded environment, and fix the table; the pods converge on their next
refresh.

## Removing a product or document

A removed or disabled row stops admitting its client on presets issuers, stops
attributing it, and leaves the scan list and the presets list. An
organization's block on the product keeps applying. Each load also reads, in
the same transaction, the soft-deleted documents and every document of a
soft-deleted product, most recently removed first, up to
`maxRetiredDocuments`. When no live target matches a client at sign-in,
`Snapshot.LastKnown` names the product that last published its client ID: a
disabled live document first, then a removed exact URL, then the most specific
removed pattern. An enabled live document always wins, so a URL re-added under
another product follows that product. Nothing else reads these documents: they
never admit, never appear in presets, scan lists or device payloads, never
change attribution, and never count in validation. A malformed removed pattern
is dropped. The built-in fallback keeps them too, except when nothing is
removed or the removals would leave no product, where the built-in catalog
already lists every product.

An organization can still change or clear its existing decision on a removed
product, but cannot add a new one. A decision on a product whose documents are
all disabled stays editable for the same reason.

Before removing a row, check two things. A block only keeps applying while
the removed document stays within the cap, so hard-deleting rows or removing
more than `maxRetiredDocuments` documents drops the oldest blocks. And a
re-added URL under a different product moves the client to that product's
decision. To find the organizations affected:

```sql
SELECT count(*) FROM ai_scan_targets
WHERE (ai_product_id = '<PRODUCT_ID>' OR id = '<PRODUCT_ID>') AND status = 'blocked';
```

The inventory reads a removed product as unreviewed even while its block
applies, so tell affected organizations if they need to clear one.

Every load rejects an empty catalog, a malformed enabled pattern, more
products, documents per product, client info names or enabled patterns than the
caps, and two enabled patterns of different products that one client ID can
match unless one is strictly narrower. Then the consumer validator runs; the
`aitargets` one also checks every enabled document URL against the CIMD
client_id rules. Pattern hosts are not tied to the hosts a vendor already uses.
NULL elements in array columns are dropped and nested arrays are flattened when
read, so no single row can fail the read itself.

Metrics: `ai_catalog.refresh.failures` counts failed refreshes,
`ai_catalog.snapshot.age` is the seconds since the served snapshot last matched
the database (not reported while the built-in catalog is served), and `ai_catalog.built_in` is 1 while a process serves the
built-in catalog. A skipped row is logged by the load that first sees it, and a
refresh error is logged when it changes, not every minute.

Snapshots own their data and return copies. A compound operation should capture
one snapshot and use it throughout. Snapshots keep input order for products
and listings; database reads sort by id, then URL, in byte order. Listing order
is content, so it is part of the fingerprint. Exact URLs win over patterns;
among matching patterns the one with the most literal path segments wins.
A disabled document still wins its match, so its client is unknown rather than
a broader pattern's product; an enabled document with the same URL or
specificity wins over it.
Validation keeps overlapping patterns of different products strictly nested,
so a tie can only be between one product's own patterns, and goes to the lower
URL. The database's `is_pattern` discriminator is authoritative. A malformed
disabled pattern is logged and omitted; that is a successful partial load,
distinct from a database failure. Documents of soft-deleted products are read
only for block enforcement.

Full fingerprints drive refresh detection. The device scan list version is
derived from the served targets in `agent/aitargets`, not from the snapshot.

Product families and reported-name aliases stay in the compiled registry by
design: they change with plugin and telemetry code, so the database does not
store them and snapshots leave them empty.

Platform MCP parity: this package adds no reader, permission, or tool contract,
so no Platform MCP tool changes. Platform MCP only lists the inventory and
library, which leave removed products out as before; it has no decision tool.
