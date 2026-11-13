# Instructions for Claude Design: RBAC options briefs

## What to produce

Create six pages that help Speakeasy engineers and engineering leadership
decide *whether* and *how* to rebuild our authorization (RBAC) system:

0. **Why refactor RBAC.** The opening page, which comes before any option. It
   makes the case for change using the evidence below.
1. **Option A: Clean-slate relationships.** A Zanzibar-style relationship
   model stored in our own Postgres, evaluated by our own Go engine.
2. **Option B: Extensible RBAC.** The approved implementation plan. It
   evolves today's grant model with a scope catalogue, a validating client and
   a swappable backend.
3. **Option C: OpenFGA.** The same model running on embedded OpenFGA.
4. **Option D: Relationship schema over today's grants.** A hybrid: Option
   A's schema and engine reading Option B's tables, through a reversible
   mapping.
5. **A comparison page** that puts the four options side by side.

Give each option brief the same five sections, in this order:

1. **How the model looks.** The data model and permission declarations, as
   real code.
2. **How using it looks.** Real handler code.
3. **Changes involved.** What we would build, migrate and remove in our
   codebase.
4. **Benchmarks.** Measured numbers and what they mean.
5. **Strengths and risks.**

Each option brief should fit on one or two pages or slides. A reader should
be able to take in the model in about 30 seconds, then read the code.

## Rules for the content

- **Use the code exactly as given.** Every code block below is real code from
  a working spike that compiles and passes its tests. Don't rewrite, shorten
  or "improve" it. You may drop lines only where the block marks `// ...`.
  Label each block with the file path given.
- **Use only the numbers given.** Don't round them, invent others, or
  extrapolate to production. Always show the benchmark caveat (see the
  Benchmarks section).
- **Present the options neutrally.** The comparison page may state the
  trade-offs given here, but it should not declare a winner. The team decides.
- **Use the same names everywhere.** "Option A — Clean-slate relationships",
  "Option B — Extensible RBAC", "Option C — OpenFGA", "Option D — Relationship
  schema over today's grants".
- **Keep the same example people.** Every option uses the same example
  organization, so the code is comparable. Introduce the cast once, on the
  comparison page or as a shared strip: Alice, Bob, Carol, Dave, Erin, Frank,
  and the agent `agt_triage`.
- **Design style.** Use Speakeasy's design system if you have it. Otherwise
  use a clean, technical style: monospaced code with syntax highlighting,
  plenty of whitespace, one accent color per option (the same color on every
  page for that option), and tables for benchmarks. Avoid decorative imagery.
- **Product name.** Call it "Speakeasy", never "Gram". Code identifiers keep
  their names.

## Opening page — Why refactor RBAC

### Purpose

This page answers "why change anything?" before any option is shown. It
should persuade a skeptical senior engineer who believes the current system
works.

- **Lead with the thesis.**
- **Then show what this is *not*.** This pre-empts the obvious objections.
- **Then the evidence, then the upcoming requirements, then the
  counter-arguments.**
- **Keep the tone factual.** This is a cost-of-change argument, not a
  security alarm.

### Thesis (use as the headline and opening paragraph)

> **Our permission rules have no single owner.** They are spread across at
> least four evaluators and expressed through separate mechanisms: expansion
> maps, opposing scopes, two matching modes, precedence exceptions, and
> versioned delegation ceilings. Every new code path reimplements part of
> these rules, and gaps have repeatedly been found after release. The next
> four requirements are each cross-cutting, so each would have to be threaded
> through every evaluator. We propose consolidating the rules into one model
> with one owner before adding them.

### What this is not

Show these as three short "not" statements, visually de-emphasized:

- **Not a performance problem.** Checks run in memory after loading, and
  latency is fine today.
- **Not a data-model problem.** The spike showed the existing grant tables can
  hold every new requirement (Option D runs on them unchanged).
- **Not a known vulnerability.** The gaps below were found and fixed. The
  argument is the recurring cost and risk of finding them after release.

### Evidence 1: rules have no single owner

Show as a stat strip. These counts come from grep over current `main` and are
approximate; label them "approx.".

| Measure | Count |
| --- | --- |
| Independent evaluators of the same rules (engine, SQL list queries, agent delegation, dashboard `useRBAC`) | 4 |
| Sites that evaluate grants they loaded themselves, outside the engine | ~35 |
| Call sites that load principals or grants directly | ~90 |
| Ad hoc ownership checks (`UserID ==`) that bypass RBAC | ~27 |
| Inline platform-admin (`IsAdmin`) checks | ~20 |
| Handler authorization calls, each an untyped four-field struct | ~520 |

### Evidence 2: gaps keep being found after release

Show as a timeline or list of real pull requests. Each was a correct fix; the
pattern is that a rule was missing on one path, or interpreted differently
there.

| PR | What was missing or inconsistent |
| --- | --- |
| #7202 | Blocks weren't carried into delegated agent credentials. |
| #7561 | Workload operations didn't enforce the agent owner's live permissions. |
| #7315 | Plugin delivery had to locally undo the "read/write implies connect" rule. |
| #5535 | The demo org showed one set of scopes in the dashboard and enforced another. |
| #7661 | Grants embedded in assistant tokens went stale; they are now derived live. |

### Evidence 3: the vocabulary is a set of mechanisms, not a model

Show as a "to add one thing, you touch…" diagram or checklist.

- **Adding a scope** takes about 10 coordinated edits: constants, the
  expansion map, system-role grants, the scope catalogue endpoint, the
  full-access list, three enums in the API design, test counts, docs, and code
  generation.
- **Adding a selector dimension** means touching the allowlist plus about 6
  separate translation functions across the access, agents, keys and Platform
  MCP code.
- **Each rule is a separate mechanism:**
  - the scope-expansion map is keyed backwards (required scope → scopes that
    satisfy it);
  - blocks are separate `*:blocked_*` scopes rather than an effect on a grant;
  - there are two matching modes, permissive and strict;
  - principal precedence has 6 exceptions;
  - delegated policies are versioned, with legacy reinterpretation at
    admission.
- **There are signs of drift:** a stored `effect` column exists that nothing
  reads, and `drop_resource` is deprecated.
- **The guide is long:** `docs/rbac.md` is 1,218 lines, much of it explaining
  exceptions.

### Evidence 4: a missing fact means "allowed" by default

