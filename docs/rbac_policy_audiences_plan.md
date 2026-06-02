# RFC Plan: RBAC-backed risk policy audiences

## Status

Draft plan for implementing `docs/rbac_for_policies.md`.

## Summary

Risk policies should gain an audience: a policy is evaluated only when the current request or stored message matches at least one audience principal for that policy. The storage should reuse Gram's RBAC grant model, but this must be treated as an internal product relation, not as a normal user-facing RBAC permission.

Recommended shape:

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

The `principal_urn` is the audience subject. The selector links the relation to exactly one risk policy. Selectors must not encode sources, rules, actions, detector categories, or bypass semantics.

## Important design corrections

The source sketch is directionally right, but these pieces need tightening before implementation:

1. `risk_policy:evaluate` must be hidden from the generic Roles & Permissions screen. If it is added to `ListScopes`, generated access enums, and dashboard scope pickers like a normal scope, operators will be able to edit policy-audience grants in the wrong product surface.
2. The existing `authz.SyncGrantsTx` path deletes all grants for a role before inserting the visible role grants. If policy-audience rows live in `principal_grants`, a normal role edit can accidentally erase policy audiences. This must be fixed before writing audience rows to role principals.
3. "If the user has grants" is the wrong runtime condition. The evaluator should always include a synthetic `role:everyone` principal, then add `user:<gram_user_id>` and assigned role principals when identity is known.
4. Existing policies currently apply to everyone. Shipping positive-only audiences without a compatibility path would silently make old policies stop applying. Existing and newly-created policies should default to `everyone` until the policy UI can change that audience.
5. Realtime hooks and async chat-message analysis need separate integration work. Hook requests have request auth context; batch analysis has message rows and may not always have a Gram `user_id`.

## Scope naming

Use `risk_policy:evaluate`.

Rationale:

- It keeps the resource family specific (`risk_policy`, not generic `policy`).
- It reads as "this subject is in the set for which this policy is evaluated."
- It avoids `apply`, which can be confused with mutating or deploying a policy.
- It maps cleanly to a Zanzibar-style object relation: `risk_policy:<id>#evaluate@principal`.

Do not add scope expansion for this scope. `org:admin` should not automatically mean "all policies apply to admins"; audience is not an authorization privilege.

## Audience model

Supported audience targets:

- `role:everyone`: synthetic subject included for every request/message in the organization, including unknown users.
- `user:<gram_user_id>`: direct user audience.
- `role:<slug>` legacy principal while the role-principal migration still requires dual-read.
- `role:global:<uuid>` / `role:organization:<uuid>` canonical role principals.

The audience API should expose friendly target objects, not raw `RoleGrant`:

```json
{
  "targets": [
    { "type": "everyone" },
    { "type": "role", "role_id": "...", "role_slug": "org-engineering" },
    { "type": "user", "user_id": "user_..." }
  ]
}
```

Internally, role targets should write both canonical and legacy role principals for now, matching `authz.RolePrincipals`.

Do not support wildcard policy selectors in the first version. `resource_id` for `risk_policy:evaluate` should be a concrete policy UUID. "This policy applies to everyone" is represented by `principal_urn = role:everyone` and `resource_id = <policy_uuid>`, not by `resource_id = "*"`.

## Server design

### Authz package

Add the internal relation primitives:

- `server/internal/authz/scopes.go`: add `ScopeRiskPolicyEvaluate = "risk_policy:evaluate"` with no expansions.
- `server/internal/authz/selector.go`: add `risk_policy` resource kind derivation and selector validation with no extra keys.
- `server/internal/authz/checks.go`: add `RiskPolicyEvaluateCheck(policyID string)`.

Add a direct grant-matching helper that does not call `ShouldEnforce`:

```go
FindMatchedGrants(ctx, orgID string, principals []urn.Principal, checks []authz.Check) ([]bool, error)
```

Reason: hook traffic is commonly API-key authenticated, and current `Require`, `Filter`, and `FindMatched` intentionally return allow-all when `ShouldEnforce` is false for API keys. Policy audience is not an authorization gate; it is an explicit relation lookup, so it must not use that enforcement shortcut.

### Preserve hidden grants during role edits

Before adding audience rows, split the current role sync behavior:

- Generic role update and system-role seeding should delete/replace only public role-management scopes.
- Role deletion should still delete every grant for the role principal.
- Access API responses (`ListGrants`, `ListRoles`, `GetRole`) should filter out internal audience scopes.

This is required because the access role editor currently uses replacement semantics for all grants attached to a role principal.

### Risk API

Add policy-audience endpoints on the risk service rather than the access service:

- `GET /rpc/risk.policies.audience.get?id=<policy_id>`
- `PUT /rpc/risk.policies.audience.update`

Both should require `org:admin`, verify the policy belongs to the active project, write only `allow` audience grants, bump the policy version, audit the change, and signal risk analysis for the project.

Keep `createRiskPolicy` defaulting to `everyone` by inserting the `role:everyone` audience row in the same transaction as the policy. Existing policies need an idempotent backfill/runbook that grants `role:everyone` for each non-deleted policy before runtime filtering is enabled.

