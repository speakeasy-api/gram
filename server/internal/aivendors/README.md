# AI client catalog snapshots

`Load` reads one database snapshot of the AI client catalog. Nothing in
production loads one yet. `CompiledSnapshot` wraps the compiled registry in the
same `Snapshot` type. The API, MCP gateway and Platform MCP services read an
injected `Store` built from it, so every reader sees the registry's catalog
content (snapshots carry no families or aliases) until the database becomes the
source.

`Store.Run` can refresh a store on a minute timer, but nothing in production
runs it yet. Each refresh reads live products and
documents in a read-only, repeatable-read transaction. A full content
fingerprint detects changes, including edits that do not update timestamps. The
catalog is small, so the reader fetches its contents instead of relying on a
timestamp watermark. Refresh errors retain the previous snapshot. The loader
requires a validator, so a consumer can reject a catalog before it is
published; a rejected first load returns an error. Cancelling the context stops
the refresh loop.

Snapshots own their data and return copies. A compound operation should capture
one snapshot and use it throughout. Snapshots keep input order for products,
listings and patterns: exact URLs win over patterns, and patterns are tried in
input order, as the compiled registry does today. Order is content, so it is
part of the fingerprint. Database reads sort by id, then URL, in byte order.
`CompiledSnapshot` has no families or aliases, like a database snapshot. The database's
`is_pattern` discriminator is authoritative. Invalid patterns are logged and
omitted from all projections; that is a successful partial load, distinct from
a database failure. A valid empty catalog is published, including when staff
soft-delete every product. Retaining a last good snapshot is reserved for read
or validation failures, not valid staff deletions.

`Snapshot.Version` is a positive 31-bit content-derived identifier. It is stable
but neither monotonic nor collision-free; full fingerprints drive refresh
detection.

Product families and reported-name aliases stay in the compiled registry by
design: they change with plugin and telemetry code, so the database does not
store them and snapshots leave them empty.

Platform MCP parity: this package adds no reader, permission, or tool contract,
so no Platform MCP tool changes.