The normal matcher skips any grant restriction the check doesn't supply. A
grant limited to one tool still satisfies a server-level check. That is
intentional for admission, but every new dimension inherits it: an unsupplied
fact silently widens access instead of failing.

We have already had to add a separate strict mode for auth clients and API
keys. Team review, which restricts whose activity a reviewer may read, would
need it too.

Show this as a small before/after:

```text
grant:  mcp:connect on server S, tool = search_docs
check:  mcp:connect on server S            (no tool supplied)
today:  allowed — the tool restriction is skipped
wanted: "undetermined" — a restriction the check can't answer never allows
```

### Why now: four cross-cutting requirements are next

Show as a small grid: requirements down the side, the places each must change
across the top. The visual point is that almost every requirement touches
every column.

| Requirement | Engine | SQL list queries | Agent delegation | Credential encodings | Dashboard |
| --- | --- | --- | --- | --- | --- |
| Denial always wins (no direct-grant override) | ✓ | ✓ | ✓ | ✓ | ✓ |
| Managing a server doesn't grant use | ✓ | ✓ | ✓ | ✓ | ✓ |
| Any grant can expire | ✓ | ✓ | ✓ | ✓ | ✓ |
| Team activity review with protected people | ✓ | ✓ | — (human-only) | — (human-only) | ✓ |

Caption: "Today there is no notion of validity anywhere in the model. Expiry
alone would have to be added to the loaders, versioned credential encodings,
containment, the SQL mirrors and the dashboard."

### Counter-arguments, answered

Show as three question/answer pairs:

- **"Incremental fixes have worked."** True: each gap was closed. But that is
  the cost we are describing, and it grows with every requirement.
- **"This is a rewrite."** It isn't. Options B and D keep the existing data and
  the admission and delegation foundations. The change is consolidating
  semantics, not replacing the system.
- **"Is this a security problem?"** These were correctness gaps found and
  fixed after release. Consolidating the rules reduces how often they occur.

### What success looks like

Close the page with a short list. These targets are shared by every option:

- **One owner of permission semantics.** Checks, lists, delegation and the
  dashboard all ask it, and none reimplements it.
- **Adding a permission is one declaration**, with generated code everywhere
  else.
- **A restriction the check can't answer never allows.**
- **Typed, one-line handler checks** that cannot bypass the engine.
- **"Why does X have access?"** answered from the same evaluation that
  enforces it.

## Shared context for all briefs

### The problem

The opening page, "Why refactor RBAC", states the problem. Each option brief
may restate it in one sentence. The requirements every option must meet:

- **Understandable permissions.**
- **Separate management from use.**
- **Bounded agent self-service.**
- **Scoped activity review:** for example, a team lead can read their team's
  sessions while protected people stay hidden.
- **Temporary access:** any grant can expire.

The system sits on the hot path. It has to stay fast, fail closed, be easy to
extend, and take one line to call from a handler.

### The example organization (identical across options)

| Who                          | Situation                                                                                     |
| ---------------------------- | --------------------------------------------------------------------------------------------- |
| Alice                        | Engineering lead. Reviews her team's activity. Owns the agent `agt_triage`.                   |
| Bob                          | Engineer on Alice's team.                                                                     |
| Carol                        | Executive whose activity is protected.                                                        |
| Dave                         | Platform operator. Can edit servers but not use them.                                         |
| Erin                         | Investigator. Has temporary access to sensitive activity, expiring in 30 minutes.             |
| Frank                        | Organization-wide activity reviewer. Still cannot see Carol.                                  |
| `agt_triage` + key `key_123` | Alice's agent. Allowed three tools on server `srv_linear`. The key is limited to that server. |

Engineering is blocked from destructive tools on `srv_linear`.

### Scenario results (identical across options)

All four options pass the same 20 scenarios, both in memory and on Postgres.
Their list results also agree with each other. Show this as a shared
checklist:

- Management is separate from use: Dave can edit the server but cannot
  connect.
- An agent request passes three limits: the credential, the agent's own
  policy, and the owner's current permissions. `update_issue` is allowed and
  `delete_issue` is denied.
- An explicit denial wins: Engineering cannot call destructive tools.
- Team review: Alice reads Bob's chat. Carol's chat is protected. Dave's chat
  is outside her team.
- Agent history: Alice reads her agent's chats as their owner.
- Protection beats organization-wide review: Frank cannot read Carol's chat.
- Temporary access: Erin can read Carol's chat, then is denied 31 minutes
  later with nothing deleted.
- Self-service: members can create agents.
- Revocation: removing Alice from Engineering denies her agent on the very
  next request.

## Option A — Clean-slate relationships

### One-line summary

A schema written once in Go, one Postgres relationship table, and
subject-side snapshots. Every check is a few in-memory map lookups.

### How the model looks

The permission schema is the only place permissions are declared. Constants,
the catalogue, API enums and dashboard types would all be generated from it.

```go
// model/model.go
s.NewType(MCPServer, s.Parent(Project),
	s.Grant("editor", s.AnchoredAt(Organization, Project, s.Self),
		s.Describe("Edit configuration, connection audience and visibility")),
	s.Grant("viewer", s.AnchoredAt(Organization, Project, s.Self),
		s.Describe("Inspect non-secret configuration")),
	s.Grant("connector", s.AnchoredAt(Organization, Project, s.Self),
		s.Conditions(CondMCPTools), s.Delegable(),
		s.Describe("Connect to the server and call its tools")),
	s.Block("connect_blocked", s.AnchoredAt(Organization, Project, s.Self),
		s.Conditions(CondMCPTools)),

	s.Permission("edit", s.Rel("editor")),
	s.Permission("read", s.Union(s.Rel("viewer"), s.Perm("edit"))),
	// No edge from edit or read: managing a server never grants use.
	s.Permission("connect", s.Rel("connector"), s.ExcludedBy("connect_blocked")),
),
s.NewType(User, s.Parent(Organization), s.MemberOf(Group),
	s.Grant("activity_reviewer", s.AnchoredAt(Organization, Group), s.Sensitive(),
		s.Describe("Read transcripts of this person's sessions")),
	s.Grant("protected_reviewer", s.AnchoredAt(Organization), s.Sensitive()),
	s.Marker("protected", "Hide activity from ordinary reviewers"),

	s.Permission("review_activity", s.Union(
		s.Minus(s.Rel("activity_reviewer"), s.Marked("protected")),
		s.Rel("protected_reviewer"),
	)),
),
s.NewType(Chat, s.Parent(Project), s.Attr("author", User), s.Attr("agent", Agent),
	s.Permission("read", s.Union(
		s.Is("author"),                     // personal history stays with the user
		s.Via("agent", "manage"),           // the agent's current owner
		s.Via("author", "review_activity"), // team or org review, aware of protection
		s.Via("agent", "review_activity"),  // review of the owner's team
	)),
),
```