### Realtime hook enforcement

Thread an audience context into:

- `server/internal/hooks/risk_scan.go`
- shadow-MCP lookup in `server/internal/hooks/impl.go`
- Cursor/Codex/Claude surfaces that already resolve `authCtx.UserID` or metadata user IDs.

The runtime principal resolver should return:

1. `role:everyone`
2. `user:<gram_user_id>` when a Gram user is known
3. all assigned role principals for that user in the organization

Then `risk.Scanner.ScanForEnforcement` and `LookupShadowMCPBlockingPolicy` should filter enabled policies to the subset whose `risk_policy:evaluate` grant matches the resolved principal set before scanning.

When no user is known, only `role:everyone` policies apply.

### Async risk analysis

Batch analysis must not keep applying all policies globally. The existing coordinator fans out every fetched message to every active policy, so add audience filtering before scanning each policy batch.

Preferred MVP:

- Extend `GetMessageContentBatch` to include `user_id`.
- In `AnalyzeBatch`, resolve principals per distinct `user_id` plus `role:everyone`.
- Scan only messages whose principals match the policy audience.
- Still write an empty `risk_results` row for non-applicable messages only if required to preserve the existing "row means processed for this policy/version" accounting.

Longer term, the result ledger should distinguish `not_applicable` from "scanned and no finding." Today `risk_results` has only `found=false`, which is overloaded. If product metrics need exact "messages scanned" semantics, add an explicit status/skipped reason in a migration-only PR.

## Reviewable PR split

1. **Authz internal relation and sync safety**
   - Add hidden `risk_policy:evaluate` primitives.
   - Add direct grant matching that bypasses `ShouldEnforce`.
   - Split role-grant replacement from all-grant deletion.
   - Filter internal scopes from access API responses.
   - Tests: selector validation, matcher behavior under API-key/non-enterprise/no-session contexts, role update preserving internal grants.

2. **Risk audience management API**
   - Add get/update audience endpoints and generated SDK.
   - Store audience rows in `principal_grants`.
   - Default new policies to `role:everyone`.
   - Bump policy version and audit on audience changes.
   - Tests: create default audience, update role/user/everyone audience, policy ownership checks, audit entry.

3. **Compatibility backfill**
   - Add an idempotent server task/runbook to grant `role:everyone` to every existing non-deleted policy.
   - Run against local/staging first; production execution should be operator-run, not an agent-run migration.
   - Verification query should report policies missing an audience row.

4. **Realtime enforcement integration**
   - Resolve audience principals in hook flows.
   - Filter policies before realtime scanning and shadow-MCP blocking lookup.
   - Tests: everyone applies, role applies, wrong role skips, unknown user only gets everyone, API-key requests do not accidentally match everything.

5. **Async analysis integration**
   - Filter policy batches by message user audience.
   - Decide whether non-applicable messages get ledger rows or whether a schema change is needed first.
   - Tests: role-targeted policy creates findings only for matching users; non-matching messages do not produce findings or retry indefinitely.

6. **Dashboard policy-page UI**
   - Add the audience section to Risk Policies, backed by the risk audience API.
   - Do not expose the internal scope in the access role editor.
   - Tests: audience target selection and update payload shaping.

## Caveats and watch-outs

- Role assignment lag matters. Audience evaluation uses local role assignment rows, which can lag WorkOS until sync catches up. This matches current RBAC behavior but should be called out in UI/help text.
- Direct user audiences require a Gram user ID. Hook metadata with only an email should resolve to a Gram user when possible; otherwise only `everyone` applies.
- `role:everyone` is intentionally synthetic. Do not add it to WorkOS or make it assignable.
- Audience changes should be treated like policy changes for analysis freshness. Bump the policy version and signal the coordinator.
- Deny grants are out of scope. The audience API should reject or never write `effect=deny` for `risk_policy:evaluate`.
- Avoid adding `risk_policy:evaluate` to `ListScopes`, `RoleGrantModel`, the dashboard `Scope` union, or public full-access grant lists unless the product decision changes.
- Deleting a policy should clean up its audience grants for tidiness, even though orphaned selector rows are harmless.

## Verification commands

- `mise gen:sqlc-server` after SQL query changes.
- `mise run gen:goa-server` after Goa risk API changes.
- `mise run gen:sdk` after generated API changes.
- `go test ./server/internal/authz ./server/internal/access ./server/internal/risk ./server/internal/hooks ./server/internal/background/activities/risk_analysis`
- `mise lint:server`

## Unresolved questions

1. Should `role:everyone` include unauthenticated/unknown-user hook traffic, or only authenticated organization members? This plan recommends including unknown users so existing policy behavior can be preserved.
2. Should async non-applicable messages be recorded as `found=false`, or should we first add an explicit `risk_results.status = scanned|not_applicable|error`? The current ledger cannot express that distinction.
3. Do we need direct user audiences in the first UI/API release, or are role plus everyone enough for the initial product need?
