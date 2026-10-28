# Clean-slate authorization: requirements review

This is the investigation checkpoint for a clean-slate authorization design. It
lists what the current RBAC system has to do, what it has to start doing, and
the non-functional bar. It does not propose a design. Once you approve this
baseline, the next checkpoint will compare a Zanzibar-style relational model in
Postgres with OpenFGA, and will include the data models, interfaces and handler
examples.

## Evidence boundary

We inspected `main` at `712d7c14ff`. Tests were read, not run. We used the
Practical RBAC drafts in `docs/rfcs/` as source material, especially the
investigation and OpenFGA feasibility reviews, and checked their claims against
the current code. The personal engineering library could not be searched
because its index points to a missing document file.

Call-site counts come from grepping non-test Go files and are approximate.

## 1. What the current system decides

These are the behaviors a replacement must reproduce, or must change on
purpose. They are grouped by the part of the model they constrain.

### Vocabulary and resources

- **R1. Code-defined permissions.** About 20 positive scopes named
  `<resource>:<verb>`, a matching `*:blocked_*` exclusion for most of them, and
  an internal `root`. Implications are explicit and one step deep, for example
  `write ⇒ read` and `mcp:read|write ⇒ mcp:connect`. The API shows them to
  clients as `sub_scopes`.
  _Evidence:_ `authz/scopes.go` (`scopeExpansions`); `access.ListScopes`.
- **R2. A resource hierarchy.** An organization contains projects. A project
  contains MCP servers (hosted and external), assistants, skills,
  environments, plugins and risk policies. A grant can name a resource, every
  resource of a kind, or every resource of a kind inside a project (the
  `project_id` dimension).
  _Evidence:_ `authz/selector.go`; `AssistantCheck` and `MCPCheck` in
  `authz/checks.go`.
- **R3. Finer dimensions inside a resource.** MCP grants can narrow by `tool`
  and `disposition`. Auth client grants can narrow by `client_type`,
  `connection_owner` and `upstream_client`. Risk blocks can narrow by
  `server_url`. Every scope family has its own allowlist of dimension keys.
  _Evidence:_ `allowedSelectorKeys` and `ValidateSelector` in `selector.go`;
  `MCPAuthClientCheck`.
- **R4. Sensitive permissions are never implied and never defaults.**
  `log:read`, `chat:read` and `chat:write` are not implied by any other scope,
  not even `org:admin`, and no system role holds them. Members do not get
  `environment:read`, because environment values contain secrets.
  _Evidence:_ `docs/rbac.md` (Scope, System Roles).

### Subjects

- **R5. Several kinds of principal.**
  - Principals that hold grants: `user`, `role:global|organization`,
    `user:all`, `agent` and `api_key`.
  - Callers that hold no grants: workloads (they inherit through their assigned
    agent), assistant tokens, chat sessions and external users.
  - Teams have no principal. Directory groups and attributes are mapped onto
    roles, and there is no group principal type.

  _Evidence:_ `urn/principal.go`; `ResolveUserPrincipals` and
  `ResolveAgentPrincipals` in `authz/principal.go`; `directory_role_mappings`.

- **R6. Roles.** WorkOS reconciles role assignments. Two system roles (admin
  and member) are seeded and cannot be edited, and organizations can create
  custom roles. Agents get roles through a separate assignment table. An
  agent's grants are filtered through the runtime scope allowlist, so a role
  cannot give an agent a scope that the agent could not be granted directly.
  _Evidence:_ `organization_role_assignments`, `agent_role_assignments`;
  `runtimepolicy/scopes.go`.

### How grants combine

- **R7. Allow minus exclusion.** A matching `blocked_*` grant subtracts from an
  allow. An exclusion never grants access by itself.
  _Evidence:_ `authz/expressions.go` (`GrantDifference`).
- **R8. Principal precedence.**
  - A grant made directly to a user and naming a concrete resource outranks a
    block the user inherits from a role or `user:all`.
  - The user's own blocks always apply. Wildcard direct grants, agent grants
    and inferred (backfilled) grants never outrank a block.
  - `risk_policy:bypass` cannot be outranked.

  _Evidence:_ `authz/precedence.go`. The Practical RBAC draft proposes dropping
  this exception.

- **R9. Two matching modes.** Most checks skip a dimension that the grant
  constrains but the check does not supply. This is how server-level admission
  passes before the tool is known. Auth client checks and API key grants
  instead require every dimension to match.
  _Evidence:_ `Selector.Matches` and `StrictMatches`; `apiKeyGrantMatches`.