Storage is one table of `object#relation@subject` rows. Each row has an
optional condition and an optional expiry. A trigger bumps a per-organization
revision on every write.

```sql
-- pgstore/schema.sql
CREATE TABLE authz_relationships (
  id uuid NOT NULL PRIMARY KEY,
  organization_id TEXT NOT NULL,
  object_type TEXT NOT NULL,
  object_id TEXT NOT NULL,
  relation TEXT NOT NULL,
  subject_type TEXT NOT NULL,
  subject_id TEXT NOT NULL,
  subject_relation TEXT NOT NULL DEFAULT '',
  condition_name TEXT,
  condition_params JSONB,
  expires_at timestamptz,
  source TEXT NOT NULL,
  source_ref TEXT,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CONSTRAINT authz_relationships_condition_check CHECK ((condition_name IS NULL) = (condition_params IS NULL))
);

CREATE TABLE authz_revisions (
  organization_id TEXT NOT NULL PRIMARY KEY,
  revision BIGINT NOT NULL DEFAULT 0
);
-- + statement-level triggers that bump authz_revisions on insert/update/delete
```

Example rows from the example organization:

| object                  | relation                  | subject                  | condition / expiry                                       |
| ----------------------- | ------------------------- | ------------------------ | -------------------------------------------------------- |
| `group:grp_eng`         | `member`                  | `user:alice`             |                                                          |
| `role:role_eng`         | `assignee`                | `group:grp_eng#member`   |                                                          |
| `mcp_server:srv_linear` | `connector`               | `role:role_eng#assignee` |                                                          |
| `mcp_server:srv_linear` | `connect_blocked`         | `role:role_eng#assignee` | `mcp_tools {dispositions:[destructive]}`                 |
| `project:proj_web`      | `mcp_server.editor`       | `user:dave`              |                                                          |
| `group:grp_eng`         | `user.activity_reviewer`  | `user:alice`             |                                                          |
| `user:carol`            | `protected`               | `organization:org_acme`  |                                                          |
| `organization:org_acme` | `user.protected_reviewer` | `user:erin`              | expires in 30 min                                        |
| `mcp_server:srv_linear` | `connector`               | `agent:agt_triage`       | `mcp_tools {tools:[list_issues,get_issue,update_issue]}` |

The engine admits the caller once per request. Admission builds one snapshot
per "layer": credential, agent, and owner.

```go
// authz/snapshot.go
type Principal struct {
	Org        string
	Actor      Subject
	Layers     []*Snapshot
	Facts      *OrgFacts
	Revision   int64
	AdmittedAt time.Time // database time captured at admission
	// ...
	ValidUntil time.Time // earliest expiry across layers; zero means never
}
```

### How using it looks

```go
// example/handlers.go
// UpdateToolset: load the row (tenant-scoped), then one line of authorization.
func (s *Service) UpdateToolset(ctx context.Context, ts toolsetRow) error {
	if err := authz.Require(ctx, s.authz, perm.MCPServerEdit, mcpServerObject(ts)); err != nil {
		return err
	}
	// ... mutate
	return nil
}

// CallTool: the same server check, plus the facts its conditions need.
func (s *Service) CallTool(ctx context.Context, ts toolsetRow, tool, disposition string) error {
	return authz.Require(ctx, s.authz, perm.MCPServerConnect, mcpServerObject(ts),
		authz.With(model.MCPToolContext{Tool: tool, Disposition: disposition}))
}

// ListChats: authorization becomes part of the query, so pagination and counts
// only ever see authorized rows. The same Filter would feed ClickHouse.
func (s *Service) ListChats(ctx context.Context, projectID string, limit int) ([]string, error) {
	f, err := authz.List(ctx, s.authz, perm.ChatRead)
	if err != nil {
		return nil, err
	}
	args := []any{projectID, limit}
	where, err := f.SQL(map[string]string{
		"id": "c.id", "project": "c.project_id", "author": "c.user_id", "agent": "c.agent_id", "agent.owner": "a.owner_user_id",
	}, &args)
	// ... SELECT ... WHERE c.project_id = $1 AND <where> ORDER BY c.id LIMIT $2
}
```

The list predicate is generated for Alice and runs against real tables. Show
it as a call-out, since it is this option's signature feature:

```sql
WHERE ((c.user_id = ANY($1))                                   -- her own chats
   OR (a.owner_user_id = ANY($2))                              -- her agents' chats
   OR (c.user_id = ANY($3) AND NOT (c.user_id = ANY($4)))      -- team, minus protected
   OR (a.owner_user_id = ANY($5) AND NOT (a.owner_user_id = ANY($6))))
-- args [alice] [alice] [alice bob] [carol] [alice bob] [carol]
```

### Changes involved

- **New storage:**
  - the `authz_relationships`, `authz_revisions` and `authz_access_requests`
    tables;
  - revision triggers;
  - a one-time translation of `principal_grants`, role assignments and
    directory mappings into relationships.
- **New code:**
  - the schema language and compiler;
  - a `gen:authz` generator for typed `perm.*` and `objects.*`, Goa enums,
    TypeScript and docs;
  - the engine (admission, layers, `Lookup`, `CanGrant`, `Guard`).
- **Rewrite:**
  - about 520 handler `Require` calls move to typed `authz.Require` with
    one-line object adapters;
  - list endpoints apply `Lookup` filters in SQL and ClickHouse;
  - directory sync writes `group#member` rows.
- **Remove:**
  - the SQL policy mirrors (`ListAccessibleMCPServersForUser`,
    `ListAccessibleSkillsForUser`);
  - the separate delegation-containment code;
  - the dashboard's TypeScript matching, which is replaced by
    server-computed permissions.
- **Policy changes customers will notice:**
  - a direct user grant no longer overrides a block;
  - management no longer implies connect.
- **Risk.** We own a new evaluator. We would mitigate this by running OpenFGA
  in CI as a differential test oracle.
