# RFC: Risk Policy Audience (RBAC-backed)

| Field   | Value                                           |
| ------- | ----------------------------------------------- |
| Status  | Draft                                           |
| Author  | tgmendes (plan by Claude)                       |
| Created | 2026-05-29                                      |
| Related | `docs/rbac_for_policies.md` (problem statement) |
| Scope   | Server. UI out of scope.                        |

## 0. TL;DR — key decisions

- **What:** give risk policies an _audience_ — the set of principals a policy is
  evaluated for. Positive targeting only (no bypass/deny). See §2.
- **Coarse tier on the policy row, not RBAC.** `risk_policies.audience_type ∈
{everyone, targeted}`, **default `everyone`**. Existing policies are
  unchanged on day one — zero behavior change until someone sets `targeted`.
  (§3)
- **Fine tier via an RBAC relation, not a new table.** `principal —
risk_policy:evaluate → policy` rows in `principal_grants`. No detector/rule
  content in selectors — only `{resource_kind, resource_id}`. (§4)
- **Asymmetric fail-safe is the load-bearing rule.** Audience may only _narrow_
  enforcement for callers Gram can tie to an in-org `user:<id>`. Anyone
  unidentifiable (no auth, api-key with no owning user, stale/cross-org user, any
  error) is **always scanned by every policy**. The dangerous "unauthenticated
  caller slips a policy" outcome is impossible by construction. (§2.1)
- **No new engine primitive.** Evaluation uses `authz.LoadGrants` directly, which
  ignores `ShouldEnforce` and the RBAC feature flag — sidestepping the API-key
  allow-all trap in the standard enforcement entry points. Evaluation is therefore
  flag-independent; _authoring_ is gated instead. (§5)
- **`risk_policy:evaluate` is an internal relation, not a product capability.** It
  is hidden from the RBAC UI/enums/seeding **and** — critically — protected from
  being clobbered when a role is edited in the generic RBAC editor (the role-grant
  replace path deletes _all_ grants for a principal today). This protection lands
  **before** any audience row is ever written. (§8)
- **Authoring lives on the policy page**, embedded in the risk-policy
  create/update payload as structured targets, written in the same transaction.
  Replace-set semantics per policy. Audience-only edits do **not** bump policy
  version (offline ignores audience; a bump would force a pointless re-drain). (§7)
- **In scope:** shadow-MCP block path (same gating). **Deferred:** audience-aware
  offline drain (stays complete, ignores audience). **Not now:** caching,
  `role:public`, wildcard/deny grants. (§6, §9)

## 1. Summary

Give risk policies an **audience**: the set of principals (users/roles) a policy
applies to. Today every enabled+enforcing policy applies to every caller in the
project. We want a policy to be evaluated only for callers in its audience, with
an explicit "applies to everyone" option that is also the default.

We back this with RBAC (decision already made in the problem statement). A new
relation `risk_policy:evaluate` links a principal to a policy. At hook time we
resolve the caller's principal set, load their audience grants, and evaluate only
the policies they are in the audience of (plus all "everyone" policies).

## 2. The most important thing to get right (read first)

A grant here means **"this policy applies to me"** — positive inclusion. That
makes the default and the failure mode the two decisions that matter most,
because the dangerous direction is _"no grant ⇒ don't scan."_ Two principles
follow:

- **Default (no config) must remain "scan everyone."** A policy with no audience
  configured must still apply to all callers (§3). Anything else silently stops
  enforcing every existing policy the moment this ships — a security regression.
- **Errors never reduce scanning.** Any failure to establish who the caller is
  must fall back to scanning, never to skipping (§2.1).

### 2.1 The audience asymmetry (the rule everything hinges on)

Audience may only ever _narrow_ enforcement for callers Gram can **identify**. It
must never let an unidentified caller escape a policy. There are two distinct
"no matching grant" cases and they resolve **oppositely**:

1. **Authenticated, a known Gram principal in this org, but no audience grant for
   the policy.** For a `targeted` policy this caller is genuinely _outside the
   audience_ → the policy does **not** apply. This is the whole point of the
   feature.
