# AI client catalog bootstrap

`catalog.json` holds 53 products and 26 CIMD documents. It tracks the compiled
`aivendors` registry until that registry is removed (AIM-450), then becomes
frozen. `TestCatalogMatchesRegistry` fails when any field differs, including a
field newly added to the registry. Reported-name aliases and product families
are not in the file or the database: they change with plugin and telemetry
code, so they stay in the registry (`Product.Aliases`, `Product.Family`,
`aivendors.Identify`).

Regenerate the file from the registry, then format it:

```sh
mise run test:server ./internal/aivendors/catalogseed/ -run TestCatalogMatchesRegistry -args -update
mise exec -- aube exec oxfmt -- --write server/internal/aivendors/catalogseed/catalog.json
```

Production is the source of truth after bootstrap. The seed only inserts, so a
registry change reaches existing rows only as new products or documents. New
clients belong in the admin review queue. API, MCP, and private ingress
processes synchronously ensure reference data and load a database snapshot
before serving. They refresh that snapshot about once a minute.

The `ai-client-catalog` startup seed hashes the embedded file and its application
revision. Workers apply each version through the existing startup workflow.
Normal cost is one workflow start and one activity per version per task queue,
plus retries during rollouts; there is no periodic schedule.

Products retain their stable text IDs. Documents get deterministic bootstrap
IDs, so editing their URL, disabling them, or soft-deleting them survives a
rerun. Existing URLs, including soft-deleted rows, are left alone. Documents
are not inserted under deleted products. Arbitrary URL history of preexisting
non-bootstrap rows is not recoverable after their URL has been edited.

Loading rejects unknown fields and a document URL under two products.

Catalog rows must only ever be soft-deleted: the next run reinserts a
hard-deleted seed row.

Catalog inserts commit in one transaction and the decision backfill runs in a
second, so a failed backfill never leaves the catalog empty. Each successful
run logs the seed version and the counts inserted and linked; a failed run
surfaces through the startup seed activity's error.

The seed fills only missing references on decision-only rows for the frozen
scan library, and only to live products. It preserves definitions, existing
references, decisions, rationales and timestamps. A reference to a product that
is soft-deleted later stays as decision history; live catalog readers exclude
soft-deleted products. Future built-in decision writes set the reference
directly; while the product is missing or soft-deleted they return a retryable
unavailable error.

After all previous-version API replicas have stopped during the initial rolling
deployment, run the deployed binary with its configured database URL and site
URL (`--database-url` / `GRAM_DATABASE_URL` and `--site-url` / `GRAM_SITE_URL`,
both required):

```sh
gram app-seed --name ai-client-catalog
```

This direct command repeats the idempotent backfill even when Temporal already
completed the seed version. It links decisions written by old replicas after
the initial backfill. Complete this reconciliation before switching readers
to database-backed classification in AIM-449.

Demo provisioning calls `EnsureCatalog` before its tenant transaction. This
inserts reference data without running the cross-organization backfill. Demo
decisions reference the seeded products and postflight requires all five links.
This checks reference integrity, not product liveness: staff deletions remain
authoritative and must not make demo provisioning fail.
Isolated demo tests seed the global catalog before taking their safety baseline.

Platform MCP needs no new tool for process-startup initialization of global
reference data or the worker-owned decision backfill. The existing Shadow AI
inventory/library and client admission tools retain their readers, permissions
and result contracts. Catalog and decision integration tests verify persistence
and refreshed reader behavior.
