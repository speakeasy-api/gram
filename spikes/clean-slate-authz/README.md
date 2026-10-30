# Spike: three RBAC options, compared on the same organization

This spike implements three authorization designs against one example
organization, and the tests require them to agree:

- **Option A: clean-slate relationships.** A Postgres-native,
  Zanzibar-style engine, from
  [`docs/rfcs/clean-slate-authz-design-review.md`](../../docs/rfcs/clean-slate-authz-design-review.md).
  Packages `schema/`, `model/`, `authz/`, `pgstore/`, `memstore/`.
- **Option B: Extensible RBAC.** The approved plan "Technical implementation
  plan: Extensible RBAC for Speakeasy": a catalogue, a `Client`/`Backend`
  pair and a shared evaluator over today's grant shape. Package `xrbac/`.
- **Option C: OpenFGA.** OpenFGA v1.22.0, embedded. Package `fga/`.
- **Option D: relationship schema over today's grants.** Option A's schema
  and engine reading Option B's tables, through a reversible mapping.
  Packages `hmodel/` and `hybrid/`.

All four pass the same 20 scenarios, both in memory and on Postgres.
[`CLAUDE_DESIGN_BRIEF.md`](CLAUDE_DESIGN_BRIEF.md) holds the side-by-side
comparison: code, changes involved and benchmarks, written as instructions
for Claude Design.

It is a standalone Go module, so it does not touch the server, its `go.mod`,
or its database.

## Run it

```sh
cd spikes/clean-slate-authz
go test ./...                        # our engine (in-memory store) + OpenFGA (memory)

# With Postgres: our pgstore and OpenFGA's postgres datastore, in separate databases.
export SPIKE_DATABASE_URL='postgres://<USER>:<PASS>@127.0.0.1:5439/authz_spike?sslmode=disable'
export SPIKE_FGA_DATABASE_URL='postgres://<USER>:<PASS>@127.0.0.1:5439/authz_spike_fga?sslmode=disable'
go test -v ./...
go test -run '^$' -bench . -benchmem .
```

`pgstore.Reset` drops and recreates its own tables, which all carry the
`authz_*` or `spike_*` prefix. OpenFGA runs its own migrations into the second
database.

## Layout

| Path                             | What it is                                                                                                                  | Lines |
| -------------------------------- | --------------------------------------------------------------------------------------------------------------------------- | ----- |
| `schema/`                        | Declaration DSL and compiler: types, anchored grants, blocks, attributes, markers, permissions, three-valued conditions     | 335   |
| `model/`                         | The Speakeasy schema, written once, plus the `mcp_tools` condition                                                          | 171   |
| `authz/`                         | The engine: admission, layers, snapshots, `OrgFacts`, evaluation, `Lookup` → SQL, `CanGrant`, `Guard`, revision-keyed cache | 1,179 |
| `pgstore/`                       | Relationship table, statement-level revision trigger, recursive subject loader                                              | 273   |
| `memstore/`                      | The same semantics in memory                                                                                                | 126   |
| `objects/`, `perm/`              | What `gen:authz` would generate: typed objects and typed permissions                                                        | 108   |
| `fga/`                           | The equivalent OpenFGA model and an embedded-server backend that translates our rows into tuples                            | 349   |
| `example/`                       | Handler hookup: middleware, resource check, tool call, list pushdown, self-service agent creation, stream guard             | —     |
| `spike_test.go`, `bench_test.go` | Shared scenarios and benchmarks                                                                                             | —     |

## What the tests show

All of the following pass on every backend: our engine on the in-memory store
and on Postgres, and OpenFGA in memory and on Postgres.

- **`TestScenarios` (20 cases)**
  - Management is separate from use: Dave can edit the server, but not
    connect to it.
  - The three-layer agent check: the API key's ceiling, the agent's tool
    policy, and Alice's current permissions.
  - A conditional block: Engineering may not call destructive tools.
  - Team review: Alice can read Bob's chat. She cannot read Carol's, because
    Carol is protected, or Dave's, because he is not on her team.
  - Personal history and agent-owner history.
  - Temporary access: Erin can read Carol's chat, and is denied 31 minutes
    later without anything being deleted.
  - Self-service agent creation.