- **Spike size:** about 1,180 lines of engine, a 335-line schema DSL, and a
  273-line Postgres store.

### Strengths and risks

- **Strengths:**
  - checks in nanoseconds to microseconds;
  - one list predicate that SQL and ClickHouse can apply before pagination;
  - revocation is immediate (one transaction plus a revision bump);
  - ownership and authorship are read from domain rows, not copied;
  - expiry and conditions compose independently;
  - the one-line typed handler API makes a wrong check fail to compile.
- **Risks:**
  - the largest migration of the three;
  - a new evaluator whose correctness we own;
  - snapshots assume a bounded subject closure and a shallow resource
    hierarchy.

## Option B — Extensible RBAC (approved plan)

### One-line summary

Keep today's grants. Add a scope catalogue, a validating `Client` and a
swappable `Backend`. Normalize grants into rules where a denial always wins,
add validity intervals, and reload authority on every call.

### How the model looks

The catalogue has one definition per scope:

```go
// xrbac/catalog.go
type ScopeDefinition struct {
	Scope             Scope
	Description       string
	ResourceKind      string   // "mcp", "agent", "chat", "project", "org"
	AllowedAnchors    []string // kinds a grant may be anchored at
	RequiredAncestors []string // ancestor kinds an existing-resource target must carry
	Visibility        string   // "user_visible" or "internal"
	GrantSurface      GrantSurface
	ImpliedBy         []Scope // one-step, authored edges
	ExcludedBy        []Scope // opposing scopes
	ExclusionPurpose  ExclusionPurpose
	SelectorKeys      []string // narrowing keys beyond resource_kind/resource_id
	Lifecycle         ScopeLifecycle
	RuntimeSafeSince  int // first agent-runtime version that may hold it; 0 = never
}

// After the semantic cutover: no ImpliedBy, so management never satisfies connect.
{Scope: ScopeMCPConnect, Description: "Connect to the server and call its tools", ResourceKind: "mcp",
	AllowedAnchors: []string{"mcp", "project", "org"}, RequiredAncestors: []string{"project", "org"},
	Visibility: "user_visible", GrantSurface: GrantSurfaceAccess, ExcludedBy: []Scope{ScopeMCPBlockedConn},
	ExclusionPurpose: ExclusionAccessDeny, SelectorKeys: []string{"project_id", "tool", "disposition"},
	Lifecycle: LifecycleActive, RuntimeSafeSince: 1},
```

Storage is today's `principal_grants` plus two additive validity columns.
Stored grants decode into internal rules, and legacy `*:blocked_*` scopes
become explicit denials.

```sql
-- xrbac/pg_schema.sql (mirrors today's table)
CREATE TABLE x_principal_grants (
  id TEXT PRIMARY KEY,
  organization_id TEXT NOT NULL,
  principal_urn TEXT NOT NULL,
  scope TEXT NOT NULL,
  selectors JSONB NOT NULL,
  valid_from timestamptz,   -- new (D3)
  valid_until timestamptz,  -- new (D3), end-exclusive
  created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
```

```go
// xrbac/types.go
type Rule struct {
	ID         string
	Scope      Scope // the scope allowed or denied
	Effect     Effect
	Selector   map[string]string
	ValidFrom  *time.Time
	ValidUntil *time.Time // end-exclusive
	Source     string     // "user:alice", "role:role_eng", "relationship:owner", "credential:key_123"
}
```

Example grants from the example organization. Team review is a
`subject_principal` selector on `chat:read`, and protection is a reviewer-side
`chat:blocked_read`:

| principal          | scope                 | selectors                                                                                       | valid_until |
| ------------------ | --------------------- | ----------------------------------------------------------------------------------------------- | ----------- |
| `role:role_eng`    | `mcp:connect`         | `{resource_kind: mcp, resource_id: srv_linear}`                                                 |             |
| `role:role_eng`    | `mcp:blocked_connect` | `{…, disposition: destructive}`                                                                 |             |
| `user:dave`        | `mcp:write`           | `{resource_kind: mcp, resource_id: *, project_id: proj_web}`                                    |             |
| `user:alice`       | `chat:read`           | `{resource_kind: chat, resource_id: *, project_id: proj_web, subject_principal: role:role_eng}` |             |
| `user:erin`        | `chat:read`           | `{resource_kind: chat, resource_id: *, subject_principal: *}`                                   | in 30 min   |
| `user:frank`       | `chat:read`           | `{…, subject_principal: *}`                                                                     |             |
| `user:frank`       | `chat:blocked_read`   | `{…, subject_principal: user:carol}`                                                            |             |
| `agent:agt_triage` | `mcp:connect`         | `{…, tool: list_issues}` (+ `get_issue`, `update_issue`)                                        |             |
| `agent:agt_triage` | `mcp:blocked_connect` | `{…, disposition: destructive}`, a denial carried over by containment                           |             |

Ownership and self-access are not grants. They are relationship rules derived
from domain rows, and a denial still overrides them:

```go
// xrbac/backend.go
func relationships(userID string, ownedAgents []string) []Rule {
	rules := []Rule{{Scope: ScopeChatRead, Selector: map[string]string{"resource_kind": "chat", "resource_id": "*", "subject_principal": "user:" + userID}, Source: "relationship:self"}}
	for _, a := range ownedAgents {
		rules = append(rules,
			Rule{Scope: ScopeAgentWrite, Selector: map[string]string{"resource_kind": "agent", "resource_id": a}, Source: "relationship:owner"},
			Rule{Scope: ScopeChatRead, Selector: map[string]string{"resource_kind": "chat", "resource_id": "*", "subject_principal": "agent:" + a}, Source: "relationship:owner"})
	}
	return rules
}
```

The backend contract is the plan's interface:

```go
// xrbac/backend.go
type Backend interface {
	Authorize(context.Context, Evaluation) (Decision, error)
	AuthorizeAny(context.Context, []Evaluation) (Decision, error)
	AuthorizeAll(context.Context, []Evaluation) (Decision, error)
	Filter(context.Context, []Evaluation) ([]Target, error)
	DiscoverAccess(context.Context, Subject, AccessQuery) (Decision, error)
	CheckDelegation(context.Context, Subject, Delegation) (Decision, error)
	Explain(context.Context, Evaluation) (Explanation, error)
}
```

### How using it looks

