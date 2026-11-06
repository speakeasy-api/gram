# Vitest transformed-module cache

## PR → merge queue → main

`client-test` keeps its existing gates and runs the complete dashboard, admin and
registry-editor commands regardless of a cache hit. Only
`node_modules/.vitest-cache`, including `_metadata.json`, is persisted. This is
transformed code, not test results; assertions and isolation are unchanged.

1. **PR runs** restore their own compatible cache, falling back to main, and save
   updated transforms after successful tests. Vitest re-transforms edited source
   modules while reusing compatible unchanged modules.
2. **Merge-queue runs** do the same, then upload the transforms as a seven-day
   artifact named `<prefix><sha>-attempt-<run_attempt>`. Ordinary PR runs do not
   upload this artifact. Upload failures do not fail the required test job.
3. **Main pushes** run the independent `vitest-module-cache` job. Like the existing
   image retagging flow, it reuses the queue's output for the exact commit rather
   than repeating work. It installs only Node to calculate the same compatibility
   prefix, selects a verified artifact, downloads it, checks metadata exists and
   saves `<prefix><sha>` in main's cache scope. **No dependency install, test rerun,
   daily key or scheduled warming.**

GitHub documents that runs can restore caches from their current/default branch,
but main cannot restore a PR's cache. An explicit artifact handoff bridges that
scope boundary; merging does not promote cache entries automatically. Blacksmith
transparently serves `actions/cache` with its own branch-protection setting. This
workflow does not change that setting. See [GitHub cache restrictions](https://docs.github.com/en/actions/reference/workflows-and-actions/dependency-caching#restrictions-for-accessing-a-cache).

## Trust and failure handling

`.github/scripts/vitest-cache-source.mjs` accepts only:

- A completed, successful `merge_group` run of `.github/workflows/pr.yaml` for the
  **exact main push SHA**, same repository/head repository, targeting main's queue.
- A successful `client-test` job in that run's exact attempt, with all three normal
  test steps successful.
- One unexpired artifact whose name exactly matches the expected compatibility
  prefix, SHA and attempt, and whose API provenance matches the run/repository.

The download action verifies the artifact digest. No downloaded code is executed
by the publishing job. A PR artifact, older attempt, nearby revision, incompatible
configuration or ambiguous artifact is not accepted. Lookup/API/download/metadata
failures cannot publish a cache or suppress required tests. If the queue workflow
has not finished yet, or no eligible artifact exists, publishing skips; rerunning
that main job after completion can retry. There is no fallback test execution.

The optional job has a five-minute timeout and is not a CI Gate dependency.
Immutable SHA keys make concurrent publishers safe: different commits do not
overwrite one another, and retries cannot replace an existing entry. An older
compatible fallback is safe because Vitest hashes source content per module.
Consumer restore/save failures remain non-blocking.

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
| [128116178275](https://github.com/speakeasy-api/gram/actions/runs/42282826999/job/128116178275) | 206.85s / 55.67s (Vitest durations) | Miss 19:49:17 UTC; save step 2s       |

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

The main handoff adds Node setup, API lookup, artifact download and cache-save
work, not another test run. Queue runs add artifact upload/storage (seven-day
retention). Measure those costs alongside consumer restore/save overhead; net
benefit remains unproven. The old same-ref benchmark is not cross-ref evidence.

## Post-merge verification and troubleshooting

1. In Blacksmith, inspect the repository's Branch Protected Caches setting and
   cache inventory (key, version, scope, architecture and transparent-cache flag).
   Keep protection enabled. The [documented CLI](https://docs.blacksmith.sh/blacksmith-cli/cache) supports
   the following command (not executed during this investigation):
   `blacksmith cache list --repo speakeasy-api/gram --search vitest-modules-blacksmith-v1 --per-page 100 --page 1 --format json`.
   Increment `--page` until all matching entries have been inspected.
2. Record the successful queue source run/job and artifact ID, then the main
   publisher run/job URL, exact revision, compatibility prefix, `refs/heads/main`
   scope and cache version. Check that metadata is saved.
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
   If publishing does not pay for itself, remove persistence rather than
   expanding into a new cache platform or weakening test coverage.

Cross-ref production validation and net benefit remain pending until the producer
has landed and normal queue traffic has consumed its entry.
