# AI client catalog bootstrap

`catalog.json` holds 53 products, 41 aliases and 26 CIMD documents. It tracks
the compiled `aivendors` registry until that registry is removed (AIM-450), then
becomes frozen. `TestCatalogMatchesRegistry` fails when the two differ.
Regenerate the file from the registry, then format it:

```sh
mise run test:server ./internal/aivendors/catalogseed/ -run TestCatalogMatchesRegistry -args -update
mise exec -- aube exec oxfmt -- --write server/internal/aivendors/catalogseed/catalog.json
```

Production is the source of truth after bootstrap. The seed only inserts, so a
registry change reaches existing rows only as new products, aliases or
documents. New clients belong in the admin review queue. This seed does not
change the compiled catalog readers; that transition belongs to AIM-449.

The `ai-client-catalog` startup seed hashes the embedded file and its application
revision. Workers apply each version through the existing startup workflow.
Normal cost is one workflow start and one activity per version per task queue,
plus retries during rollouts; there is no periodic schedule.

Products retain their stable text IDs. Documents get deterministic bootstrap
IDs, so editing their URL, disabling them, or soft-deleting them survives a
rerun. Existing URLs, including soft-deleted rows, are left alone. Documents
are not inserted under deleted products. Arbitrary URL history of preexisting
non-bootstrap rows is not recoverable after their URL has been edited.

Aliases follow the same rules keyed by name: deterministic IDs, and an
existing alias with the same folded name (live or soft-deleted, under any
product) is left alone. The fold drops case, spaces, tabs, underscores and
hyphens, which matches the hyphen-free key `Identify` looks up; Go and Postgres
fold ASCII names identically. None are inserted under deleted products. A test
rejects a file where one folded name belongs to two products, and loading
rejects unknown fields, unknown alias clients and a URL under two products.

A product's family is written only when the product is inserted, so a family
staff later set or cleared survives a rerun.

Catalog rows must only ever be soft-deleted: the next run reinserts a
hard-deleted seed row.

Catalog inserts commit in one transaction and the decision backfill runs in a
second, so a failed backfill never leaves the catalog empty. Each run logs the
seed version and the counts inserted and linked.

The seed fills only missing references on decision-only rows for the frozen
scan library. It preserves definitions, existing references, decisions,
rationales and timestamps. Future built-in decision writes set the reference
directly; before the product exists they return a retryable unavailable error.

After all previous-version API replicas have stopped during the initial rolling
deployment, run the deployed binary with its configured database connection:

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
Isolated demo tests seed the global catalog before taking their safety baseline.

Platform MCP needs no new tool for this worker-owned initialization of global
reference data. The existing Shadow AI inventory/library and client admission
tools retain their readers, permissions and result contracts. Catalog and
decision integration tests verify the new persistence behavior; reader changes
remain in AIM-449.