```go
// example/xrbac_handlers.go
func (s *XService) UpdateToolset(ctx context.Context, subject xrbac.Subject, ts xToolsetRow) error {
	if err := s.authz.Require(ctx, subject, xrbac.ResourceCheck(xrbac.ScopeMCPWrite, serverTarget(ts))); err != nil {
		return err
	}
	// ... mutate
	return nil
}

// CallTool: the tool check (and, when used, the auth-client check) in one
// consistent evaluation.
func (s *XService) CallTool(ctx context.Context, subject xrbac.Subject, ts xToolsetRow, tool, classification string) error {
	d, err := s.authz.AuthorizeAll(ctx, subject, []xrbac.Request{
		xrbac.ToolCallCheck(xrbac.ScopeMCPConnect, serverTarget(ts), xrbac.ToolFacts{Name: tool, Classification: classification}),
		// xrbac.ResourceCheck(xrbac.ScopeMCPAuth, authClientTarget), // when an upstream auth client is used
	})
	if err != nil {
		return err
	}
	return d.RequireAllowed()
}

// ListChats: PlanQuery is deferred in the plan, so a list loads a bounded
// candidate page with SQLc and filters it. Totals and pagination are not
// authorization-aware until the query design lands.
func (s *XService) ListChats(ctx context.Context, subject xrbac.Subject, page []string, org, project string) ([]xrbac.Target, error) {
	// ... build one ResourceCheck(ScopeChatRead, chatTarget) per candidate
	return s.authz.Filter(ctx, subject, reqs)
}

// UpdateAgentPolicy: edit permission, then whole-policy containment against
// the editor and the current owner (loaded by the backend, not the caller).
func (s *XService) UpdateAgentPolicy(ctx context.Context, subject xrbac.Subject, agent xrbac.Target, proposed []xrbac.Rule) error {
	if err := s.authz.Require(ctx, subject, xrbac.ResourceCheck(xrbac.ScopeAgentWrite, agent)); err != nil {
		return err
	}
	d, err := s.authz.CheckDelegation(ctx, subject, xrbac.Delegation{Agent: agent, Proposed: proposed})
	if err != nil {
		return err
	}
	if err := d.RequireAllowed(); err != nil {
		return err
	}
	// Save policy + audit in the caller's transaction (plan task 3/4).
	return nil
}
```

A behavior worth a call-out: containment is strict. Alice can give her agent
three tools only if the proposal also carries over her "no destructive tools"
denial. Without it, the proposal is rejected with
`proposal drops an overlapping denial`.

### Changes involved

These are the plan's eight tasks:

1. **Catalogue and generator** for API enums and SDK types.
2. **Typed client and shared evaluator:** deny wins, relationship rules, and
   containment.
3. **PostgreSQL backend loading.** It preserves repeatable-read admission and
   the caller's write transactions, with no cross-request cache.
4. **Migration of admission, delegation and ownership checks.**
5. **Separate MCP capabilities.** This removes management → connect, requires
   explicit connect and tool grants, and migrates the existing defaults (D4).
6. **Validity columns** on `principal_grants`, applied everywhere permissions
   are evaluated and in credential encodings.
7. **API, dashboard, Platform MCP, seeds and docs** realigned to
   server-evaluated capabilities.
8. **Cutover and removal** of `Engine` and the duplicated non-query
   evaluators.

The data model stays the same; the only schema change is additive validity
columns.

- **Deferred:** authorization-aware list queries (`PlanQuery`). Activity lists
  and inventory SQL mirrors stay in place until a SQLc-compatible design
  exists. `Filter` is not a pagination substitute.
- **Open decisions D1–D6:**
  - D1: tool classification;
  - D2: malformed rows;
  - D3: validity storage;
  - D4: moving connection defaults;
  - D5: revocation timing for streams;
  - D6: names.
- **Spike size:** about 1,490 lines (catalogue, client, backend, evaluator,
  Postgres and memory sources).

### Strengths and risks

- **Strengths:**
  - the smallest data migration;
  - it keeps proven identity, delegation and admission foundations;
  - the swappable backend leaves room to adopt another engine later;
  - deny-wins and containment are explicit and strict;
  - revocation is immediate, because there is no cache.
- **Risks:**
  - every client call loads authority, so handlers must batch checks
    (`AuthorizeAll`/`Filter`) to stay fast;
  - list and count parity for activity review is deferred, so team review is
    complete only for single reads;
  - protection must be expressed as a block on each reviewer grant rather
    than once on the protected person;
  - selector-based matching stays the core abstraction.

## Option C — OpenFGA

### One-line summary

Run the same model on embedded OpenFGA v1.22.0, with its own datastore. Our
rows are translated into tuples, and the application checks each layer
separately.

### How the model looks

```text
# fga/model.fga (excerpt)
type user
  relations
    define organization: [organization]
    define member_group: [group]
    define protected: [user:*]
    define activity_reviewer: user_activity_reviewer from member_group or user_activity_reviewer from organization
    define review_activity: (activity_reviewer but not protected) or user_protected_reviewer from organization

type mcp_server
  relations
    define project: [project]
    define editor: [user, role#assignee, group#member] or mcp_server_editor from project
    define viewer: [user, role#assignee, group#member, organization#member] or mcp_server_viewer from project
    define connector: [user, agent, api_key, role#assignee, group#member, organization#member, user with mcp_tools, agent with mcp_tools, api_key with mcp_tools, role#assignee with mcp_tools] or mcp_server_connector from project
    define connect_blocked: [user, agent, role#assignee, group#member, organization#member, user with mcp_tools, agent with mcp_tools, role#assignee with mcp_tools] or mcp_server_connect_blocked from project
    define edit: editor
    define read: viewer or edit
    define connect: connector but not connect_blocked

type chat
  relations
    define author: [user]
    define agent: [agent]
    define read: author or manage from agent or review_activity from author or review_activity from agent

condition mcp_tools(tool: string, disposition: string, tools: list<string>, dispositions: list<string>) {
  (size(tools) == 0 || tool in tools) && (size(dispositions) == 0 || disposition in dispositions)
}

condition not_expired(current_time: timestamp, expires_at: timestamp) {
  current_time < expires_at
}
```

What the translation costs:

- **Extra tuples.** Our 21 relationship rows became 21 mirrored tuples plus 17
  derived tuples. The derived tuples cover facts the other options read from
  domain rows: parents, owner, author, org membership, and reverse group
  pointers.
