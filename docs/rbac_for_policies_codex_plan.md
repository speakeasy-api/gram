# RFC: Risk Policy Audiences via RBAC Relations

| Field   | Value                                                         |
| ------- | ------------------------------------------------------------- |
| Status  | Draft                                                         |
| Author  | Codex                                                         |
| Created | 2026-05-29                                                    |
| Related | `docs/rbac_for_policies.md`, `docs/rbac_for_policies_plan.md` |
| Scope   | Server plan; dashboard noted only where it affects API shape  |

## 1. Summary

Risk policies need an audience: the set of users or roles for whom a policy is evaluated. The right design is a hybrid:

- Use a policy-level `audience_mode` column for the coarse default: `everyone` or `restricted`.
- Use RBAC `principal_grants` only for the restricted relation: `principal -> risk_policy:evaluate -> policy`.
- Keep the new relation hidden from the generic RBAC role editor. The policy page owns audience authoring.
- Treat unknown/unresolvable callers as fail-safe: restricted policies still apply when Gram cannot prove the caller is a known in-org user.

This preserves today's behavior for existing policies while giving operators positive targeting for known users and roles.

## 2. Comparison With `rbac_for_policies_plan.md`

I agree with the existing plan's core design more than my initial synthetic-`role:everyone` sketch.

### Where the existing plan is stronger

The `audience_mode` column is the right default mechanism. Encoding "everyone" as a grant to a synthetic principal is tempting, but it blurs two different ideas:

- "This policy targets everyone."
- "This principal is included in this policy."

Those are not the same. A policy-level `audience_mode = 'everyone'` is clearer, backward compatible, and lets the hot path skip principal and grant loading when no restricted policies exist.

The existing plan's anonymous/unidentified rule is also stronger. Unknown callers should not get a free pass just because they cannot match a user or role grant. If identity resolution fails, scan everything.

The existing plan is also right that audience lookup can use `authz.LoadGrants` directly instead of adding a new `Engine.CheckGrant` primitive. `LoadGrants` does not consult `ShouldEnforce`, so it avoids the API-key allow-all trap without expanding the public engine surface.

### Where I would amend the existing plan

The biggest missing operational risk is hidden-grant clobbering. `authz.SyncGrantsTx` currently deletes all grants for a role principal before replacing visible RBAC grants. If policy audiences are stored in `principal_grants`, editing a role in the generic access UI can delete `risk_policy:evaluate` rows unless the delete/replace path is scoped to public grants.

That fix should land before any policy-audience rows are written.

I would also be explicit that `risk_policy:evaluate` is an internal relation, not a grantable product capability. It should not appear in `server/design/access/design.go`, `ListScopes`, generated access role enums, `allScopeGrants`, `SystemRoleGrants`, or the dashboard scope picker.

### Best approach

Use the existing plan as the base, with one required addition: preserve/filter internal audience grants in the access role-management paths. Do not use my earlier pure-RBAC `role:everyone` approach.

## 3. Data Model

Add an expand-only migration:

```sql
ALTER TABLE risk_policies
ADD COLUMN audience_mode text NOT NULL DEFAULT 'everyone';

ALTER TABLE risk_policies
ADD CONSTRAINT risk_policies_audience_mode_check
CHECK (audience_mode IN ('everyone', 'restricted'));
```

Do not add a separate audience table in the first version. Restricted audience membership is stored in existing `principal_grants` rows:

```json
{
  "principal_urn": "role:organization:<role_uuid>",
  "scope": "risk_policy:evaluate",
  "effect": "allow",
  "selectors": {
    "resource_kind": "risk_policy",
    "resource_id": "<policy_uuid>"
  }
}
```

Rules:

- `audience_mode = 'everyone'`: policy applies to all callers; ignore audience grants.
- `audience_mode = 'restricted'`: policy applies to known in-org principals with a matching grant.
- Anonymous/unidentified callers are fail-safe and still get restricted policies.
- Deny grants are not supported for this relation.
- Wildcard `resource_id = '*'` is not supported for this relation in v1.

## 4. Scope And Selector

Add internal authz primitives:

- `ScopeRiskPolicyEvaluate = "risk_policy:evaluate"`
- resource kind `risk_policy`
- `authz.RiskPolicyEvaluateCheck(policyID string)`

Selector validation must allow only:

```json
{ "resource_kind": "risk_policy", "resource_id": "<policy_uuid>" }
```

No extra selector keys. No detector source, rule ID, action, category, bypass flag, or business logic belongs in selectors.

Do not add this scope to public access API enums. It is an internal relation used by the risk service.

## 5. Principal Resolution

Audience resolution must distinguish "known in-org principal" from "unknown."

Known in-org caller:

- `user:<authCtx.UserID>` exists.
- The user has an active relationship with `authCtx.ActiveOrganizationID`.
- Role principals can be resolved from local role assignments.

Unknown caller:

- no auth context
- no user ID
- user ID is stale or not a member of the organization
- role lookup fails
- API key owner cannot be resolved
- any other identity-resolution error

Applicability for a restricted policy:

```text
unknown caller -> applies
known caller with matching risk_policy:evaluate grant -> applies
known caller without matching grant -> does not apply
```

This is intentionally fail-safe. Errors can increase scanning, never reduce it.

## 6. Runtime Flow

For `ScanForEnforcement` and `LookupShadowMCPBlockingPolicy`:

