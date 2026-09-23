# Intent-ranked command palette (Jev launcher) — design

**Date:** 2026-09-19
**Status:** Approved; amended 2026-09-22 to route Jev through OpenRouter
**Reference:** [dabit3/jev-experiments/jev-launcher](https://github.com/dabit3/jev-experiments/tree/main/jev-launcher)

## Summary

The dashboard's ⌘K palette today lists pages, resources, recents and people
and lets cmdk's substring filter order them. This change adds an
intent layer modelled on the Jev launcher: on every keystroke the client
sends the typed text and a short list of fuzzy-prefiltered candidates to
a new `launcher.judge` management method, which forwards typed questions
to the TypeSafe Jev API via OpenRouter and returns probability distributions. The client
re-ranks the list from those distributions, shows a green ↵ on the top row
when the intent is settled, and — new for Gram — lets Jev choose a verb
per row. Verbs in this version are `open`, `enable`/`disable` an MCP
server, and `publish` the project's plugin marketplace. Mutating verbs
always require a second Enter inside the palette.

Everything expensive or risky stays in code: indexing (the existing SDK
list hooks), fuzzy prefiltering, ranking maths, and execution. Jev never
generates text; it only picks among options the code supplies.

## Goals

- Queries like `the disabled slack mcp`, `plugins`, `people in sales`,
  `publish marketplace`, `turn off the jira server` land on the right row.
- Fuzzy order renders immediately; Jev's answer replaces it when it lands.
  Nothing waits on the network.
- A wrong Enter on a mutating verb is impossible without a second Enter.
- With no OpenRouter key resolvable for the org, the palette behaves exactly as today.

## Non-goals (this version)

- "All of them" sets and group rows (the launcher's `scope`/`match_cN`
  questions). Dropped; can be added later without changing the ranking.
- Verbs beyond the three named. `pause`/`resume` trigger, approve access
  request, and per-plugin publish are candidates for a follow-up.
- Billing or per-org cost accounting for Jev calls. Usage is logged only.
- Sending People (org members) to Jev. See _Data leaving the tenant_.
- A feature flag. The feature is on wherever an OpenRouter key resolves.

## Architecture

```
keystroke ─▶ candidates (hooks) ─▶ fuzzy prefilter ─▶ top 13 ─▶ launcher.judge ─▶ TypeSafe
                     │                    │                              │
                     ▼                    ▼                              ▼
              flat typed list     immediate fuzzy order        target / action / ready
                                          │                              │
                                          └────────── ranker ◀───────────┘
                                                        │
                                                 ordered rows + verb + ready ↵
```

Four units, each testable alone:

| Unit            | Location                                                              | Depends on                                            |
| --------------- | --------------------------------------------------------------------- | ----------------------------------------------------- |
| Candidate hooks | `client/dashboard/src/components/command-palette/candidates/*.ts`     | existing `@gram/client` list hooks, `useRBAC`, routes |
| Ranker          | `client/dashboard/src/components/command-palette/ranker.ts` (pure)    | nothing                                               |
| Judge client    | `client/dashboard/src/components/command-palette/useLauncherJudge.ts` | generated `useLauncherJudgeMutation`                  |
| Judge service   | `server/internal/launcher/` + `server/internal/thirdparty/typesafe/`  | OpenRouter provisioner, o11y                          |

## Client

### Candidate model

```ts
export type Verb = "open" | "enable" | "disable" | "publish";

export type CandidateKind =
  | "page"
  | "recent"
  | "mcp_server"
  | "catalog"
  | "plugin"
  | "marketplace"
  | "assistant"
  | "environment"
  | "source"
  | "deployment"
  | "policy"
  | "rule"
  | "access_request"
  | "person";

export interface LauncherCandidate {
  id: string; // stable: "page:/settings", "mcp:<id>", "marketplace"
  kind: CandidateKind;
  title: string; // what Jev sees
  detail: string; // state Jev can read: "MCP server · disabled · 12 tools"
  keywords: string[]; // slug, id, aliases — fuzzy only, never sent
  verbs: Verb[]; // "open" always first; others by state + RBAC
  icon?: IconName;
  group: string; // display heading when no judgment ("Pages", "MCP Servers", …)
  run: (verb: Verb) => void | Promise<void>;
}
```

`detail` plays the role of the launcher's "modified 16 min ago": it carries
the state Jev needs to disambiguate. Every kind renders a short, stable
detail string. Examples:

| kind          | detail                                                                          |
| ------------- | ------------------------------------------------------------------------------- |
| `mcp_server`  | `MCP server · enabled` / `MCP server · disabled`                                |
| `marketplace` | `Plugin marketplace · unpublished changes` / `· up to date` / `· not connected` |
| `plugin`      | `Plugin · 4 servers`                                                            |
| `catalog`     | `Catalog entry · <registry specifier>` (open-only; requires `project:read`)     |
| `page`        | `Page` (org pages: `Organization page`)                                         |
| `recent`      | `Recently visited`                                                              |
| `person`      | `Member · <role>` (fuzzy only, see below)                                       |

### Verb attachment rules

| Verb      | Attached to                        | When                                                                              |
| --------- | ---------------------------------- | --------------------------------------------------------------------------------- |
| `open`    | every candidate                    | always                                                                            |
| `disable` | `mcp_server`                       | `visibility !== "disabled"` and `hasScope("mcp:write")`                           |
| `enable`  | `mcp_server`                       | `visibility === "disabled"` and `hasScope("mcp:write")`                           |
| `publish` | the single `marketplace` candidate | `publishStatus.configured && publishStatus.connected` and `hasScope("org:admin")` |

Jev only ever sees verbs the user can run. The `marketplace` candidate is
synthetic and project-level; it exists only inside a project and only when
the plugins publish status resolves.

### Candidate hooks

`ResourceResults.tsx` currently renders `<CommandItem>`s from nine
independent Suspense components. Each becomes a hook returning
`LauncherCandidate[]` (or `[]` while loading / on error) using the
non-suspense form of the same SDK hook so one failing endpoint cannot
blank the palette. The registered `CommandAction`s from the context
registry and the localStorage recents are adapted through the same
interface. One `useLauncherCandidates()` hook concatenates them.

Fault isolation moves from error boundaries to per-hook `isError`, which
is simpler and keeps hooks unconditional.

### Fuzzy prefilter

Port of `Fuzzy.swift`, in `ranker.ts`:

- Tokenise query and candidate title+keywords; strip filler words. Start
  from the launcher's stop-word list and add dashboard filler that should
  not crowd out identifiers: `server`, `page`, `turn`, `set`, `switch`,
  `go`, `to`. `mcp` is **not** a stop word: it distinguishes MCP rows from
  plugins. The final list is fixed by the ranker tests.
- Per-token best score: exact `1.0`; term prefix `0.8 + 0.15·len/termlen`;
  title initials prefix `0.7` (token ≥ 2); contains `0.55`; subsequence
  `0.2 + 0.2·contiguity` (token ≥ 3). Unmatched tokens halve the score. A
  small length bonus (`0.02·(1 − len/40)`) prefers short titles.
- Keep candidates with fuzzy ≥ `0.15`, sorted by fuzzy desc, title asc.
  Top **13** are sent to Jev; hard cap 32 enforced server-side too.
- Empty query: no prefilter, no Jev call. Render recents and pages as today.

### Ranking

Given a judgment `J = {target: Record<cid, p>, action: Record<verb|"unclear", p>, ready: p}`:

```
score(c) = 0.65 · J.target[c] + 0.20 · Σ_{v ∈ c.verbs} J.action[v] + 0.15 · fuzzy(c)
```

Without a judgment, `score = fuzzy`. Ties break on title ascending.
Candidates that were not sent to Jev (below the top 13, or People) keep
`score = 0.15 · fuzzy` so they sort beneath judged rows but remain
reachable.

**Verb resolution per row:** `verb(c) = argmax_{v ∈ c.verbs} J.action[v]`,
except when `J.action.unclear` is the overall argmax, in which case
`verb = "open"`. The row label shows the verb whenever it is not `open`:
`Disable · Slack MCP`, `Publish · Plugin marketplace`.

**Readiness:** the top row shows the green ↵ when
`J.ready ≥ 0.6` **or** `J.target[top] ≥ 0.9`. The second rule exists
because `ready` hedges on paraphrases where `target` is certain. The
affordance hides as soon as ↑/↓ moves the selection, and is purely
visual: Enter always runs the highlighted row.

### In-flight handling

- Each keystroke increments `sequence`, aborts the previous request via
  `AbortController`, and dispatches a new one. No debounce.
- A response is applied only if its sequence is greater than
  `newestApplied`. Older answers are counted as stale and dropped.
- While a newer request is in flight the previous judgment is kept and the
  list is rendered from it, dimmed (`judgmentIsFresh = false`), so the
  order does not flicker back to fuzzy between keystrokes.
- On error, timeout, or `disabled: true` the judgment is cleared and the
  list stays in fuzzy order. `disabled: true` also sets a session flag so
  no further calls are made until reload.

### cmdk integration

`CommandDialog` is rendered with `shouldFilter={false}`. The palette
computes order; cmdk only renders and manages keyboard focus. Because cmdk
auto-selects the first item in DOM order, the ranked list is rendered as
one flat `CommandGroup` per display bucket in score order, with the
forceMounted "Ask Project Assistant" row last (preserving AGE-2807). The
empty state is rendered manually (cmdk's `CommandEmpty` depends on its own
filter).

### Mutation confirm flow

State machine in the palette component:

```
list ──Enter on verb≠open──▶ confirm(candidate, verb) ──Enter──▶ running ──▶ closed
  ▲                               │ Esc                            │ error
  └───────────────────────────────┘                                └──▶ list (toast)
```

- **confirm:** the input is replaced by a bar: `Disable Slack MCP?  ↵ confirm · esc back`.
  The list collapses to the single row. Query text is preserved.
- **running:** the row shows a spinner; Enter/Esc are ignored.
- **Execution** is the candidate's `run(verb)`:
  - `enable`/`disable`: `useUpdateMcpServerMutation` with the existing
    `mcpServerVisibilityUpdateForm(server, "private" | "disabled")` shape
    from `DangerZoneSection.tsx`, then `invalidateAllMcpServers`,
    `invalidateAllGetMcpServer`, `invalidateAllMcpEndpoints`. Toast copies
    `mcpServerVisibilityToast`. These helpers are lifted into a shared
    module so the settings page and the palette share one implementation.
  - `publish`: `usePublishPluginsMutation` with `githubUsernames: []`
    (the server accepts an empty collaborator list), then
    `invalidateAllPublishStatus`. Toast matches `Plugins.tsx`.
- `ready` never bypasses confirm. Publish is one-way; the unconditional
  second Enter is the price of offering it at all.

### Footer

The existing hint footer gains, at the right edge, the last round-trip
latency in ms when a judgment is fresh. Nothing else. No cost display.

## Server

### Design

`server/design/launcher/design.go`, service `launcher`:

```go
Method("judge", func() {
    Security(security.Session, security.ProjectSlug)
    Payload(func() {
        security.SessionPayload()
        security.ProjectPayload()
        Attribute("query", String, MaxLength(200))
        Attribute("context", LauncherContext)          // {route: String}
        Attribute("candidates", ArrayOf(LauncherCandidate), MaxLength(32))
        Required("query", "candidates")
    })
    Result(LauncherJudgment)
    HTTP(func() { POST("/rpc/launcher.judge"); security.SessionHeader(); security.ProjectHeader() })
})
```

`LauncherCandidate` = `{id, kind, title (≤120), detail (≤160), verbs: []String}`.
`LauncherJudgment` = `{disabled: Bool, target: Map[String]Float, action: Map[String]Float, ready: Float, latency_ms: Int}`.

Session + project auth means the generated SDK hook works with the
dashboard's cookie session and injected `gram-project` header; no
hand-assembled headers.

### Implementation

`server/internal/launcher/`:

- `impl.go`: validates counts and lengths, returns `{disabled: true}`
  immediately when no key is configured, otherwise builds the request,
  calls the TypeSafe client, maps the answer back. Question ids are `c0…cN`
  by index, mapped to the caller's ids in the response.
- `questions.go`: builds the Jev `state` and `questions`. Wordings are
  adapted verbatim from the launcher's iterated prompts with Gram nouns:

  - `query_note`: "Text the user has typed so far into a ⌘K command palette
    in an admin dashboard. It is often an incomplete prefix or a short
    natural-language phrase."
  - `target` (Choice over `c0…cN` + `none`): the launcher's wording, with
    the example changed to "`the disabled slack one` means the MCP server
    whose `detail` says disabled".
  - `action` (Choice over `open`, `enable`, `disable`, `publish`,
    `unclear`), rubrics:
    - `open` — Navigate to the item's page in the dashboard.
    - `enable` — Turn an MCP server back on so people can connect to it.
    - `disable` — Turn an MCP server off so nobody can connect to it.
    - `publish` — Publish the project's plugin marketplace to GitHub so
      pending plugin changes go live.
    - `unclear` — Too little typed or too ambiguous to tell what kind of
      action is meant. Prefer this over guessing a mutating verb.
      Each candidate's `verbs` is included in its `criteria` text so Jev
      knows which verbs apply to which row.
  - `ready` (Noul): the launcher's wording with "web_search" replaced by
    the "Ask Project Assistant" fallback.

- Candidate `criteria` string: `<Kind label>: "<title>" — "<detail>" (verbs: open, disable)`.
  Title and detail are quoted (`strconv.Quote`) because they are
  workspace-controlled text; the `target` and `action` instructions tell the
  judge they are data, never instructions.

`server/internal/thirdparty/typesafe/`:

- `client.go`: `POST https://openrouter.ai/api/v1/systemone` (OpenRouter hosts
  Jev's System One API unchanged, in beta), `Authorization: Bearer <OpenRouter key>`,
  body `{model: "typesafe/jev-latest", state, questions}`, 4 s timeout, no
  retry. The key is passed per call, never held by the client. Returns typed
  errors for transport, non-200, and decode failures.
- Response shape: `answers[<id>].probabilities[<option>]` for Choice,
  `answers[<id>].noul` for Noul, `usage.{input_tokens,output_tokens}`.
  OpenRouter adds `id`, `provider` and `usage.cost`; they are tolerated and
  `usage.cost` is recorded when present.
- Records span attributes: latency, input/output tokens, candidate count,
  org and project ids. No query text in telemetry.

### Configuration

No new configuration. The launcher service takes the existing OpenRouter
provisioner (the `openRouter` value built in `server/cmd/gram/start.go`) and
reads the org's existing key per request with `LookupAPIKey(ctx, orgID,
openrouter.KeyTypeInternal)`, the same slot the other internal judges use.
It never provisions a key: typing into the palette must not mint one for an
organization that has none. Locally that provisioner is the development one,
which returns `OPENROUTER_DEV_KEY` from `mise.local.toml`. A missing, empty or
`unset` key, or a lookup error, makes `judge` return `{disabled: true}` with
no outbound call. `model` is the constant `typesafe/jev-latest`.

### Wiring

Register the service in `server/cmd/gram/start.go` next to the other
management services, passing the `openRouter` provisioner and a client built
on `guardianPolicy.PooledClient()`. Regenerate: `mise gen:goa-server` then `mise gen:sdk`.
No SQL, no migration, no Temporal.

### Data leaving the tenant

Titles and details of candidates are sent to TypeSafe. This version sends
pages, recents, MCP servers, catalog entries, plugins, the marketplace, assistants,
environments, sources, deployments, policies, rules and access requests.
**People are never sent**: `person` candidates, and any candidate marked
`fuzzyOnly` (a recent whose href is an identity page, whose label is the
person's name or email), are excluded from the prefilter's outgoing slice
and rank by fuzzy alone. `keywords` (slugs,
ids) are never sent. The query text is sent and is not logged server-side.

## Error handling

| Failure                       | Client behaviour                                       |
| ----------------------------- | ------------------------------------------------------ |
| Key unset (`disabled: true`)  | Fuzzy order, no ↵, stop calling for the session        |
| Timeout / 5xx / transport     | Fuzzy order for that keystroke; next keystroke retries |
| Stale answer (older sequence) | Dropped, counted                                       |
| Mutation error                | Toast, return to `list` state with query intact        |
| RBAC lacking                  | Verb never attached; row is `open` only                |

Server: upstream errors map to a 502-class `oops` error; validation to 400.
The client treats any error identically (fallback), so the codes exist for
observability, not behaviour.

## Testing

**Server (`server/internal/launcher`, `thirdparty/typesafe`)**

- Golden JSON test for the built Jev request from a fixed candidate list.
- Response mapping: probabilities by index → caller ids; missing `action`
  ⇒ `unclear: 1`; missing `ready` ⇒ 0.
- Disabled-key path returns `{disabled: true}` without any outbound call.
- `httptest` fake TypeSafe server: 200, 401, 500, slow (timeout), malformed.
- Validation: 33 candidates rejected; overlong title rejected.

**Client (vitest, `aube run -F dashboard test`)**

- `ranker.test.ts`: fuzzy tiers ported from `RankingTests.swift` cases
  (`dark`→toggle, `wifi off`≠`wifi on` analogue with `enable`/`disable`),
  filler stripping, score formula, verb resolution incl. `unclear`,
  readiness both rules, People excluded from outgoing slice, cap 13.
- `useLauncherJudge.test.ts`: sequence guard drops older responses; abort
  on new keystroke; `disabled` latches; error clears judgment.
- `CommandPalette.test.tsx`: confirm bar appears for `disable`, Esc returns
  to list with query intact, second Enter calls `run`; `open` rows
  navigate on first Enter; green ↵ present/absent by readiness.
- Existing `ResourceResults.test.tsx` cases migrate to the candidate hooks.

**Manual**

- `mise run playwright` against the local stack with `OPENROUTER_DEV_KEY`
  set: `the disabled slack mcp`, `turn off jira`, `publish marketplace`,
  `people in sales` (fuzzy only), `sett` (ready on Settings page).

## Rollout

Server (`launcher` service + `typesafe` client), SDK regen and client
(candidate hooks refactor + ranker + judge hook + confirm flow) ship in one
PR. Nothing to configure: orgs with an existing OpenRouter key get it live;
everyone else keeps today's fuzzy ordering via `disabled: true`.

## Open questions

- Should `assistant` candidates get `pause`/`resume` trigger verbs in a
  follow-up? Out of scope here.