- **One condition per tuple.** A grant that is both temporary and
  tool-limited cannot be written as one tuple. It needs a composite condition
  for each combination.

### How using it looks

```go
// example/fga_handlers.go
// layers mirrors the multi-policy rule: one Check per required subject.
func layers(agentID, keyID, ownerUserID string) []string {
	return []string{"api_key:" + keyID, "agent:" + agentID, "user:" + ownerUserID}
}

func (s *FGAService) CallTool(ctx context.Context, agentID, keyID, ownerUserID, serverID, tool, disposition string) error {
	ok, err := s.fga.CheckLayers(ctx, layers(agentID, keyID, ownerUserID), "connect", "mcp_server:"+serverID,
		fga.Facts{Tool: tool, Disposition: disposition, Now: time.Now()})
	if err != nil {
		return err // datastore or engine failure: fail closed
	}
	if !ok {
		return errForbidden
	}
	return nil
}

// ListChats: ListObjects returns unsorted, capped ids; every chat must exist
// as tuples (author/agent) for this to work. The ids then feed a SQL query.
func (s *FGAService) ListChats(ctx context.Context, userID string) ([]string, error) {
	return s.fga.ListObjects(ctx, "user:"+userID, "read", "chat", fga.Facts{Now: time.Now()})
}

// RemoveFromGroup: a directory change is a domain write *and* tuple writes.
// They cannot share a transaction; production needs an outbox + replay.
func (s *FGAService) RemoveFromGroup(ctx context.Context, userID, groupID string) error {
	// 1. domain write (WorkOS sync row) — in our Postgres transaction
	// 2. outbox → OpenFGA, after commit:
	return s.fga.Delete(ctx,
		&openfgav1.TupleKeyWithoutCondition{Object: "group:" + groupID, Relation: "member", User: "user:" + userID},
		&openfgav1.TupleKeyWithoutCondition{Object: "user:" + userID, Relation: "member_group", User: "group:" + groupID})
}
```

### Changes involved

- **Deploy:**
  - OpenFGA, either embedded as a Go library or as a service;
  - a dedicated datastore (a separate Postgres database) with OpenFGA's own
    migrations;
  - a per-worktree setup for local development.
- **Sync:** an outbox that projects every grant, role assignment, directory
  membership, parent link, agent owner and chat author into tuples, with
  replay and reconciliation. Writes are not atomic with domain changes.
- **Application code still needed:**
  - layer composition (credential, agent, owner);
  - condition facts;
  - delegation containment;
  - stream revalidation;
  - turning `ListObjects` ids into paginated SQL queries;
  - Explain translation.
- **Migration.** The same policy changes as Option A, plus translating every
  stored grant into tuples. Model versions are pinned per release.
- **Effort.** An earlier assessment estimated 5–10 engineer-days for a
  feasibility spike, 4–8 more weeks for the first production domain, and
  several months for a broad migration. That estimate was made before this
  spike, at low confidence.

### Strengths and risks

- **Strengths:**
  - a mature graph evaluator;
  - a declarative, portable model with its own tooling (playground,
    `fga model test`, versioned models);
  - not limited to shallow hierarchies.
- **Risks:**
  - slowest without caches;
  - its caches serve stale answers: with a 10-second cache, a revoked agent
    was still allowed immediately after revocation in our test;
  - dual writes outside our transactions;
  - every chat must be mirrored as tuples for lists to work;
  - one condition per tuple;
  - a new failure point on the hot path.

## Option D — Relationship schema over today's grants

### One-line summary

Option A's schema, engine and handler API, running on Option B's tables.
Every schema grant names the stored scope it maps to, and one reversible
mapping turns each `principal_grants` row into a relationship and back.

### How the model looks

This is the same language as Option A. Each grant adds `StoredAs`, and a grant
can be limited to whose activity it covers with `SubjectScoped`:

```go
// hmodel/hmodel.go
s.NewType("mcp_server", s.Parent("project"),
	s.Grant("viewer", s.StoredAs("mcp:read"), s.AnchoredAt("organization", "project", s.Self),
		s.Describe("Inspect non-secret configuration")),
	s.Grant("editor", s.StoredAs("mcp:write"), s.AnchoredAt("organization", "project", s.Self),
		s.Describe("Edit configuration, connection audience and visibility")),
	s.Grant("connector", s.StoredAs("mcp:connect"), s.AnchoredAt("organization", "project", s.Self),
		s.Conditions(model.CondMCPTools), s.Delegable(),
		s.Describe("Connect to the server and call its tools")),
	s.Block("connect_blocked", s.StoredAs("mcp:blocked_connect"), s.AnchoredAt("organization", "project", s.Self),
		s.Conditions(model.CondMCPTools)),

	s.Permission("edit", s.Rel("editor")),
	s.Permission("read", s.Union(s.Rel("viewer"), s.Perm("edit"))),
	// No edge from edit or read: managing a server never grants use.
	s.Permission("connect", s.Rel("connector"), s.ExcludedBy("connect_blocked")),
),

s.NewType("user", s.Parent("organization"),
	// One row on the person, not a block on every reviewer grant.
	s.MarkerAs("protected", "Hide activity from ordinary reviewers", "user:protected"),
	s.Permission("is_protected", s.Marked("protected")),
),

s.NewType("chat", s.Parent("project"), s.Attr("author", "user"), s.Attr("agent", "agent"),
	// chat:read with a subject_principal selector, as Option B stores it.
	s.Grant("reader", s.StoredAs("chat:read"), s.AnchoredAt("organization", "project"),
		s.SubjectScoped("author", "agent"), s.Sensitive(),
		s.Describe("Read transcripts of the selected people's sessions")),
	s.Grant("protected_reader", s.StoredAs("chat:read_protected"), s.AnchoredAt("organization", "project"),
		s.SubjectScoped("author", "agent"), s.Sensitive(),
		s.Describe("Read protected people's sessions (temporary access)")),
	s.Block("read_blocked", s.StoredAs("chat:blocked_read"), s.AnchoredAt("organization", "project"),
		s.SubjectScoped("author", "agent")),

	s.Permission("read", s.Union(
		s.Is("author"),           // personal history
		s.Via("agent", "manage"), // the agent's current owner
		s.Minus(s.Rel("reader"), s.Via("author", "is_protected")),
		s.Rel("protected_reader"),
	), s.ExcludedBy("read_blocked")), // a matching block beats everything, self-access included
),
```