- **R10. Several policies, all of which must allow.** An agent-credential
  request must pass each of these independently: the credential's stored or
  derived ceiling, the agent's policy, the current owner's policy, and the
  authorizer's policy. For `RequireAny`, one complete alternative must pass
  every policy; clauses cannot be borrowed across alternatives.
  _Evidence:_ `grantAuthorization.evaluate` in `authz/context.go`;
  `runtimepolicy/{credential,workload}_admission.go`.
- **R11. Risk-policy algebra.** A risk policy applies when the caller has
  `risk_policy:evaluate` and does not have a matching `risk_policy:bypass`.
  `risk_policy:block` is an allow-by-default blocklist held only by
  `user:all`, and it is read directly rather than through `Require`.
  _Evidence:_ `risk/scanner.go`; `risk/policy_bypass_evaluator.go`.
- **R12. Request-bound symbols.** The engine binds `$represented_user` once
  per request from trusted context, so personal connections can be used only
  by their owner. Delegated policies keep the symbol unresolved.
  _Evidence:_ `docs/rbac.md` (Auth client checks).

### Questions the system answers

- **R13. Six different authorization questions.**

  | Question                                                | Today                                                            |
  | ------------------------------------------------------- | ---------------------------------------------------------------- |
  | May I do X on Y?                                        | `Require` and `RequireAny`, about 520 calls                      |
  | Which of these IDs may I access?                        | `Filter` and `FindMatched`, about 19 calls                       |
  | Which resources may I access? (SQL)                     | `ListAccessibleMCPServersForUser`, `ListAccessibleSkillsForUser` |
  | May I delegate this grant set?                          | `runtimepolicy.DelegationContained`                              |
  | Why do I have, or not have, access?                     | `ExplainGrantCheck`, `access/explain_access.go`                  |
  | Mandatory MCP connect/auth, even for callers RBAC skips | `AuthorizeMandatory`, `PrepareMandatory`                         |

  Today these questions are answered by four separate evaluators: the engine,
  SQL, runtimepolicy, and the dashboard.

### Lifecycle and freshness

- **R14. Revocation is immediate, because nothing is cached.**
  - `PrepareContext` runs on every request and makes three or four Postgres
    queries: org membership, role principals including directory mappings,
    grants, and sometimes global roles.
  - Credential and workload admission read every policy in one
    repeatable-read transaction. The code comments say these must not be
    cached across requests.
  - Long-lived requests use `RefreshContext` to reload and `AdmissionBoundary`
    to stay intersected with what was originally admitted.
  - A suspended or revoked agent, or one waiting on an owner reassignment,
    loses all of its principals.

  _Evidence:_ `engine.go:112–209`; `credential_admission.go:31`;
  `context.go:58–92`.

- **R15. Provisioning.** New organizations get system-role grants and the
  `user:all` personal `mcp:auth` default. The `auth_permission_provisions`
  ledger keeps a backfill from restoring grants an admin removed, and every
  explicit write path must mark matching ledger rows as taken over.
  _Evidence:_ `authz/provisioner.go`; `MarkAuthPermissionTakenOverTx`.
- **R16. Policy versioning.** Delegated policies carry a version (currently 4,
  with 5 coming). Older versions are reinterpreted on admission, for example
  as a legacy auth ceiling. The runtime scope allowlist is also versioned.
  _Evidence:_ `runtimepolicy.admitStoredPolicy`; `IssuedDelegatedPolicyVersion`.

### Special modes and bypasses

- **R17. Paths that skip or replace normal enforcement.**
  - Skipped: legacy API keys, sessionless requests (except assistant tokens
    and Platform MCP), and public MCP servers.
  - Fixed grant set: the demo organization.
  - All scopes: support sessions.
  - Overridden: scope overrides in dev, or for platform admins. Under an
    override, agent management can only narrow.
  - Separate checks: platform-staff `IsAdmin`, with 15 `RequirePlatformAdmin`
    callers and about 20 inline checks.
  - Denied: sensitive content during impersonation.

  _Evidence:_ `Engine.ShouldEnforce`; `contextvalues.IsSupportSession`;
  `authz/sensitive_content.go`.

### Gates that are not RBAC

- **R18. Other access controls.**
  - Ownership: a chat owner bypasses `chat:read`, an agent owner manages their
    own agent, and there are about 27 ad hoc `UserID ==` comparisons.
  - API key transport scopes: consumer, producer, chat, hooks, agent and
    agent_user.
  - Product feature gates.
  - Separately matched audiences for risk policies and plugin assignments,
    which include `email:` principals.

  _Evidence:_ `chat/impl.go:1060–1118`; `agentmanagement/authorize.go`;
  `auth/key.go:30–40`; `risk/policy_audience.go`.

