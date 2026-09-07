---
name: manage-mcp-access
description: Create or update a custom AICP MCP access role, assign it to an explicitly selected member, and verify the member's effective access.
---

# Manage MCP access

Use this workflow only through the authenticated Speakeasy AI Control Plane (AICP) Platform MCP. It mirrors the guarded dashboard workflow for giving one explicitly selected organization member access to one explicitly selected configured MCP through a custom role. Installing the package grants no organization or project access.

## Safety rules

- Never infer the project, MCP, custom role, member, or access rules from prior context. Present the eligible choices and require the user to select each target explicitly.
- Show members only by the masked identities returned by `list_access_members`. Never reconstruct, request, or reveal an unmasked identity.
- Do not expose raw identifiers or opaque references merely to distinguish choices. Present product names and masked identities; retain tool-returned selectors internally and use them only with the exact choice they represent.
- Assign only a role whose returned type is `custom`. Do not assign or modify built-in or system roles.
- Preserve all non-MCP grants and all MCP rules outside the exact confirmed delta. Never broaden access to every project, every MCP, or every tool as a shortcut.
- Stop when member results are suppressed, ambiguous, expired, or do not contain the selected masked identity. Ask the user for a different identity query rather than guessing.
- If any read reports that the capability is unavailable, stop without claiming a change.

## Workflow

1. Call `list_projects`, present the eligible project names, and ask the user to choose one exact project. Keep its returned selector internal.
2. Call `find_mcp` for only that project. Present configured MCP names and ask the user to choose one exact MCP. Keep its returned selector internal.
3. Call `get_mcp_access` for the selected project and MCP. Explain its authorization mode, known tool catalogue, and current role coverage. If it is not managed by role-based access, stop; this workflow must not imply that assigning a role will take effect.
4. Call `list_access_roles`. Present role names, types, privacy-safe member counts, and relevant MCP access summaries. Ask the user to choose an existing custom role or explicitly choose to create a new custom role.
5. Resolve the custom role without silently broadening it:
   - For a new role, collect its name, optional description, and exact MCP access rules from the selected MCP's current enumerable tools or supported disposition classes. Show the proposed rules and ask for explicit confirmation, then call `create_mcp_access_role` for the exact project and rules.
   - For an existing custom role, compare its coverage from `get_mcp_access` with the user's requested outcome. If a bounded MCP rule delta is needed, show exactly what will be added or removed, ask for explicit confirmation, then call `update_mcp_access_role`. If no delta is needed, make no role mutation.
   - Use fresh tool-returned opaque references and any required freshness or retry fields only as agent-operational inputs. Do not describe those mechanics as part of the user-facing outcome.
6. After any role mutation, call both `list_access_roles` and `get_mcp_access` again. Continue only if the selected custom role exists and the re-read shows the exact confirmed access to the selected MCP. If the state conflicts or is incomplete, stop and report that no member assignment will be attempted.
7. Ask the user for an identity search of at least three characters, then call `list_access_members`. Present only the returned masked identities and current role names. Require the user to choose one exact masked member; do not accept an unmasked identity or select on their behalf.
8. Immediately before assignment, refresh `list_access_roles`, `get_mcp_access`, and `list_access_members` for the exact prior choices. Confirm internally that the project, MCP, custom role, access rules, and masked member still match. If a selector expired or the choices changed, present the safe names and masked identities again and ask the user to reselect.
9. Summarize the proposed outcome using only the project name, MCP name, custom role name, masked member identity, and effective access. Ask for one final explicit confirmation of that complete assignment.
10. After confirmation, call `assign_mcp_access_role` using only the exact refreshed custom-role and member choices, the explicit project, and `confirmed: true`. Pass the selected member's `version` from the immediately preceding `list_access_members` response as `expected_version`, and supply a stable `idempotency_key` for retries of that exact request. Keep these fields internal. A version conflict requires a fresh member read, explicit reselection, and renewed confirmation before submitting a new request. Do not retry with another member, role, MCP, or project after a refusal or conflict.
11. The mutation response's `snapshot_scope: assignment_commit` identifies a historical snapshot, including on receipt replay. Never reuse its member version for a new write. Verify live state rather than treating that snapshot as final proof:
    - Call `list_access_roles` again and confirm the selected role's current state.
    - Call `get_mcp_access` again and confirm that role still grants the exact intended access to the selected MCP.
    - Call `list_access_members` again with the selected role and the user's exact identity query. Confirm the selected masked member is listed with that role when privacy-safe enumeration is available.
    - Use any privacy-safe effective-access evidence returned by `assign_mcp_access_role` together with these re-reads. If member enumeration is suppressed or any evidence is stale or inconsistent, report verification as incomplete instead of claiming effective access.
12. Report only the verified product outcome: selected project, MCP, custom role, masked member, and effective access. Clearly separate confirmed state from anything that could not be verified.

Project, MCP, custom-role, masked-member, rule-delta, and final assignment choices all remain explicit conversation checkpoints. All selectors and mutation-control fields remain agent-operational.
