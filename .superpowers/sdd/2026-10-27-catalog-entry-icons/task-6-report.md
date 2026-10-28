# Task 6 automated milestone

Base: `f34b78b035`. Scope: local fixture and carried regression coverage only. No shared local services, seed commands, or browsers were started/restarted by this agent.

## Changes

- Embedded a tiny fictional, static triangle SVG in the local-only fixture. Uses `assets.NewPlatformService` / `StorePlatformImage`, canonical `ServeImageURL`, and the registry validator/Create flow. Both default and dark reference the same safe asset.
- Existing `local.go` creates a global **registry source**, not a catalog entry. Added the stable fictional entry identity `example.test/local-catalog-logo`; existing entry is preserved on reseed rather than overwriting staff edits. No production tenant seed phase changed.
- Added optional `LocalFixturesOptions.SiteURL`; nil preserves existing callers. **Controller integration required:** wire the canonical configured site URL in `server/cmd/gram/demoseed.go` when calling RunLocalFixtures. Command files were outside this agent's assignment. Until wired, normal local seeding skips this new logo fixture.
- Extended local reseed test: stable entry ID and icon association across two tenant reseeds; configured public origin; one matching entry; repeat platform upload returns the same asset ID (hash dedup), using local test blob storage.
- Added persisted registry lifecycle coverage: independent primary/dark replacement, primary removal leaving dark, explicit empty namespace `{}` cleanup; Get, raw discovery list, and raw discovery detail preserve both slots, unrelated large integer metadata and upstream icons. Namespace never leaks inside server data.
- Fixed UI late-completion test to return a valid public URL matching the uploaded asset ID; URL validation no longer masks missing unmount protection. Mounts a fresh editor after close and checks late completion neither calls the old change handler nor contaminates the reopened draft. No separate full-sheet entry-switch case added.

## Actual automated evidence

All backend invocations used command-scoped `DOCKER_HOST=$(docker context inspect --format '{{.Endpoints.docker.Host}}')`.

- `mise run test:server -tags=demoseed_safety ./internal/demoseed -run TestLocalFixtures -count=1`: **PASS**, 2 tests, 10.685s reported test time. Existing local tests have this build tag; the untagged command from the brief would not execute them. Initial combined edit/test call timed out at an accidentally short 20s; one retry used a 180s timeout and passed.
- `mise run test:server ./internal/assets ./internal/mcpregistry/... ./internal/externalmcp ./internal/adminmcp`: **PASS**, 1,407 tests, 21.429s reported. Full focused backend suite run once, 180s timeout.
- `aube run -F admin test src/pages/registry`: **PASS**, 116 tests / 11 files, 8.03s reported.
- `aube run -F admin type-check`: **PASS**, `tsc -b --noEmit`, exit 0.
- `git diff --check`: **PASS**.
- `mise run lint:brand`: **PASS**.
- Go formatting and targeted `aube exec oxfmt -- client/admin/src/pages/registry/RegistryIcons.test.tsx` applied.

Logs retained locally in `/tmp/catalog-task6-{fixture-retry,backend,ui,types,brand}.log`. No full production demo safety suite was needed: shared phases are untouched; tagged local fixture tests were run explicitly.

## Exclusions / remaining controller evidence

Catalog assets are global staff data, not tenant-owned demo content. No customer-dashboard rendering changes are in this slice; no production demo seed mutation is appropriate. No new page/navigation/tool/API behavior is introduced by these fixtures/tests.

Live browser interactions, service startup/reseed, actual HTTP response headers, direct SVG navigation/security behavior, transformed asset bytes/hash/length checks, and real UI screenshot are **not verified by this agent** and remain controller-owned. Final security/correctness review remains controller-owned (no reviewer/subagent was started). This is an automated milestone, not an end-to-end completion claim.