2. **Anonymous / not resolvable to a Gram principal.** A `targeted` policy
   still applies — **always**. An unauthenticated caller must never bypass a
   policy merely because no grant names them. There is no "audience" to be
   outside of when we don't know who you are.

So the applicability rule for a `targeted` policy P and caller C is:

```
applies(P, C) =
    C is anonymous / unidentified            → TRUE   (always scan)
    C is a known Gram principal in P.audience → TRUE
    C is a known Gram principal not in P.audience → FALSE
```

`everyone` policies always apply to all of the above. This makes the dangerous
direction (an unauthenticated caller slipping a policy) **impossible by
construction**, and confines the "silently not scanned" outcome to identifiable,
in-org principals an operator deliberately excluded.

"Known Gram principal" = we resolved a `user:<id>` that is a **member of this
org**. Anything short of that — no auth context, an api-key with no owning user, a
`user:<id>` that is not (or no longer) a member of this org (cross-org / stale),
or any resolution error — is **anonymous** for this rule, and every policy
applies. The bar is deliberately high: _if we cannot tie the call to a Gram user
in the org, the user is not authed and policies always apply._ This also makes
the fail-safe in §6 fall out for free: an error during resolution ⇒ anonymous ⇒
scan everything.

## 3. Default semantics — a policy-level column, not RBAC (key divergence)

If we naively flip to "a policy only applies to its audience," every existing
policy (which has no audience rows) silently stops enforcing the moment this
ships. That is a security regression and unacceptable.

**Decision:** applicability is two-tier and the coarse tier lives on the policy
row, not in RBAC:

- `risk_policies.audience_type TEXT NOT NULL DEFAULT 'everyone'`,
  `CHECK (audience_type IN ('everyone','targeted'))`.
- `everyone` (default) → policy applies to all callers; **audience grants are not
  consulted at all**. Existing policies keep behaving exactly as today.
- `targeted` → policy applies only to principals holding a
  `risk_policy:evaluate` grant for it.

Why a column and not a pure-RBAC "everyone" construct: RBAC has no clean
"everyone (authenticated + anonymous)" principal. A synthetic `role:public` would
only model the unauthenticated case. A wildcard grant (`resource_id:"*"`) means
"this _principal_ is in every policy's audience," which is a different statement
than "this _policy_ targets all principals." Encoding "everyone" as a policy
attribute is unambiguous, is the natural backward-compat default, and lets the hot
path skip principal resolution entirely for the common case (§6).

This also means we do **not** need to introduce a `public` system role in phase 1:
"everyone" already covers anonymous traffic, and a `targeted` policy
deliberately does not catch principals outside its audience. We can add `public`
later if operators ask for "applies to anonymous only."

## 4. The relation

New RBAC scope `risk_policy:evaluate` and resource kind `risk_policy`. A grant:

```jsonc
{
  "principal_urn": "role:engineer:<role_uuid>", // or user:<id>
  "scope": "risk_policy:evaluate",
  "effect": "allow", // deny is meaningless here (§11)
  "selectors": {
    "resource_kind": "risk_policy",
    "resource_id": "<policy-uuid>",
  },
}
```

Zanzibar reading: object `risk_policy:<id>`, relation `evaluate`, subject
`role:engineer` — "this policy is evaluated for this principal." The scope name
`risk_policy:evaluate` keeps the existing `family:capability` shape while reading
as `object:relation`. (A future bypass feature would be `risk_policy:bypass`,
sharing the `risk_policy` resource kind.) "Audience" stays the product noun for
the set of principals; `evaluate` is the underlying relation verb.

Selector carries **only** `resource_kind` + `resource_id` — no policy rule
content. Honors the problem statement's hard constraint.

## 5. No new engine primitive needed