Storage is Option B's tables, unchanged: `principal_grants`, plus the validity
columns, role assignments and directory mappings. The mapping derives from
`StoredAs`, so no one maintains a second table by hand. Every row of the
example organization round-trips exactly; show a few of them as a two-column
"stored row ⇄ relationship" table:

| Stored row (principal · scope · selectors) | Relationship the engine sees |
| --- | --- |
| `role:role_eng` · `mcp:connect` · `{resource_kind: mcp, resource_id: srv_linear}` | `mcp_server:srv_linear#connector@role:role_eng` |
| `role:role_eng` · `mcp:blocked_connect` · `{…, disposition: destructive}` | `mcp_server:srv_linear#connect_blocked@role:role_eng` |
| `user:dave` · `mcp:write` · `{resource_kind: mcp, resource_id: *, project_id: proj_web}` | `project:proj_web#mcp_server.editor@user:dave` |
| `user:alice` · `chat:read` · `{…, project_id: proj_web, subject_principal: role:role_eng}` | `project:proj_web#chat.reader@user:alice` (scoped to role_eng members) |
| `user:erin` · `chat:read_protected` · `{…, subject_principal: *}` (valid_until in 30 min) | `organization:org_acme#chat.protected_reader@user:erin` |
| `agent:agt_triage` · `mcp:connect` · `{…, tool: list_issues}` | `mcp_server:srv_linear#connector@agent:agt_triage` |
| `organization:org_acme` · `user:protected` · `{resource_kind: user, resource_id: carol}` | `user:carol#protected@organization:org_acme` |

There are two new row conventions, and both are ordinary `principal_grants`
rows:

- a `user:protected` marker, one row on the protected person;
- a `chat:read_protected` scope for protected review.

Any row the schema cannot express fails the load instead of being silently
ignored. These are the spike's real error messages:

```text
grant bad1: unsupported selector keys [region]
grant bad2: mcp_server.editor takes no tool constraints
grant bad3: chat.reader cannot be anchored at self
grant bad4: scope "risk_policy:bypass" has no schema mapping
```

### How using it looks

Handlers are written exactly as in Option A. Only the wiring differs:

```go
// example/hybrid_handlers.go
func NewHybridEngine(pool *pgxpool.Pool) (*authz.Engine, *hybrid.Mapping, error) {
	m, err := hybrid.NewMapping(hmodel.Schema) // fails at startup if two relations claim one scope
	if err != nil {
		return nil, nil, err
	}
	return authz.New(hmodel.Schema, &hybrid.PgStore{Pool: pool, M: m}, nil), m, nil
}

func (s *HybridService) CallTool(ctx context.Context, ts toolsetRow, tool, disposition string) error {
	return authz.Require(ctx, s.authz, perm.MCPServerConnect, mcpServerObject(ts),
		authz.With(model.MCPToolContext{Tool: tool, Disposition: disposition}))
}

// GrantAgentTool: containment in schema terms, then Encode writes the row in
// today's principal_grants shape — the role editor and APIs keep working.
func (s *HybridService) GrantAgentTool(ctx context.Context, org string, server objects.MCPServer, agentID, tool string) error {
	p, _ := authz.PrincipalFrom(ctx)
	params, _ := json.Marshal(model.MCPToolsParams{Tools: []string{tool}})
	if err := s.authz.CanGrant(ctx, p.Actor.ID, []authz.Proposal{{
		ForType: "mcp_server", Grant: "connector", Anchor: server.AuthzRef(), Params: params,
	}}); err != nil {
		return err
	}
	row, err := s.mapping.Encode(org, authz.Relationship{
		Object: authz.ObjectID{Type: "mcp_server", ID: server.ID}, Relation: "connector",
		Subject: authz.Agent(agentID), ConditionName: model.CondMCPTools, ConditionParams: params, SourceRef: "<NEW_GRANT_ID>",
	})
	// ... INSERT row into principal_grants
}
```

Lists get Option A's predicate, run against B's tables. Show this as a
call-out. It is the generated filter for Frank, an organization-wide reviewer
who must not see the protected Carol:

```sql
WHERE ((c.user_id = ANY($1)) OR (a.owner_user_id = ANY($2))
   OR (c.id IS NOT NULL AND (c.user_id IS NOT NULL) AND NOT COALESCE((c.user_id = ANY($3)), FALSE))
   OR (c.id IS NOT NULL AND (c.agent_id IS NOT NULL) AND NOT COALESCE((c.user_id = ANY($4)), FALSE)))
-- args [frank] [frank] [carol] [carol]
```

Note for the brief: running Option A's list predicate on these rows caught a
real SQL bug. Without `COALESCE`, `NOT (NULL)` silently dropped agent-only
chats. It is now fixed for Options A and D.

### Changes involved

- **Kept from Option B's plan:**
  - the data and tables;
  - validity columns (task 6);
  - loading and admission orchestration (task 3);
  - delegation and ownership migration (task 4);
  - separating MCP capabilities (task 5);
  - aligning API, dashboard and Platform MCP (task 7);
  - cutover (task 8);
  - decisions D1–D6.
- **Replaced:**
  - task 1's hand-written `ScopeDefinition` catalogue becomes the schema
    language with `StoredAs`; the catalogue, API enums and SDK types are
    generated from it;
  - task 2's evaluator becomes Option A's compiled engine, extended with
    subject-scoped grants;
  - the per-call `Client` load becomes one admission per request;
  - the deferred `PlanQuery` becomes Option A's `Lookup` predicate.
- **New:**
  - the `hybrid` mapping (`Decode` and `Encode`), plus round-trip tests over
    production row shapes;
  - an inventory of row shapes that cannot be mapped;
  - two row conventions (the protection marker and protected review);
  - handler migration to typed `authz.Require`, as in Option A.
- **Optional later:** a revision trigger on `principal_grants` and the
  membership tables, which would enable Option A's revision-checked cache.
- **No data migration for rows that map.** Rows that don't map must be found
  and fixed before cutover; until then they fail closed.
- **Spike size:** about 680 lines (the mapping, two stores, and the schema),
  plus about 90 lines of engine changes. It reuses Option A's engine.

### Strengths and risks