### Observability and clients

- **R19. Decision telemetry.** The challenge logger publishes decisions to the
  outbox, gated by a feature flag. Mandatory-shadow metrics compare old and new
  decisions. Role and member mutations are audited.
  _Evidence:_ `authz/challenge_logger.go`; `docs/rbac.md` (Shadowing).
- **R20. Dashboard parity.**
  - `useRBAC` and `RequireScope` reimplement selector matching, exclusions and
    precedence in TypeScript.
  - The scope enum must be kept in sync in three places in
    `design/access/design.go`.
  - `ListScopes` is the catalog.

  _Evidence:_ `client/dashboard/src/hooks/useRBAC.ts`.

## 2. New requirements and what they demand of the model

| Requirement                          | What the model must provide                                                                                                                                                                                                                                                                                                                                                           |
| ------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Understandable permissions**       | Each permission is defined once, with its description, examples, implications and sensitivity. There is a declared mapping from a workflow to the permissions it needs. "Why" is answered by an explain query that returns the chain of relationships, for example: user → role → grant on project → server.                                                                          |
| **Decouple MCP management from use** | No implication from management to `connect`. Use comes from a per-server connection audience, which can include an "all eligible members" subject set. Configuration permissions stay separate.                                                                                                                                                                                       |
| **Bounded agent self-service**       | `agent:create` on the organization. `owner` becomes an object relation that confers management of that agent only. Delegation is checked for containment against both the caller and the owner.                                                                                                                                                                                       |
| **Scoped activity review**           | Activity rows carry verified human or agent attribution. A reviewer holds `chat:read` or `log:read` over a subject set (a team, a role, a person, a project). Protected employees are an exclusion that wins over reviewer grants. The filter must push down into Postgres and ClickHouse queries so the same rule covers lists, counts, search, downloads, streams and Platform MCP. |
| **Temporary access**                 | Every grant and every role assignment can carry an expiry, evaluated at check time against server time, without depending on cleanup. Open streams and queued exports must revalidate when access expires or is revoked.                                                                                                                                                              |

## 3. Non-functional requirements

- **Fast.** After admission, a check must cost zero I/O. Today checks are
  already in memory once `PrepareContext` has run. The costs are elsewhere:
  - `PrepareContext` makes three or four queries per request, with no cache.
  - MCP surfaces repeat admission.
  - `tools/list` runs one `Require` per tool.
  - Risk scans reload principals and grants for each scan.
  - Each list surface runs its own SQL mirror.

  We have not measured latency. A budget, for example for p99 admission, needs
  to be agreed rather than assumed.

- **Reliable.**
  - Fail closed.
  - When a fact that a restriction depends on is missing, the result is
    "undetermined", never "allow".
  - Revocation must take effect within an agreed window. Today the window is
    zero.
  - One evaluator, so that checks, lists and the dashboard cannot disagree.
- **Extendable.** Adding a scope today takes about ten coordinated edits: the
  constants, the expansions map, system-role grants, `ListScopes`, the
  full-access list in `ListGrants`, three design enums, test counts, the docs
  table, and code generation. The target is one declaration plus generated
  code.
- **Dead easy in handlers.** Today a check is an exhaustruct `Check` literal
  with four fields. About 35 sites load grants by hand, and about 90 call
  `ResolveUserPrincipals` or `LoadGrants` directly. The target is one typed
  call per handler that takes a loaded resource, plus a matching list helper.
  Bypassing the engine should not be possible.

## 4. Uncertainties that could change the design

- Whether the clean-slate design must keep the stored grants and the
  direct-user precedence exception (R8), or may migrate them. The Practical
  RBAC draft already proposes removing the exception.
- What a protected employee is: a user attribute, a group, or a per-reviewer
  exclusion. Who may set it?
- Delegation semantics. "Can delegate only permissions they're allowed to
  grant" could mean either:
  - delegation is limited to what the caller holds, as in today's containment
    check, or
  - delegation requires a separate grantable right, such as an `admin` option
    on each grant.
- How stale a permission may be on the hot path. A short-lived cache, a
  version-stamped cache, or none at all, as today.
- Whether legacy API keys, transport scopes and platform-staff `IsAdmin` are in
  scope, or remain separate gates.
- Whether teams are WorkOS directory groups or a first-class Speakeasy object.

## Review question

:::question
Is this requirements baseline complete enough to start comparing designs?

Approval confirms the baseline only. It does not settle the uncertainties in
section 4 or choose an engine.

- [ ] Yes. Propose the clean-slate model, with the OpenFGA comparison and code.
- [ ] Not yet. A requirement is missing or wrong; see my comments.
:::