The standard enforcement entry points (`Engine.Require`, `RequireAny`, `Filter`,
`FindMatched`) all short-circuit to "allow" on API-key requests, non-enterprise
accounts, and sessionless calls
([engine.go:521](../server/internal/authz/engine.go#L521)) — which is exactly the
hook traffic shape. Calling any of them from the scanner would silently grant
allow-all on most hook requests, so they are unusable here.

We sidestep that entirely. [`authz.LoadGrants`](../server/internal/authz/load.go#L12)
is a plain loader: it takes an org + principals and returns grant rows. It does
**not** consult `ShouldEnforce` and does **not** check the RBAC feature flag. The
scanner can call it directly and filter for `scope == risk_policy:evaluate`. No
new engine method, no `ShouldEnforce` carve-out to reason about.

Consequence to accept: audience evaluation is independent of the RBAC feature
flag. That is fine — grant rows only exist if an operator authored an audience,
and we gate _authoring_ (§7), not evaluation. Evaluation reading rows that
happen to exist is harmless and is in fact what we want.

## 6. Runtime flow (hook → decision)

This flow applies to **both** realtime entry points: the text scan
(`ScanForEnforcement`) and the **shadow-MCP block path**
(`LookupShadowMCPBlockingPolicy`, [scanner.go:206](../server/internal/risk/scanner.go#L206),
invoked from [hooks/impl.go:179](../server/internal/hooks/impl.go#L179)). Shadow
MCP is in scope; both lookups partition and gate by audience identically.

1. **Arrival.** A Claude / Cursor / Codex hook reaches the scanner via
   `scanClaudeForEnforcement` etc.
   ([risk_scan.go](../server/internal/hooks/risk_scan.go)), or the shadow-MCP
   pre-tool-call lookup at [hooks/impl.go:179](../server/internal/hooks/impl.go#L179).
2. **Policy load + partition.** Scanner loads enabled enforcing policies
   (`ListEnabledEnforcingPoliciesByProject`, or
   `ListEnabledShadowMCPPoliciesByProject` for the shadow path) and partitions them
   by `audience_type`. _Everyone_ policies are always applicable.
3. **Fast path.** If there are **no `targeted` policies**, skip principal
   resolution and grant loading entirely → behaves exactly like today. This
   keeps the common case free.
4. **Identify the caller** (only if ≥1 targeted policy). Try to resolve a
   `user:<id>` that is a **member of this org** (from `authCtx.UserID`, set for
   both API-key and session auth) ∪ that user's role URNs (a standalone sibling of
   `Engine.resolveRolePrincipals`
   ([engine.go:171](../server/internal/authz/engine.go#L171)) callable outside
   `PrepareContext`). No caching for now — resolve per request.
   - **If no in-org user resolves** (no auth context, api-key with no owning user,
     a `user:<id>` not a member of this org, or any error) → caller is
     **anonymous** → _every_ targeted policy applies. Skip the grant load. This
     is the §2.1 rule and the fail-safe in one branch.
5. **Audience load** (only for identified callers). `LoadGrants(ctx, db, orgID,
principals)`, filter to `risk_policy:evaluate`, collect the set of policy IDs
   the caller is in the audience of.
6. **Applicable set:**
   - anonymous caller → all `everyone` policies ∪ **all** `targeted` policies.
   - identified caller → all `everyone` policies ∪ (`targeted` ∩ caller's
     audience set).
     Run detection only for the applicable set (existing fan-out).
7. **Decision/audit.** Unchanged from today for the policies that do run.

**Fail-safe (§2.1):** any failure to _positively_ establish a known in-org
identity collapses to the anonymous branch → scan everything. Errors can never
narrow the applicable set; only a successful identity resolution can.

## 7. Authoring the audience (management API)

Per the problem statement, audience is configured on the policy page, not the
RBAC screen.

**Decision: embed in the policy payload.** Add `audience_type` and a **structured
`audience_targets`** field to the risk policy create/update payload (and result).
Targets are typed `{type: "user"|"role", id}` objects, **not** raw principal URN
strings — the URN encoding is an internal authz detail and must not leak to API
clients. The handler maps targets → principal URNs. Write the policy row and its
audience grants in one transaction → atomic, one save, one audit entry. Couples
risk→authz on the write path (acceptable; the dependency direction is allowed).
(Rejected alternative: dedicated `set/getPolicyAudience` endpoints — cleaner
separation but two round-trips and a second audit stream.)

Set semantics are **replace-the-set for this policy**: delete all
`risk_policy:evaluate` grants whose selector `resource_id == policyID`, insert the
new set. This needs a new authz writer `SetPolicyAudience(ctx, tx, orgID,
policyID, principals)` plus a query `DeleteAudienceGrantsByResource` (the existing
`UpsertPrincipalGrant` handles inserts). Note `SyncGrants` is keyed by _principal_
and is the wrong granularity — do not reuse it.

**Validation:** `audience_type = 'targeted'` requires a non-empty
`audience_targets` set. A targeted policy with no targets scans nobody-but-
anonymous (every known caller excluded) — a confusing half-state; reject it at
write time rather than persist it.

**Do not bump policy `version` on an audience-only change.** `version` drives the
offline drain's re-analysis, and offline ignores audience (§9). Audience changes
nothing about _what_ is detected, only _for whom_ at realtime — so a version bump
would force a full re-drain that reproduces identical findings. Bump version only
when detection-affecting fields change, exactly as today. (This is a deliberate
deviation from the sibling Codex plan, which proposed bumping.)

**Authoring guards:** require the org to have RBAC/roles available before
allowing role-based audiences (non-RBAC orgs have no seeded roles, so a
role-targeted policy would be unsatisfiable — see §11 C8). Gate the
audience-authoring fields behind the same enterprise/RBAC check the rest of the
access surface uses. Reject targets that are not users/roles of this org.

## 8. Protecting the RBAC surfaces (the clobbering risk + hiding)

`risk_policy:evaluate` rows live in `principal_grants` alongside real RBAC grants.
Two distinct problems follow, and the **write** one is a silent data-loss bug, not
cosmetic. (Credit: surfaced by the sibling Codex plan; verified against the code.)

### 8.1 Write-path clobbering (must land before any audience row is written)

The role editor's save path —
[`role_manager.go:332`](../server/internal/access/role_manager.go#L332)
`UpdateRole` → [`authz.SyncGrantsTx`](../server/internal/authz/grants.go#L163) →
[`DeleteRoleGrants`](../server/internal/authz/grants.go#L266) →
`DeletePrincipalGrantsByPrincipal` — **deletes every grant for the role
principal** and then re-inserts only the visible scopes from the editor payload:

```sql
DELETE FROM principal_grants WHERE organization_id = $1 AND principal_urn = $2;
```

So once a policy's audience targets `role:engineer`, _any unrelated edit to that
role in the RBAC UI silently deletes the `risk_policy:evaluate` row_ and the
audience evaporates with no error.

Fix:

- Split "delete **visible** role grants" from "delete **all** grants for a role
  principal." Introduce an `IsInternalScope(scope)` predicate (`risk_policy:*`
  today) and make the replace path in `SyncGrantsTx` delete only
  `scope NOT internal` rows. Add a `DeleteRoleGrantsExceptInternal`-style query.
- **Role _deletion_** (`DeleteRole`,
  [`role_manager.go:499`](../server/internal/access/role_manager.go#L499)) must
  still delete _all_ grants for the principal, internal ones included — the role
  is going away, so its audience memberships should too.
- This protection **must merge before** the authoring API (PR5) can write any
  audience row, or we ship a window where role edits eat audiences.

### 8.2 Hiding from the RBAC read/enum surfaces (cosmetic but required)

- **Do not** add `risk_policy:evaluate` to the `scope` enum or `risk_policy` to
  the `resource_kind` enum in
  [`server/design/access/design.go`](../server/design/access/design.go). Those
  enums drive the dashboard grants UI; leaving the scope out keeps it hidden.
- **Do** add the scope/kind to the `authz` package internals
  (`ResourceKindForScope`, a selector validator) since those run on every grant
  write for validation.
- **Filter** internal scopes out of the role/grant read paths that feed the UI:
  `GrantsForRole` ([role_manager.go:1007](../server/internal/access/role_manager.go#L1007)),
  `ListPrincipalGrantsByOrg`, `ListGrants`, and role-grant payload conversion.
- **Exclude** it from `allScopeGrants()` (superadmin impersonation) and from
  `SystemRoleGrants` seeding — it is a relation, not a capability, and nobody
  should inherit it by default.

## 9. Offline drain — deferred (decided)

The background drain workflow ([risk/impl.go:163] region) scans all messages and
is the auditor's full view. It has no live caller principal. **Decision: the drain
ignores audience entirely and keeps recording findings for all policies, as
today. Audience-aware offline evaluation is out of scope and deferred.** Realtime
_enforcement_ respects audience; offline _findings_ stay complete. This divergence
is intentional and not revisited in this work.

## 10. Work breakdown (small, safe, reviewable PRs)

Ordered by dependency. PR1–PR4 are behavior-preserving or inert; nothing
user-visible changes until PR5 (authoring) lets a policy go `targeted`.

**PR1 — migration (own PR, per CLAUDE.md).** Add `audience_type` column
(`DEFAULT 'everyone'`, CHECK constraint) plus a partial
`(project_id, audience_type)` index for project-scoped audience lookups. `mise
db:diff`, commit `schema.sql` + migration + `atlas.sum` + sqlc regen. No app
logic. Expand-only.

**PR2 — authz foundation (pure additions, no callers).** Add
`ScopeRiskPolicyEvaluate`, `ResourceKindRiskPolicy`, the `ResourceKindForScope`
branch, the selector validator (only `{resource_kind, resource_id}`), an
`IsInternalScope` predicate, a standalone in-org principal resolver, and the
audience read/write helpers (`SetPolicyAudience`, `ListAudienceGrantsByResource`,
`DeleteAudienceGrantsByResource`). Unit tests for resolution + selector
validation. Nothing calls any of it yet.

**PR3 — access-surface protection (behavior-preserving) — §8.** Scope the role
replace path (`SyncGrantsTx` / `DeleteRoleGrants`) to delete only non-internal
scopes; keep role _deletion_ deleting everything; filter internal scopes from the
role/grant read paths and enums. Lands **before** any audience row can exist.
Tests: a visible role edit preserves a (manually inserted) internal grant; role
delete still removes it; internal scopes never appear in role views.

**PR4 — scanner enforcement (inert) — §6.** Thread the caller principal into
`ScanForEnforcement` **and `LookupShadowMCPBlockingPolicy`**; resolve it in the
three hook sites **and the shadow-MCP lookup site
([hooks/impl.go:179](../server/internal/hooks/impl.go#L179)) — first verify the
principal is available there (§13.1)**; implement partition +
audience-intersection + anonymous/fail-safe in both paths. Every existing policy
is `everyone`, so the targeted set is empty and behavior is identical. Update
hook + scanner tests.

**PR5 — authoring API + audit + delete cleanup — §7.** Add `audience_type` +
structured `audience_targets` to the risk-policy create/update payload + result;
write grants in the policy transaction; validate non-empty targets when
`targeted`; reject non-org targets; **do not bump version on audience-only
change**; new audit action `risk_policy:set_audience` (or fold into the update
snapshot); delete a policy's audience grants on delete (§11 C9). Integration test:
two callers (in vs out of audience) hitting the same targeted policy — one
scanned, one not; targeted-with-empty-targets rejected.

**PR6 — dashboard UI (out of scope here; follow-up).** Audience section on the
policy page, structured targets, never surfaced in Roles & Permissions.

Shared-infra note: the standalone principal resolver and the "evaluate grant rows
outside `ShouldEnforce`" approach are reusable by a future bypass feature. Build
the resolver generically.

## 11. Caveats / things to watch

- **C1 — default = everyone.** Non-negotiable for backward compat and fail-safe.
  Enforced by the column default (§3).
- **C2 — grant table repurposed as a relation store.** `risk_policy:evaluate` is
  not a capability; `deny` effect is meaningless; deny-wins evaluation does not
  apply. We only ever write `allow`. Isolate it from RBAC UI/seeding (§8). This
  is a conceptual smell we accept because the team chose one mental model.
- **C3 — evaluation ignores the RBAC feature flag** (uses `LoadGrants` directly,
  §5). Acceptable; authoring is gated instead.
- **C4 — fail-safe = anonymous branch = scan everything** (§2.1, §6). Any failure
  to positively resolve a known in-org identity scans all policies. Errors can
  only ever increase scanning, never reduce it.
- **C5 — realtime vs offline divergence** (§9). Decided: offline drain ignores
  audience and stays complete. Not revisited here.
- **C6 — targeted policies skip _identifiable, in-audience-excluded_ callers
  only.** Anonymous / unidentifiable callers are **always** scanned (§2.1), so the
  dangerous bypass direction is impossible by construction. The remaining footgun
  is narrow — an operator excluding a known principal they didn't mean to — and
  the UI (PR6) must surface the audience clearly. **The implementation must not
  collapse "anonymous" and "authenticated-but-not-in-audience" into one
  not-matched branch; they resolve oppositely.**
- **C7 — hot-path cost.** Only paid when ≥1 targeted policy exists (the fast
  path in §6 step 3 skips resolution otherwise). No caching for now — resolve
  per request; revisit only if profiling shows it matters.
- **C8 — non-RBAC orgs have no roles.** Role-based audiences are effectively an
  RBAC/enterprise feature; user-based audiences could work more broadly. Gate
  authoring accordingly (§7).
- **C9 — soft-orphan grants.** Soft-deleting a policy leaves audience grants;
  clean them in the delete handler (PR5). Harmless if missed (no policy ⇒ never
  in the applicable set), but keeps the table tidy.
- **C10 — scope family settled.** `risk_policy:evaluate` now, `risk_policy:bypass`
  reserved later; both share the `risk_policy` resource kind.
- **C11 — role-edit clobbering (silent data loss).** The role-grant replace path
  deletes _all_ grants for a principal; without §8.1 a routine role edit destroys
  the audience. The fix must precede any audience write (PR3 before PR5). This is
  the highest-severity correctness item in the plan.
- **C12 — no version bump on audience change** (§7). Avoids a pointless full
  re-drain; offline ignores audience anyway.

## 12. Resolved decisions (from review)

- **Scope name:** `risk_policy:evaluate` (object `risk_policy`, relation
  `evaluate`). Sibling `risk_policy:bypass` reserved for the future feature.
- **Offline drain:** deferred — drain ignores audience, records all findings (§9).
- **Authoring shape:** embed `audience_type` + `audience_principals` in the policy
  payload, written in the policy transaction (§7).
- **Shadow MCP:** in scope — `LookupShadowMCPBlockingPolicy` gets the same
  principal threading and audience gating as `ScanForEnforcement` (§6, PR3).
- **Anonymous rule:** if a call cannot be tied to a `user:<id>` that is a member
  of this org, the caller is anonymous and **every** policy applies — including
  api-key-without-owning-user and cross-org/stale user IDs (§2.1).
- **Caching:** none for now; resolve per request (§6 step 4, C7).
- **API shape:** structured `audience_targets` (`{type, id}`), not raw URN
  strings; `targeted` requires non-empty targets (§7).
- **No version bump** on audience-only changes (§7, C12).
- **Role-edit clobbering** is fixed by §8.1 and ordered before any audience write
  (PR3 ≪ PR5, C11). Merged from the sibling Codex review.

## 13. Remaining things to verify during implementation

1. **Principal availability at the shadow-MCP lookup site**
   ([hooks/impl.go:179](../server/internal/hooks/impl.go#L179)): confirm `authCtx`
   / owning user is reachable there; if not, scope the smallest plumbing change to
   make it available.
2. **`user:<id>` org-membership check:** confirm the cheapest way to assert "this
   resolved user is a member of this org" so the §2.1 anonymous bar is enforced
   correctly (a stale/cross-org user must collapse to anonymous, not silently
   match an empty audience).

```

```
