---
name: maintaining-command-palette
description: Use when adding, changing, or reviewing dashboard pages, routes, sidebar navigation, named resource lists or detail pages, resource names, permissions, feature gates, or Command K search candidates in client/dashboard; also for missing Cmd+K results, launcher search, and command-palette coverage reviews.
---

# Maintaining Command K coverage

Command K is a first-class discovery surface. A route, sidebar item, overview page, or Recently Visited entry does **not** automatically make individual resources searchable. Skills and registered agent identities previously had overview pages but no resource candidate source; repo review guidance did not catch either omission.

## Source map

Paths below are relative to `client/dashboard/src/`.

| Concern                               | Source                                                                                                                                    |
| ------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| Project navigation pages and gates    | `hooks/useProjectNavRoutes.ts`, consumed by sidebar and `App.tsx`                                                                         |
| Organization navigation registration  | `App.tsx` (`RouteProvider`, `routesToNavActions`)                                                                                         |
| Resource source registration and RBAC | `components/command-palette/candidates/index.ts`                                                                                          |
| Candidate shape and privacy           | `components/command-palette/candidates/types.ts` (`isSendable`)                                                                           |
| Example resource sources              | `components/command-palette/candidates/useSkillCandidates.ts`, `components/command-palette/candidates/useAgentIdentityCandidates.ts`      |
| Matching, intent prefilter, display   | `components/command-palette/ranker.ts`, `components/command-palette/useLauncherJudge.ts`, `components/command-palette/CommandPalette.tsx` |

## Implementation and review checklist

1. **Check pages and resources separately.** For each added or changed user-facing page, verify a navigation action with the right label, destination, feature gates, and permissions. For each durable named resource with a useful detail destination, verify a registered candidate source searches its display name and appropriate aliases/slug/id and opens its canonical overview. A candidate hook that is never wired into `useLauncherCandidates` is not coverage. Existing sidebar registration only proves page coverage; recents only prove that a visited URL was recorded.
2. **Update rather than duplicate.** If an existing source already covers the changed resource, check that labels, keywords, routes, query keys, and gates remain correct. Cosmetic changes or an extra overview field do not require a new candidate source. Do not demand a mutating verb merely because the UI adds a button; assess safety separately.
3. **Mirror access and tenancy.** Gate list requests and returned candidates consistently with the resource page and its API. Match org/project/resource-scoped RBAC, product flags, rollout and support/impersonation restrictions. Include the actual tenant/project in query keys. Cached results must not leak across access or tenant changes. Disable queries and pagination while the palette is closed. A failing source must not blank the palette or repeatedly retry in an effect.
4. **Cover more than page one.** For bounded inventories, drain list pagination while open, or use a server-side search that can find any authorized resource. Test a match from a later page and pagination failures. Never crawl high-volume telemetry/history on each palette opening merely to satisfy this rule.
5. **Preserve privacy.** Identity candidates use `fuzzyOnly: true` and/or the existing `person` kind. Verify they cannot enter intent-service requests through `isSendable`, including recently visited identity pages. Do not put secrets or sensitive payloads in titles, detail, or keywords.
6. **Prove it.** Test name/alias matching data, canonical navigation, later-page inclusion where applicable, closed-palette behavior, unauthorized/feature-disabled states, and project/organization scoping. Reuse existing route helpers and query caches.

Run focused checks from the repo root:

```sh
mise exec -- aube run -F dashboard test src/components/command-palette
mise exec -- aube run -F dashboard type-check
```

## Review decision and intentional exclusions

For changed discoverable surfaces, record **covered**, **add/update**, or **intentionally excluded**, with the page/resource, existing source (if any), reason, and test evidence in the implementation or PR review. No separate file is required.

Flag a concrete missing integration even if the PR never edits command-palette files. Attach the finding to the changed route/list/detail and explain the user consequence, for example: “This adds a named resource with an overview, but only the list page is registered in Command K; users cannot find an individual resource by name. Add and wire a scoped candidate source with pagination coverage.”

Accept justified exclusions for secrets, internal/admin-only operations not intended for this launcher, legacy redirects, transient dialogs, and high-volume events/logs/telemetry. A new audit-events page can need a page action without indexing millions of sensitive rows. Do not blindly expose every route or demand Command K changes for unrelated backend work, styling, or fields that do not change discovery. Do not report pre-existing unrelated coverage debt as introduced by the PR.
