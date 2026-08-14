---
name: add-mcp-from-catalog
description: Add a reviewed MCP Catalogue server to an explicit AICP project through the Speakeasy AI Control Plane Platform MCP.
---

# Add an MCP from the catalog

Use this workflow only through the authenticated Speakeasy AI Control Plane (AICP) Platform MCP. It follows the same guarded outcome a user would complete manually in the AICP dashboard: select a reviewed MCP, configure it for an explicit project, finish secure setup, verify readiness, and add it to that project's existing Default plugin. The package itself grants no organization access.

## Safety rules

- Never ask the user to paste API keys, passwords, access or refresh tokens, OAuth codes, client secrets, secret headers, or MCP credentials into chat.
- Never ask the user to supply an MCP endpoint. Use the server-owned catalog candidate and registration workflow.
- Show non-secret provider and setup URLs only when a Platform MCP tool returns them.
- Keep the chosen project and catalog candidate explicit. Do not infer either from a previous conversation or silently substitute another target.
- Registration is private until the reviewed MCP is ready and the user has chosen to add it to the project's existing Default plugin.
- Use `send_platform_mcp_feedback` only after asking for consent, and never include identifiers, URLs, credentials, payloads, headers, logs, or attachments.

## Workflow

1. Call `list_projects` to verify that the Platform MCP is authenticated and obtain the eligible projects. If authenticated discovery is unavailable, stop and ask the user to complete or repair AICP OAuth; do not claim that installation succeeded.
2. Call `search_mcp_catalog`. Present the eligible projects and reviewed candidates, then ask the user to choose one exact project and one exact candidate.
3. Call `inspect_mcp_candidate` for the selected candidate. Explain the bounded change and collect only the non-secret configuration fields declared by that result.
4. Call `register_platform_mcp_for_project` with the exact selected project, reviewed candidate, and declared non-secret configuration. Do not distribute it yet.
5. Call `get_platform_mcp_onboarding_status` to check authenticated readiness.
6. If the result requires secure dashboard setup or upstream authorization, present the exact server-returned setup or authorization URL. The user completes OAuth or secret entry outside the agent. Never request the resulting code, token, or secret in chat.
7. If readiness reports `upstream_identity_provider_not_configured`, explain that AICP can attach the one identity provider discovered from the persisted reviewed MCP source. Ask for explicit confirmation in the conversation. Only after confirmation, call `attach_platform_mcp_identity_provider`, present its exact Inspect authorization URL, and wait for the user to use Connect or Authorize.
8. After the user completes any secure handoff, call `get_platform_mcp_onboarding_status` with a forced fresh check. Do not rely on stale or inferred readiness.
9. When readiness is current and ready, summarize the target and ask the user to confirm adding it to the selected project's existing Default plugin.
10. After confirmation, call `add_platform_mcp_to_default_plugin`. Never create a replacement plugin or distribute to a different project.
11. Recheck onboarding status and report the evidence returned by the server. If a bounded repair action is returned, explain it without inventing provider- or client-specific instructions.

OAuth consent and approved secret entry are the only expected out-of-agent stops. Project and catalog selection, identity-provider attachment confirmation, and final distribution confirmation stay in the conversation.
