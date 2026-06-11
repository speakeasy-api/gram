# Skills Management — Gap Analysis

Branch: `feat/skills-june-26` (PR #2181 squashed onto `main` @ `fdd419d21`, hooks work
intentionally dropped). Compared against the Linear **Skills Management** project
requirements (capture → promote → distribute → update).

## What the branch has today

**Server / data model**

- `skills`, `skill_versions`, `skills_capture_policies`, `skills_capture_attempts`
  tables (project-scoped registry with version lineage, capture policy model with
  org default + project override, capture attempt audit trail).
- `skills.*` management API: `get`, `list`, `getSettings`, `setSettings`, `capture`,
  `captureClaude`, `uploadManual`, `listVersions`, `listPending`, `approveVersion`,
  `supersedeVersion`, `rejectVersion`, `archive`. Capture asset blobs stored via the
  assets service (`serveSkill` in the assets API + CLI client).
- Org rollout gate: `skills_capture` product feature; capture-policy enforcement for
  `disabled` / `project_only` / `user_only` / `project_and_user`.

**Dashboard** (verified rendering locally against seeded mock data)

- Build ▸ Skills nav (feature-gated), tabs: **Registry** (card/list browse),
  **Review** (pending versions, approve/reject + version diff panel), **Settings**
  (capture policy toggles).
- Skill detail: Definition / Versions / Activity / Install tabs, active version
  panel, asset download, manual zip upload with lineage selection.
- Seed data: `mise run seed` inserts 6 skills with multi-version lineage and a
  pending-review version.

## Requirements gap matrix

### 1. Capturing skills

| Requirement                                                      | Status                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| ---------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Any skill used by any org member is uploaded to the platform     | **Gap (by decision).** The ingest API exists (`capture`, `captureClaude`, `uploadManual`) but the client-side producer that watched agent sessions and uploaded skills (hooks/shared-producer + Claude/Cursor hook plugins) was deliberately dropped from this branch. Capture currently only happens via manual upload or direct API calls. Needs a new producer path — likely the device agent / plugins model per the project description. |
| Admin sees who used a skill, when, in what session, and contents | **Partial.** Versions persist author + `first_seen_trace_id` / `first_seen_session_id` / `first_seen_at`, and contents are viewable/downloadable. But per-invocation usage (every use, not just first-seen) lived in the dropped hooks ClickHouse telemetry (skill metadata materialized columns). No usage timeline survives on this branch.                                                                                                 |
| Most popular skills across the org                               | **Gap.** No usage aggregation, no popularity metric, no org-wide (cross-project) rollup — the registry is project-scoped.                                                                                                                                                                                                                                                                                                                     |

### 2. Promoting skills

| Requirement                                                                              | Status                                                                                                                                                                                                                                                                    |
| ---------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Captured skills land in a broad "lake" of unofficial skills                              | **Partial.** Captured versions enter `pending_review` and the Review tab is a primitive lake. But there is no unofficial/official distinction on the skill itself — approving a version makes it "active", which conflates _version review_ with _promotion to official_. |
| UI oriented around promotion-worthiness (popularity, efficacy)                           | **Gap.** Registry cards show author/date/version only. No popularity or efficacy signals anywhere.                                                                                                                                                                        |
| Skill efficacy primitive (agentic transcript scoring, turn-count deltas, agent feedback) | **Gap.** Nothing exists: no efficacy score, no session-transcript analysis, no "skill feedback" MCP server.                                                                                                                                                               |

### 3. Distributing skills

| Requirement                                                    | Status                                                                                                                                               |
| -------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| Official skills assignable to RBAC roles or org-wide           | **Gap.** No assignment model. (Plugins already have `plugin_assignments` with org/role principals — likely the pattern to reuse.)                    |
| "Installed by default" vs "available to install"               | **Gap.** No install-behavior flag on skills.                                                                                                         |
| Org marketplace browsable inside Claude (grouped by team/role) | **Gap.** The pilot Claude/Cursor plugin install assets were dropped with the hooks work; even those only installed capture hooks, not a marketplace. |
| Pairs with existing plugins + device agent model               | **Gap / direction.** The skill detail Install tab is manual instructions only. Distribution through the device agent is unstarted.                   |

### 4. Updating official skills

