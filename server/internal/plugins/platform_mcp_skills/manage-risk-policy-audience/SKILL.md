---
name: manage-risk-policy-audience
description: Inspect and safely change who a risk policy targets in an explicit Speakeasy AI Control Plane project, with confirmed administrator audience changes and clear refusals for unavailable self-exclusion.
---

# Manage a risk policy audience

Use the executing client's authenticated Platform MCP connection. Installing this skill grants no authority. Live membership, organization-admin permission, explicit-project authorization, and the risk-mutation rollout remain authoritative. Never request credentials or use another client's identity.

## Inspect and confirm the exact target

1. Call `list_projects` through this client's connection. Ask the user to choose the exact project slug. If discovery is incomplete or the requested project is unavailable, stop; never substitute Default or another project.
2. Call `list_risk_policies` for that project and ask the user to select the exact policy. Names are not unique: retain the returned policy ID rather than guessing from a name. Follow pagination for discovery, or accept an exact user-supplied policy ID and verify it directly.
3. Call `get_risk_policy` for the exact project and policy. Inspect its current `audience`, compatibility, and opaque `version`. Keep principal URNs, policy IDs, versions, and idempotency keys as internal tool arguments rather than displaying them as user instructions. A policy audience is a set of positive user/role grants, not a list of exceptions or proof of effective membership.

## Remove the authenticated requester

For “remove me,” explain that self-removal and effective exclusion are unavailable pending organization-scoped coordination of audience grants. `remove_self_from_risk_policy` remains discoverable to external organization administrators but always refuses before database transactions or receipt replay. Do not claim that deleting a positive user grant proves the requester is no longer covered.

Stop without writing. Do not bypass this refusal with `change_risk_policy_audience`, general audience replacement, policy disablement, role or membership changes, a risk exclusion, or reconstruction of Everyone as today's member list. Never infer a human from managed-assistant attribution. A separately requested administrator change to direct positive grants is a different outcome, not a workaround for self-exclusion.

## Add or remove direct audience grants

For a separately requested incremental administrator change, use `change_risk_policy_audience` through an external OAuth organization-admin connection. Managed assistants cannot discover or invoke this tool, and their policy reads omit exact audience identities.

1. Select exact organization user or role principal URNs from trusted administrator selections, not guessed names or emails. Native directory groups are not supported; do not treat a directory group as a role or change directory membership.
2. Explain the exact additions and removals and obtain explicit confirmation. Removing a direct user grant does not remove role-derived coverage. Removing a role grant changes that role's direct policy grant, not its membership. Never claim the removed user is unaffected by the policy: other roles or broader grants may still cover them.
3. Refresh `get_risk_policy` for the exact target immediately before writing. If the audience changed, obtain confirmation again. Call `change_risk_policy_audience` with the exact `project_slug`, `policy_id`, fresh `expected_version`, stable `idempotency_key`, `confirmed: true`, and both `add_principals` and `remove_principals` arrays. Each list is bounded to 100 user/role URNs; the resulting targeted audience must contain 1–100 principals. Use an empty array for the unchanged side; at least one array must be nonempty. The tool atomically preserves all audience entries outside the delta and unrelated policy settings.
4. Stop on Everyone audiences, last-principal removal, unsupported principals, duplicate or overlapping deltas, stale versions, and conflicts with current direct grants. This tool does not create an Everyone-except-one audience or effective exclusion. Never use an audience delta to bypass a self-removal refusal. Do not automatically fall back to replacement, policy disablement, or risk exclusion.

## Replace an audience only when explicitly requested

For a separately requested administrator audience change through an external OAuth connection, use `update_risk_policy` only for a complete replacement, not an incremental request. Managed-assistant reads omit exact audience identities, and managed assistants cannot replace audiences. Read the exact policy first and obtain explicit confirmation of the complete replacement—not merely one addition or deletion. Omit every unrelated patch field. Use `patch.audience` with `type`, `principal_urns`, and `confirm: true`, plus the usual project, policy, expected version, and idempotency key.

- `targeted` requires a nonempty list of valid organization user or role principal URNs (at most 100 entries). Preserve every principal outside the explicitly confirmed change. Use exact trusted selections; never guess identifiers or use names as identities.
- `everyone` requires an empty `principal_urns` array. This broadens the policy to everyone and needs explicit confirmation of that effect. It is never a fallback for a failed targeted update.
- Removing a direct user grant does not remove role-derived coverage. There is no negative-grant representation for Everyone-except-one or role exceptions. Do not promise effective exclusion from a raw replacement.

## Verify and report

If another grant change prevents the write, report that no change was made and retry only after a fresh policy read and renewed confirmation.

After any write, call `get_risk_policy` again for the same project and policy. Verify the intended audience and preservation of unrelated policy fields. A receipt replay proves a historical commit, not current state. On a version conflict, read again and obtain renewed confirmation before using a new key; never retry with a different target.

Report only the supported outcome: the committed audience change and whether the fresh read confirms it. Do not claim a permanent exemption, retroactive removal of findings, or immunity from other policies or future audience changes. If verification fails, distinguish the committed result from incomplete current-state verification.
