# Vitest module cache in CI

The `client-test` job restores the workspace-root `node_modules/.vitest-cache`
after `aube install` and saves the entire directory, including `_metadata.json`,
after all tests succeed. This is Vitest's transformed-module cache, not test
results: dashboard, admin, and browser tests still execute normally. Cache
service failures are non-fatal. It uses the existing `actions/cache` mechanism
backed by Blacksmith, with a separate Blacksmith namespace.

The key includes runner OS/architecture, the actual Node version, an explicit
`pnpm-lock.yaml` hash (rather than relying on package-manager detection with
aube), workspace/package manifests, TypeScript and Vite/Vitest configuration,
and the dashboard's local test plugin. The revision suffix allows each revision
to save updated modules; fallback restoration stays inside the same configuration
and dependency prefix. Vitest validates individual source modules itself.

To invalidate all CI entries, bump `vitest-modules-blacksmith-v1` in
`.github/workflows/pr.yaml`. Dependency/configuration changes invalidate the
prefix automatically. Add new transform-affecting configuration inputs to the
hash when introducing them. Locally, `vitest --clearCache` clears the cache.

## Validation gate (GRW-391)

Do not retain this optimization without a useful net CI benefit. The separate
admin fix on main removed the Tailwind stylesheet-parsing test and its test
plugin. Do not reintroduce transforms that depend on other source files without
validating their filesystem-cache invalidation.

On one revision, use the existing
`blacksmith-4vcpu-ubuntu-2404` client-test job for exactly two cold and two restored
runs. Keep dependencies, test commands, and runner class identical. For each cold
run, ensure no matching module-cache entry (including fallback entries) is
available; after each cold run, rerun against its saved entry. Do not delete
unrelated shared caches. Use an isolated cache namespace for the experiment.

Enable `DEBUG=vitest:cache:fs` consistently for all four runs to check actual
module reuse: a cache-action hit alone is insufficient. Restored runs must show
`[read]` entries for transformed source modules, not just an archive download.
Check that `_metadata.json` survives restoration and no integrity reset occurs.
Record GitHub job/attempt links, test counts, and step durations in seconds:

| Run        | Dashboard | Admin   | Restore | Save    | Actual module reads | Job link |
| ---------- | --------- | ------- | ------- | ------- | ------------------- | -------- |
| Cold 1     | pending   | pending | pending | pending | pending             | pending  |
| Restored 1 | pending   | pending | pending | pending | pending             | pending  |
| Cold 2     | pending   | pending | pending | pending | pending             | pending  |
| Restored 2 | pending   | pending | pending | pending | pending             | pending  |

Compare test-step savings against restore/save overhead (including cold-run
population cost), not Vitest's transform timing alone. Report inconclusive or
slower results and remove the cache steps rather than extending the experiment.
These measurements are pending; local correctness checks are not CI timings.
