# AI client catalog snapshots

`Load` reads one database snapshot of the AI client catalog. The API, MCP
gateway and private ingress processes ensure the bootstrap rows exist, load a
snapshot before serving, and refuse to start when that first load fails. The
services read an injected `Store` built from it.

`Store.Run` refreshes the store on a minute timer. Each refresh reads live
products and documents in a read-only, repeatable-read transaction. A full
content fingerprint detects changes, including edits that do not update
timestamps. The catalog is small, so the reader fetches its contents instead of
relying on a timestamp watermark. A failed refresh, or one that panics, keeps
the previous snapshot. Each pod refreshes on its own timer, so for up to a
minute after an edit pods can serve different snapshots. Cancelling the context
stops the refresh loop.

Every load runs `Validate`, then a consumer validator, before a snapshot is
published. `Validate` rejects an empty catalog, products over the caps, a
scanned product without a category, and two enabled patterns of different
products that one client ID can match, unless one pattern is strictly narrower.
Pattern hosts are not tied to the hosts a vendor already uses.

`ai_catalog.refresh.failures` counts failed refreshes and
`ai_catalog.snapshot.age` reports how long ago the served snapshot last matched
the database. A skipped row or a repeated refresh error is logged when it first
appears, not every minute.

Snapshots own their data and return copies. A compound operation should capture
one snapshot and use it throughout. Snapshots keep input order for products
and listings. Exact URLs win over patterns; among matching patterns the one with
the most literal path segments wins, then the lower URL, then the lower product
id, so input order never decides attribution. Listing order is content, so it is
part of the fingerprint. Database reads sort by id, then URL, in byte order.
The database's `is_pattern` discriminator is authoritative. Invalid patterns
are logged and omitted from all projections; that is a successful partial load,
distinct from a database failure. An empty catalog is rejected, so soft-deleting
every product keeps the last snapshot.

Full fingerprints drive refresh detection. The device scan list version is
derived from the served targets in `agent/aitargets`, not from the snapshot.

Product families and reported-name aliases stay in the compiled registry by
design: they change with plugin and telemetry code, so the database does not
store them and snapshots leave them empty.

Platform MCP parity: this package adds no reader, permission, or tool contract,
so no Platform MCP tool changes.