| Requirement                                                                                                                  | Status                                                                                                                                                                           |
| ---------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Manual edits (human-in-the-loop)                                                                                             | **Partial.** New versions can be uploaded manually and approved, but there is no in-dashboard editor for skill content — "edit" means re-zip and re-upload.                      |
| Agent-suggested edits via post-invocation feedback (bundled MCP server)                                                      | **Gap.** No feedback channel exists.                                                                                                                                             |
| Scheduled analysis agent synthesizing one coherent suggestion per skill (updating, not duplicating, outstanding suggestions) | **Gap.** No suggestion entity in the schema at all.                                                                                                                              |
| Approve with one click / approve-with-edits / approve-all                                                                    | **Partial.** Per-version approve/reject exists in Review. No approve-with-edits, no approve-all.                                                                                 |
| Efficacy tracking over time to evaluate edits, presented first-class                                                         | **Gap.** Depends on the efficacy primitive (see §2).                                                                                                                             |
| Roll back to an old version (keep last X)                                                                                    | **Partial.** Full version lineage is stored and listed, and the server has `supersedeVersion`; but there is no explicit "activate this older version" rollback action in the UI. |

### 5. Customer feedback (Fermat / Sarah)

Effectiveness, quality-over-time, who/how invoked, cost & operational lift, and
time/cost-savings ROI all hinge on the missing usage telemetry + efficacy scoring.
**All gap** today.

## Branch-specific notes / leftover work

1. **Postgres migration not regenerated.** Schema changes are in
   `server/database/schema.sql` but the migration file must be produced with
   `mise db:diff skills_registry` — blocked on `atlas login` (expired token; the
   migration dir uses Pro features so logged-out atlas refuses). Run `! atlas login`
   then `mise db:diff skills_registry`. Per repo rules the migration ships in its
   own PR. The local dev DB had the skills DDL + 27 pending main migrations applied
   manually via psql (revisions recorded), so local dev works meanwhile.
2. **Hooks work dropped** (per decision): `hooks/shared-producer`,
   `hooks/plugin-claude-skills`, `hooks/plugin-cursor-skills`, hooks skill-metadata
   ingestion, ClickHouse skill materialized columns, hook smoke tests. The PR #2181
   description's hooks-observability claims no longer apply to this branch.
3. **SDK generator churn.** `client/sdk` regenerated with speakeasy 1.763.1; main
   was generated with 1.761.5 (the mise-pinned 1.761.5 install actually ships a
   1.763.1 binary). Diff includes mechanical generator-version churn.
4. **CLIs page removed.** Main's `/clis` route (titled "Skills") and its
   breadcrumb mapping were removed; the skills block replaces it.
5. **Fixed during rebase:** `SkillsRoot` bounced direct navigations while the
   product-features query loaded; strictness fixes (`noUncheckedIndexedAccess`)
   in `SkillDetail` / `SkillVersionDiffPanel`; `useFeaturesGet` → `useProductFeatures`;
   test setup updated to main's `testenv.NewTestManager` / `authz.NewEngine`
   signatures; `sql.ErrNoRows` → `pgx.ErrNoRows` in assets.
6. **Org-scoping question.** The registry, capture policies, and review queue are
   project-scoped, while the project description frames skills as an org-level
   concern ("across their org", org marketplace, RBAC roles). Expect a data-model
   decision here before distribution work starts.

## Suggested build order toward the project goals

1. Re-introduce a capture producer compatible with the device agent / plugins
   model (replaces the dropped hooks producer) feeding the existing `capture` API.
2. Usage telemetry: record per-invocation skill usage (who/when/session) and
   aggregate for popularity + the admin views.
3. Promotion model: explicit official/unofficial state on skills (the lake),
   org-level visibility, promotion UX driven by popularity/efficacy.
4. Efficacy v1: skill-feedback MCP server + scheduled analysis agent producing a
   single outstanding suggestion per skill; suggestion entity + approve /
   approve-with-edits / approve-all UX.
5. Distribution: RBAC-role / org-wide assignment (mirror `plugin_assignments`),
   installed-by-default vs available-to-install, org marketplace surfaced in
   Claude via the plugins + device agent path.
6. Efficacy-over-time charting per skill version + one-click rollback.