1. Load enabled policies for the project using the existing risk queries.
2. Partition policies by `audience_mode`.
3. If no restricted policies exist, scan exactly as today.
4. If restricted policies exist, resolve the caller as known or unknown.
5. Unknown caller: include every restricted policy.
6. Known caller: call `authz.LoadGrants(ctx, db, orgID, principals)`, filter to `risk_policy:evaluate`, and intersect with restricted policy IDs.
7. Scan only the applicable set.

This keeps the default hot path cheap and avoids using `Require`, `Filter`, or `FindMatched`, all of which intentionally short-circuit for API-key and non-enforced RBAC contexts.

## 7. Authoring API

Authoring belongs in the risk policy API, not the access API.

Recommended API shape:

- Add `audience_mode` to create/update risk policy payloads and results.
- Add `audience_principals` or a structured `audience_targets` field to create/update payloads.
- Write the policy row and audience grants in one transaction.
- Bump policy version when audience changes.
- Emit a risk policy update audit snapshot that includes audience mode and target summary.
- Signal the risk coordinator when an enabled policy's audience changes.

Set semantics should be replace-by-policy:

1. Delete `risk_policy:evaluate` grants for `resource_id = <policy_id>`.
2. Insert the new allow grants for selected users/roles.

Do not use `authz.SyncGrants`, because it is role-principal scoped and is the wrong granularity.

## 8. Protect Access RBAC Surfaces

Before authoring policy audience grants, change access grant replacement so it cannot delete hidden audience rows.

Required work:

- Split "delete visible role grants" from "delete all grants for role principal."
- Generic role update and system-role sync should replace only public RBAC scopes.
- Role deletion should still delete all grants for that role principal.
- `ListRoles`, `GetRole`, `ListGrants`, and role grant payload conversion should filter out `risk_policy:evaluate`.
- `allScopeGrants` and `SystemRoleGrants` should not include `risk_policy:evaluate`.

This is the main safety amendment to the existing plan.

## 9. Offline Analysis

I agree with `rbac_for_policies_plan.md`: defer audience-aware offline drain in the first implementation.

Reasons:

- The current background workflow is a complete audit pass over stored messages.
- Message rows may not always carry a resolvable Gram user.
- The existing `risk_results` ledger cannot distinguish "not applicable" from "scanned and no finding."

Realtime enforcement should respect audience first. Offline findings can stay complete until product explicitly wants audience-filtered reporting. If that changes, add a status column such as `scanned`, `not_applicable`, `error` in a migration-only PR before changing drain semantics.

## 10. PR Split

### PR 1: Migration only

- Add `risk_policies.audience_mode`.
- Regenerate SQLc.
- No app behavior changes.
- This follows the repo rule that migrations ship separately.

Verification:

- `mise db:diff <name>`
- `mise gen:sqlc-server`
- migration lint, if available

### PR 2: Internal authz relation and access-surface protection

- Add internal `risk_policy:evaluate` scope and selector validation.
- Add risk audience read/write helpers:
  - `SetRiskPolicyAudience`
  - `ListRiskPolicyAudience`
  - `DeleteRiskPolicyAudience`
- Add grant deletion scoped to visible/public role scopes.
- Filter internal scopes from access responses.

Verification:

- authz selector tests
- access role update test proving hidden audience grants survive visible role edits
- role delete test proving all role grants are still removed

### PR 3: Risk API authoring

- Add `audience_mode` and audience targets to risk policy create/update/result types.
- Write audience grants transactionally with policy create/update.
- Keep new policies defaulting to `audience_mode = 'everyone'`.
- Delete audience grants on policy delete.
- Audit audience changes.

Verification:

- create policy defaults to everyone
- restricted policy requires non-empty audience targets
- update replaces audience set
- delete cleans audience grants
- unauthorized/non-org role targets are rejected

### PR 4: Realtime enforcement

- Thread caller identity into `ScanForEnforcement` and `LookupShadowMCPBlockingPolicy`.
- Add known/unknown principal resolver.
- Apply audience filtering before scanning.
- Keep behavior unchanged for everyone-mode policies.

Verification:

- everyone policy applies to all callers
- restricted policy applies to matching role/user
- restricted policy skips known non-matching user
- restricted policy still applies to unknown/unresolvable caller
- API-key hook does not accidentally allow or skip everything because of `ShouldEnforce`
- shadow-MCP blocking lookup follows same audience rules

### PR 5: Dashboard policy-page UI

- Add audience controls to the Risk Policies page.
- Do not expose `risk_policy:evaluate` in Roles & Permissions.
- Use structured targets rather than raw principal URNs.

Verification:

- typecheck dashboard
- component tests for payload shaping, if existing patterns support it

## 11. Caveats

- Audience is positive-only. Do not add bypass or deny semantics.
- Audience grants are relation rows, not permissions. Keep them hidden from generic RBAC UX.
- Unknown callers scan everything. This is deliberate.
- Role membership is local source of truth and may lag WorkOS sync, matching current RBAC behavior.
- Direct user audiences require a Gram user ID. Email-only metadata should resolve to a user when possible; failure means unknown caller.
- Audience changes should bump policy version so policy status and future rescans do not mix old and new semantics.

## 12. Recommendation

Adopt `docs/rbac_for_policies_plan.md` as the base plan. Add the hidden-grant preservation work from this document to the early PRs. Avoid the pure-RBAC `role:everyone` model.

The strongest design is:

```text
risk_policies.audience_mode = everyone|restricted
restricted audience rows = principal_grants(scope=risk_policy:evaluate, selector=risk_policy:<id>)
unknown identity = scan all applicable policies
generic RBAC UI = never shows or deletes audience rows
```

That gives the product the RBAC-backed relation it wants without regressing existing policy enforcement or leaking policy-audience business logic into selectors.