- **Strengths:**
  - Option A's expressive declarations, typed handler API, list predicate and
    one-marker protection;
  - Option B's data, no migration of existing rows, and B's plan for
    admission, delegation and rollout;
  - checks run in memory after one admission per request.
- **Risks:**
  - the mapping is a second representation that must stay reversible;
  - unusual legacy row shapes need explicit handling;
  - we still own the evaluator;
  - admission currently makes sequential round trips (about 6 for a human
    and 9 for an agent, including `BEGIN` and `ROLLBACK`), which could be
    pipelined;
  - without a revision table there is no cache, so every request pays
    admission.

## Benchmarks

Show this caveat every time benchmark numbers appear:

> Measured on an Apple M4 Pro against local Docker Postgres 18, on a tiny
> dataset, with `go test -bench -benchtime=2s`. Treat these as orders of
> magnitude, not capacity numbers. Production latency also depends on network
> round trips to the database.

### Per check, with authority already loaded

These measure evaluation only. For Option B, "memory" includes its per-call
load from an in-memory source. Option D is evaluated in memory after
admission.

| Check                                             | Option A | Option B (memory) | Option C (memory) | Option C (Postgres) | Option C (Postgres + 10s cache) |
| ------------------------------------------------- | -------- | ----------------- | ----------------- | ------------------- | ------------------------------- |
| One agent tool call (3 layers, condition + block) | 1.75 µs  | 2.9 µs            | 444 µs            | 2.52 ms             | 15.2 µs                         |
| Team-review check on one chat                     | 0.44 µs  | 2.4 µs            | 192 µs            | 877 µs              | 4.7 µs                          |
| `tools/list`, 40 tools                            | 42 µs    | 36 µs (`Filter`)  | 9.5 ms            | 89 ms               | 555 µs                          |

Option D, same checks: 2.29 µs, 0.61 µs and 55 µs.

### Per request, end to end on Postgres

This includes loading authority: admission for Options A and D, and the
per-call load for Option B.

| Request                       | Option A, uncached | Option A, revision-cached | Option B                                                             |
| ----------------------------- | ------------------ | ------------------------- | -------------------------------------------------------------------- |
| One agent tool call           | 1.31 ms            | 0.62 ms                   | 1.03 ms                                                              |
| Five checks in one request    | 1.21 ms            | —                         | 5.58 ms as five `Require` calls; 1.02 ms batched with `AuthorizeAll` |

Option D, uncached, on Option B's tables:

- one agent tool call: 1.18 ms;
- five checks in one request: 1.39 ms;
- admission alone: 0.83 ms for a human, 1.23 ms for an agent with three
  layers.
| Team-review check on one chat | —                  | —                         | 0.92 ms                                                              |
| `tools/list`, 40 tools        | admission + 42 µs  | admission + 42 µs         | 1.06 ms (one `Filter`)                                               |

Key findings to state:

- **Option A** pays once per request at admission; every check after that is
  in memory. Its cache never serves a revoked grant, because each request
  reads the revision.
- **Option B** pays about 1 ms per client call. A handler that makes several
  separate calls multiplies that cost, so batching is required for good
  latency.
- **Option C** is fast only with its caches, and its caches delay revocation.
- **Option D** has Option A's shape: one admission per request, then
  in-memory checks, so five checks cost about the same as one. It reads B's
  tables with no cache yet.
- **Neither OpenFGA figure includes keeping tuples in sync**, which is
  additional work on every write.

## Comparison page

Lay this out as a four-column table, followed by "When to prefer each".

| | Option A — Clean-slate relationships | Option B — Extensible RBAC | Option C — OpenFGA | Option D — Relationship schema over today's grants |
| --- | --- | --- | --- | --- |
| Data model | New relationship table | Today's grants + validity columns | OpenFGA tuples in a separate store | Today's grants + validity columns (two new row conventions) |
| Where rules live | One Go schema, generated everywhere | One scope catalogue + shared evaluator | `model.fga` + application layer code | One Go schema with `StoredAs`, generated everywhere |
| Handler call | `authz.Require(ctx, e, perm.X, obj)`; principal admitted once | `client.Require(ctx, subject, ResourceCheck(scope, target))`; loads per call | `CheckLayers(...)` per layer | Same as Option A |
| Lists, counts, pagination | Predicate pushed into SQL/ClickHouse | Deferred; bounded `Filter` meanwhile | `ListObjects` (capped, unsorted) + SQL | Same predicate as Option A, on today's tables |
| Revocation | Immediate (revision check) | Immediate (no cache) | Immediate only without cache | Immediate (no cache yet) |
| Expiry | Column on every row | Validity columns on grants | Condition (one per tuple) | Validity columns on grants |
| Protected people | One marker on the person | A block on each reviewer grant | `but not protected` | One marker row on the person |
| Migration size | Largest | Smallest | Large, plus new infrastructure | Small for data; handlers as in Option A |
| Evaluator correctness | Ours (OpenFGA as CI oracle) | Ours | OpenFGA's | Ours (OpenFGA as CI oracle) |
| New moving part | Relationship store | Client/backend | External engine + sync | Reversible row ⇄ relationship mapping |
| Spike code | ~1,800 lines | ~1,490 lines | ~260 lines + 88-line model | ~680 lines on top of Option A's engine |

When to prefer each:

- **Option A:** when activity review lists, hot-path latency and a single
  source of permission semantics matter most, and we accept a large one-time
  migration.
- **Option B:** when the smallest, incremental change on the existing data
  matters most, and we accept deferred list parity and batching discipline in
  handlers.
- **Option D:** when we want Option A's expressiveness, list predicate and
  handler ergonomics without migrating stored grants, and accept owning a
  reversible mapping.
- **Option C:** when a proven external evaluator and deep or arbitrary
  hierarchies matter more than latency, transactional writes and operational
  simplicity.

## Source

- **Spike code:** `spikes/clean-slate-authz/` in the Speakeasy repository.
  - Option A: `schema/`, `model/`, `authz/`, `pgstore/`.
  - Option B: `xrbac/`.
  - Option C: `fga/`.
  - Option D: `hmodel/`, `hybrid/`, and Option A's `authz/`.
  - Shared: `fixtures/`, `spike_test.go`, `xrbac_test.go`,
    `hybrid_test.go`.
- **Option A design:** `docs/rfcs/clean-slate-authz-design-review.md`.
- **Option B plan:** "Technical implementation plan: Extensible RBAC for
  Speakeasy" (approved).
