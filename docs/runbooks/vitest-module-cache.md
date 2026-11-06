# Vitest transformed-module cache

## Producer and consumers

`client-test` keeps its existing PR/merge-queue gates and runs the complete dashboard,
admin and registry-editor commands regardless of a cache hit. Only
`node_modules/.vitest-cache`, including `_metadata.json`, is persisted. This is
transformed code, not test results; assertions and isolation are unchanged.

Blacksmith transparently serves `actions/cache`. With branch-protected caches,
a queue ref's saved entry is not a shared cache for later queue refs. The
`vitest-module-cache` job publishes a trusted `refs/heads/main` entry after a
client-affecting main push. It checks out the push's exact SHA; it never downloads
PR artifacts or checks out PR code. Branch protection remains enabled.

The producer uses the same runner class, Node installation, dependency install,
working directories and dashboard/admin test commands as consumers. It shares the
compatibility-key composite action with them. It is independent of CI Gate and
all required tests, has a 15-minute timeout and is best-effort. A failed warm-up
never reaches the save step. Cache lookup failure stops only the optional producer;
consumer restore/save failures remain non-blocking.

## Bounded publishing and races

The producer first does a lookup-only check for `<prefix>main-YYYY-MM-DD` (UTC).
An exact hit skips dependency installation and both suites. Otherwise it restores
an older compatible entry, runs both suites and saves the daily entry. Thus there
is at most one successful warm-up per UTC day **per compatibility prefix**, not
one full-suite run on every main push. Failed attempts can retry on later pushes.
Checkout, Mise setup and lookup still cost time on cache-hit pushes.

A fixed main-only concurrency group serializes producers without cancelling a
running warm-up. GitHub may replace pending jobs with newer pushes; that is fine
for this best-effort cache. Immutable cache keys make simultaneous reservations
safe as well. An older main snapshot is valid: source content is part of every
Vitest module's transform key. New/edited modules are transformed by consumers.
No new entry is guaranteed until a qualifying main push completes successfully.

Consumers retain SHA-suffixed keys and fallback only to the shared compatibility
prefix. Main entries provide a cross-ref seed; PR/queue entries remain scoped by
the existing backend policy. There is no global or dependency-agnostic fallback.

## Invalidation

`.github/workflows/composite/vitest-cache-key/action.yaml` preserves the original
`vitest-modules-blacksmith-v1` boundary:

- Runner OS and architecture, exact `node --version`.
- Explicit `pnpm-lock.yaml` and `pnpm-workspace.yaml`.
- Root and client workspace package manifests.
- Root/client TypeScript configs, client Vitest/Vite configs and the dashboard
  development-slot plugin.

Add new transform-affecting configuration inputs to that shared action. Bump the
namespace to invalidate all persisted entries. Do not remove the explicit lockfile:
installation can change which lockfile Vitest discovers. Vitest additionally hashes
module source, environment and configuration and checks its own cache metadata.
Do not add ordinary source files to the outer compatibility hash: unchanged modules
must remain reusable when another module changes.

## Evidence and limitations (GRW-420)

The original reproduction is confirmed in GitHub logs:

| Queue job                                                                                       | Dashboard / admin                   | Restore / save evidence               |
| ----------------------------------------------------------------------------------------------- | ----------------------------------- | ------------------------------------- |
| [128108368422](https://github.com/speakeasy-api/gram/actions/runs/42280580704/job/128108368422) | 260.13s / 56.63s (Vitest durations) | Miss 19:33:51 UTC; saved 19:39:15 UTC |
| [128116178275](https://github.com/speakeasy-api/gram/actions/runs/42282826999/job/128116178275) | 208s / 56s (GitHub steps)           | Miss 19:49:17 UTC; save step 2s       |

Both queue refs had identical Linux/X64, Node v24.22.2 and configuration prefixes.
The old workflow's `verify == false` on pushes prevented a main-scoped producer.
Blacksmith documents branch-protected caches, but the repository setting and actual
backend scope/version inventory have **not** been inspected. Ref scoping is the
leading explanation, not a confirmed backend finding. GitHub's cache API is not
authoritative for Blacksmith. See [Blacksmith scoping](https://docs.blacksmith.sh/blacksmith-caching/dependencies-actions#branch-protected-caches).

A local Node v24.22.2 / Vitest 5.0.5 probe of `roleGrantTransform.test.ts` passed
six assertions in each run: cold 0 reads/14 misses, warm 14 reads/0 misses, then
13 reads/1 miss after a source-only edit. The edited module was not read from
its old transform; an unchanged dependency was reused. The temporary edit and
`DEBUG=vitest:cache:fs` instrumentation were removed. This proves local source
invalidation, **not** cross-ref CI reuse or CI speedup.

Historical cold dashboard/admin durations imply roughly 4.4–5.3 test minutes
(17.5–21.1 allocated vCPU-minutes on four vCPUs) per successful full warm-up,
before setup/cache overhead. This is not measured CPU utilization. Daily entries
amortize that work across consumers, but dependency/config changes create new
prefixes and can require more than one warm-up per day. Net benefit is unproven;
include every producer invocation, including hit-only setup and failed attempts,
in the cost calculation. The old same-ref benchmark is not cross-ref evidence.

## Post-merge verification and troubleshooting

1. In Blacksmith, inspect the repository's Branch Protected Caches setting and
   cache inventory (key, version, scope, architecture and transparent-cache flag).
   Keep protection enabled. The CLI supports:
   `blacksmith cache list --repo speakeasy-api/gram --search vitest-modules-blacksmith-v1 --format json`.
2. Record the successful main producer's run/job URL, exact revision, compatibility
   prefix, `refs/heads/main` scope and cache version. Check that metadata is saved.
3. Observe **two distinct normal merge-queue refs** with that compatible prefix
   after publication. Record restore/save step durations and dashboard/admin times.
   Same-ref retries do not satisfy this check.
4. For a bounded diagnostic run, temporarily enable `DEBUG=vitest:cache:fs` on both
   test steps. Count actual `[read]` events and `[empty]` misses; an Actions cache
   hit alone is insufficient. Remove instrumentation afterward. Record a source
   edit re-transforming its module while unchanged modules are read from cache.
5. Confirm a dependency/config edit produces a different prefix without restoring
   the prior prefix. A module-cache miss must still run all normal tests.
6. If restore misses, compare the _entire_ prefix, backend version and scope,
   cache path, successful producer completion and eviction/retention. If restore
   succeeds but reads do not, inspect Vitest's metadata/lockfile messages and
   environment/configuration hashes (including absolute checkout paths).
7. Record consumer savings minus restore/save overhead and all producer compute.
   If bounded publishing does not pay for itself, remove persistence rather than
   expanding into a new cache platform or weakening test coverage.

Cross-ref production validation and net benefit remain pending until the producer
has landed and normal queue traffic has consumed its entry.