- **`TestRevocation`**
  - **Our engine:** deleting row 5 (Alice's Engineering membership) bumps
    the revision from 19 to 20 through the trigger. The very next request
    denies the agent at the **owner** layer. Before the delete, the second
    request made zero snapshot loads, because every snapshot came from the
    cache.
  - **OpenFGA:** we had to delete _two_ tuples, the membership and the
    reverse pointer the model needs for `tupleToUserset`. With OpenFGA's
    check caches on (10s TTL), the agent **is still allowed immediately after
    the revocation**. That is the documented trade-off of those caches.
- **`TestLookupAgreesWithCheck`.** For each person, `Lookup` returns a filter
  that matches exactly the chats `Check` allows. Rendered to SQL and run
  against `spike_chats`, it returns the same rows, which also equal
  OpenFGA's `ListObjects`. The predicate for Alice:

  ```sql
  WHERE ((c.user_id = ANY($1))                                   -- her own chats
     OR (a.owner_user_id = ANY($2))                              -- her agents' chats
     OR (c.user_id = ANY($3) AND NOT (c.user_id = ANY($4)))      -- team, minus protected
     OR (a.owner_user_id = ANY($5) AND NOT (a.owner_user_id = ANY($6))))
  -- args [alice] [alice] [alice bob] [carol] [alice bob] [carol]
  ```

- **`TestDelegation`**
  - Alice can give her agent three tools on `srv_linear`.
  - She cannot delegate connect access project-wide, because she doesn't
    hold it.
  - Dave cannot delegate connect access, because management doesn't imply
    use.
  - Nobody can delegate `editor`, because it isn't `Delegable`.
- **`TestMissingFactIsUndetermined`.** A tool-limited grant evaluated without
  a tool name is `Undetermined`, which denies. `Eligible` answers "can this
  agent connect at all?" explicitly.
- **`TestDecisionExplains`.** A denial names its layer: "denied at subject
  layer". An allow carries its path:
  `group:grp_eng#user.activity_reviewer@user:alice`.
- **`TestTranslationCost`.** Our 21 rows became 21 mirrored tuples plus **17
  derived tuples** in OpenFGA. Those are facts our engine reads from domain
  rows: parents, owner, author, org membership pointers and reverse group
  pointers. A grant that is both temporary and tool-limited **cannot be
  expressed as a single OpenFGA tuple**, because a tuple carries one
  condition. It needs a composite condition for every combination.

## Benchmarks

These ran on an Apple M4 Pro against local Docker Postgres 18, with
`-benchtime=2s`. They measure one process and a tiny dataset, so treat them as
orders of magnitude, not capacity numbers.

| Benchmark                                         | Ours        | OpenFGA memory | OpenFGA postgres | OpenFGA postgres + 10s cache |
| ------------------------------------------------- | ----------- | -------------- | ---------------- | ---------------------------- |
| One agent tool call (3 layers, condition + block) | **1.75 µs** | 438 µs         | 2.52 ms          | 15.2 µs                      |
| Team-review check on one chat                     | **0.44 µs** | 195 µs         | 867 µs           | 4.7 µs                       |
| `tools/list`, 40 tools, 3 layers                  | **41 µs**   | 9.3 ms         | 88 ms            | 573 µs                       |

How to read these numbers:

- **Our checks.** These assume an already admitted principal. They are pure
  in-memory map walks; most of the 40 allocations per check are slice
  building that could be removed.
- **OpenFGA defaults.** OpenFGA runs with its default embedded-server
  settings, and every call pays request validation and graph resolution. Our
  engine has no counterpart to that overhead, so the per-check gap is real but
  partly structural. OpenFGA's caches close most of it, at the cost of stale
  answers shown in `TestRevocation`.

Admission (our engine only) is the per-request cost before any check:

| Admission           | Uncached | Cached (revision hit) |
| ------------------- | -------- | --------------------- |
| Human, 1 layer      | 663 µs   | 362 µs                |
| Agent key, 3 layers | 1.34 ms  | 609 µs                |

Cached admission is dominated by round trips to Postgres in Docker on macOS:
`BEGIN`, the revision read, the key and agent rows, and `ROLLBACK`.
Pipelining those reads into a single query is the obvious next step and was
not attempted. Today's `PrepareContext` makes 3–4 sequential queries per
request, but it was not measured here, so there is no direct before/after
comparison.

## What we learned

### In favor of our engine

1. **Lists are much simpler.** `Lookup` produced a predicate that joins into
   our own query. OpenFGA's `ListObjects` returned the right IDs, but only
   because every chat had author and agent tuples written into OpenFGA. With
   millions of chats that is a second copy of the activity data, or a capped,
   unsorted ID list fed back into SQL.
2. **Revocation is easier to reason about.** One transaction plus a trigger
   gave immediate revocation. OpenFGA needed two-tuple dual writes, which in
   production would sit outside the domain transaction. Its fast mode
   revokes only once the TTL expires.
3. **Facts stay in the rows that own them.** Ownership and authorship come
   from the loaded row, which removed 17 derived tuples even in this tiny
   example.
4. **Conditions compose.** A row's expiry and its condition are independent
   columns. OpenFGA allows one condition per tuple.
5. **The engine is small.** It is about 1,200 lines including lookup,
   delegation and cache, plus a 335-line DSL and compiler. This is spike
   quality; production would need fuzzing, an LRU with singleflight, metrics
   and explain formatting.

### In favor of OpenFGA

1. **Its evaluator is proven.** Ours is new code. Even this spike needed a
   design correction to how delegation treats conditional blocks. OpenFGA's
   graph semantics are mature.
2. **The model is declarative and portable.** `model.fga` is readable,
   tooling exists (playground, `fga model test`), and versioned models come
   for free.
3. **It isn't tied to a shallow hierarchy.** Our snapshots assume a bounded
   subject closure and a fixed, shallow resource hierarchy. Nested folders or
   user-defined hierarchies would favor OpenFGA.

### As a differential oracle

The two backends agreed on every scenario, every list and revocation. Keeping
`model.fga` alongside `model/` and running the scenario suite against both in
CI is cheap. It is the safety net the design proposes for our evaluator.

## Known spike limitations

- **No real code generation.** `objects/` and `perm/` are hand-written
  stand-ins for `gen:authz`.
- **Delegation and conditional blocks.** `CanGrant` rejects unconditional
  blocks that overlap a proposal. It leaves conditional blocks to runtime,
  where the owner layer enforces them. A conditional block held only by a
  non-owner who configures the agent is therefore not carried. The design
  needs to settle this.
- **Simplified `Lookup`.** It ignores conditional blocks: a block that
  applies to some tools never hides the whole object. It handles the fixed
  hierarchy and membership expansion, not arbitrary nesting.
- **Crude cache eviction.** The cache clears itself when full, and there is
  no singleflight. Neither affects correctness, because snapshots are keyed by
  revision and schema hash.
- **Only the agent-key credential.** Workload sessions, derived credentials
  and the authorizer layer use the same layer mechanism and are not
  exercised here.
- **The same model in OpenFGA is more verbose.** Every anchor needs explicit
  relations on the parent types, and every condition needs
  `type with condition` entries in each type restriction list. That
  verbosity is part of the comparison, not something we fixed.

## Option B (`xrbac/`) findings

These are from `xrbac_test.go` and `xrbac_bench_test.go`.

- **Same decisions, same lists.** All 20 shared scenarios pass with both the
  memory and the Postgres source. `Filter` over the five chats returns the
  same set as Option A for every person.
- **A denial always wins.** It beats direct grants, roles and relationship
  rules (ownership, self-access). Frank's reviewer-side `chat:blocked_read`
  hides Carol even though he can review everyone else.
- **Containment is strict.** Alice can delegate three tools only if the
  proposal also carries her `destructive` denial. Without it, the proposal is
  rejected.
- **`DiscoverAccess` handles disjoint limits.** A credential that allows only
  `delete_issue`, combined with an agent that allows only three other tools,
  is refused.
- **An unknown tool classification is `Unresolved`.** It cannot prove that
  the destructive block doesn't apply.
- **Revocation needs one write.** Deleting a directory membership row is
  enough: there is no cache and no projection, and the next call is denied at
  the owner limit.
- **Cost profile.** Each client call loads authority, about 1 ms on local
  Postgres. Five separate `Require` calls cost 5.6 ms; one `AuthorizeAll`
  costs 1.0 ms. See the brief for every number.
- **Lists are deferred.** `PlanQuery` is deferred in the plan, so lists use a
  bounded `Filter` over rows already loaded. Pagination and counts are not
  authorization-aware.

## Option D (`hmodel/`, `hybrid/`) findings

These are from `hybrid_test.go` and `hybrid_bench_test.go`.

- **Same decisions, same lists, on B's tables.** All 20 scenarios pass in
  memory and on Postgres. The engine reads the same `x_` tables Option B uses.
  `Lookup` renders a SQL predicate that returns the same chats as `Check` and
  as Option A.
- **The mapping round-trips exactly.** Every example row decodes to a
  relationship and encodes back to the identical row: principal, scope,
  selectors and validity. Unsupported shapes fail the load with a specific
  error: unknown selector keys, constraints a grant doesn't take, anchors a
  grant doesn't allow, and unmapped scopes.
- **Two new row conventions.** A `user:protected` marker row replaces
  Frank's reviewer-side block. A `chat:read_protected` scope replaces Erin's
  `chat:read`. Both are ordinary `principal_grants` rows.
- **It found a real bug.** Running A's predicate on these rows exposed a SQL
  NULL issue: `NOT (user_id = ANY(...))` on agent-only chats evaluated to
  NULL and dropped them. Exclusions now render as
  `NOT COALESCE(..., FALSE)`, for Options A and D.
- **Writes keep today's shape.** `CanGrant` checks containment in schema
  terms, then `Encode` produces the `principal_grants` row to insert.
- **Cost profile.** Checks run in memory after admission: 2.3 µs for a tool
  call and 0.6 µs for a chat read. Uncached admission on Postgres takes
  0.83 ms for a human and 1.23 ms for an agent. One request costs 1.18 ms
  with one check and 1.39 ms with five.
