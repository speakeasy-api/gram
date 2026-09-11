---
name: review-shadow-mcp
description: Review a Shadow MCP target, approve an explicit audience, and safely onboard and distribute it through the Speakeasy AI Control Plane Platform MCP.
---

# Review and distribute a Shadow MCP

Use this workflow only through an authenticated Speakeasy AI Control Plane (AICP) Platform MCP connected as an external administrator. It follows the guarded AICP dashboard outcome: review one observed target, make an explicit audience decision, optionally onboard an approved remote target, and distribute the ready MCP to one exact plugin. It is not available to managed project assistants or the member-safe catalogue. Installing this package grants no organization access or authority.

## Safety rules

- Keep the project, opaque Shadow MCP target, approval audience, rationale, registered MCP, and plugin explicit. Never infer or silently substitute them.
- Treat server-returned evidence as bounded. Report every gap and incomplete result. Do not recover raw target values, people, principals, evidence, or research traces.
- Never widen an audience to Everyone to make approval or distribution succeed. Everyone is valid only when the user deliberately selects and confirms the server-returned Everyone reference.
- Secrets never enter chat. Present only setup, provider, and authorization URLs returned by AICP, and wait for the user to complete secure setup in the browser.
- Registration is private and separate from approval and distribution. Do not claim an atomic promotion from observed target to distributed MCP.
- A review decision or receipt does not prove current distribution admission. Registration re-inspects the target, and distribution rechecks the current approval and complete plugin audience.
- A denial, unavailable admission result, or version conflict returns to fresh reads and user review. Never approve again, change the audience, or retry a mutation automatically.

## Workflow

1. Call `list_projects`. If its `truncated` result is true, report that project discovery is incomplete and hand off to the AICP dashboard; otherwise present the eligible projects and ask the user to select one exact project. Retain both its returned ID for Shadow and plugin inventory tools and its slug for readiness and distribution tools.
2. Call `list_shadow_mcp_inventory` with that exact project ID. Present only its bounded summaries and ask the user to select one exact opaque target reference. Call `get_shadow_mcp_review` with the same project ID and target reference.
3. Establish the intended approval audience. Call `list_plugin_assignments` for the exact project. If the user intends to match an existing plugin, ask them to name it and call `get_plugin`; use only a complete, untruncated assignment set. Ask the user to select exact server-returned audience references and provide a bounded rationale. Stop and use the AICP dashboard if the required assignments are truncated or incomplete.
4. Refresh `get_shadow_mcp_review` and any audience references before presenting the bounded review evidence, every gap, the proposed allow or deny decision, the complete selected audience, and the rationale. Ask for explicit confirmation of that exact fresh project, target, decision, audience, and rationale, then immediately call `decide_shadow_mcp_access` with the immediately preceding `expected_version`, a fresh idempotency key, and `confirmed: true`. If it conflicts or a reference expires, re-read and re-present the changed state, then obtain confirmation again. An allow requires one or more selected audience references; a deny has none.
5. Call `get_shadow_mcp_review` again and report the committed live review. The review result does not expose a raw remote URL or an onboarding action, so never reconstruct either. To onboard an approved target, hand off to the AICP dashboard and stop until the user confirms dashboard setup is complete.
6. After dashboard onboarding, call `find_mcp` with an explicit user-provided query, present the bounded configured MCP matches, and ask the user to select one exact result. Call `get_mcp` for that exact MCP and continue only when it returns a Platform-managed registration ID. Call `get_mcp_readiness` with the exact project slug and registration ID and `force: true`; continue only when fresh evidence says the MCP is ready.
7. Call `list_plugins`, present the exact project plugins, and ask the user to choose one. Call `get_plugin` for that exact plugin and require its complete, untruncated assignment set. Compare the whole set with the approved audience and current `distribution_admission`; keep publication state separate. If assignments must change, refresh `list_plugin_assignments`, present the complete replacement and state that it changes who receives every MCP server in that plugin, not only this target. Ask for explicit confirmation, then call `set_plugin_assignments` with the immediately preceding `assignment_version` as `expected_assignment_version`, a fresh idempotency key, and `confirmed: true`. Re-read `get_plugin` after the mutation.
8. Present the exact plugin, its complete audience, the ready MCP, and the current admission and publication states. Confirm this exact distribution with the user before calling `distribute_mcp_to_plugin`. If it returns a denial or conflict, re-read `get_shadow_mcp_review` and `get_plugin` and return to user review without automatically changing or renewing the approval.
9. After distribution, call `get_plugin` and `get_shadow_mcp_review` again. Report the live attachment, distribution admission, and publication evidence separately. Do not claim that users have the MCP unless the returned live state supports that conclusion.

The approval, registration, assignment, and distribution confirmations are separate decisions. Browser setup and authorization are separate secure handoffs. Preserve each boundary even when the user wants to complete the whole workflow.
