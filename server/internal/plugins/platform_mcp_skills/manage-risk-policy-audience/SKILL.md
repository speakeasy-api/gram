---
name: manage-risk-policy-audience
description: Inspect and safely change who a risk policy targets in an explicit Speakeasy AI Control Plane project, including removing the authenticated requester when the policy supports direct-user removal.
---

# Manage a risk policy audience

Use the executing client's authenticated Platform MCP connection. Installing this skill grants no authority. Live membership, organization-admin permission, explicit-project authorization, and the risk-mutation rollout remain authoritative. Never request credentials or use another client's identity.

## Inspect and confirm the exact target

1. Call `list_projects` through this client's connection. Ask the user to choose the exact project slug. If discovery is incomplete or the requested project is unavailable, stop; never substitute Default or another project.
2. Call `list_risk_policies` for that project and ask the user to select the exact policy. Names are not unique: retain the returned policy ID rather than guessing from a name. Follow pagination for discovery, or accept an exact user-supplied policy ID and verify it directly.
3. Call `get_risk_policy` for the exact project and policy. Inspect its current `audience`, compatibility, and opaque `version`. Keep principal URNs, policy IDs, versions, and idempotency keys as internal tool arguments rather than displaying them as user instructions. A policy audience is a set of positive user/role grants, not a list of exceptions or proof of effective membership.

## Remove the authenticated requester

4. For “remove me,” use `remove_self_from_risk_policy` only through the requester's external OAuth connection. The server derives the user from authentication; never supply a user ID or infer identity from an email, a display name, or a managed assistant's attribution.
5. Explain and confirm the exact project, policy, and removal before writing. This operation supports a targeted, direct-user audience only, with at least one other user remaining. Everyone, role-containing audiences, unsupported grant scopes, and last-user removal are not self-exclusion operations. On a refusal, stop: do not disable the policy, empty its audience, remove a role, edit membership, create a risk exclusion, or reconstruct Everyone as today's member list.
6. Immediately before the write, refresh `get_risk_policy`. If the target or audience changed, explain the new state and obtain confirmation again. Call `remove_self_from_risk_policy` with the exact `project_slug`, `policy_id`, fresh `expected_version`, `confirmed: true`, and a stable `idempotency_key` for retries of this exact request. Never use general audience replacement to bypass a self-removal refusal.

## Replace an audience only when explicitly requested

For a separately requested administrator audience change through an external OAuth connection, use `update_risk_policy` instead. Managed-assistant reads omit exact audience identities, and managed assistants cannot replace audiences. Read the exact policy first and obtain explicit confirmation of the complete replacement—not merely one addition or deletion. Omit every unrelated patch field. Use `patch.audience` with `type`, `principal_urns`, and `confirm: true`, plus the usual project, policy, expected version, and idempotency key.

- `targeted` requires a nonempty list of valid organization user or role principal URNs (at most 100 entries). Preserve every principal outside the explicitly confirmed change. Use exact trusted selections; never guess identifiers or use names as identities.
- `everyone` requires an empty `principal_urns` array. This broadens the policy to everyone and needs explicit confirmation of that effect. It is never a fallback for a failed targeted update.
- Removing a direct user grant does not remove role-derived coverage. There is no negative-grant representation for Everyone-except-one or role exceptions. Do not promise effective exclusion from a raw replacement.

## Verify and report

If another grant change prevents the write, report that no change was made and retry only after a fresh policy read and renewed confirmation.

After either write, call `get_risk_policy` again for the same project and policy. Verify the intended audience and preservation of unrelated policy fields. A receipt replay proves a historical commit, not current state. On a version conflict, read again and obtain renewed confirmation before using a new key; never retry with a different target.

Report only the supported outcome: the committed audience change and whether the fresh read confirms it. Do not claim a permanent exemption, retroactive removal of findings, or immunity from other policies or future audience changes. If verification fails, distinguish the committed result from incomplete current-state verification.
