---
name: add-mcp-from-remote-url
description: Add a user-supplied remote MCP server URL to an explicit AICP project through the Speakeasy AI Control Plane Platform MCP.
---

# Add an MCP from a remote URL

Use this workflow only through the authenticated Speakeasy AI Control Plane (AICP) Platform MCP when the user supplies a remote MCP server URL that is not in the reviewed MCP Catalogue. It follows the same guarded outcome a user completes in the AICP dashboard: inspect the URL, confirm the bounded evidence and project, register privately, finish secure setup, verify readiness, and add the ready server to the project's existing Default plugin. For a reviewed catalogue entry, use `add-mcp-from-catalog` instead. The package itself grants no organization access.

## Safety rules

- Never ask the user to paste API keys, passwords, access or refresh tokens, OAuth codes, client secrets, secret headers, or MCP credentials into chat. Authentication and headers are configured only through secure AICP dashboard setup.
- Use `inspect_mcp_candidate` as the only read-only inspection step for a user-supplied URL. It returns bounded evidence and does not register or distribute anything.
- Keep the target project explicit. Never infer it from a previous conversation or silently substitute another project.
- Before registration, show the user the returned URL, transport, tool count and names, authentication posture, setup requirement, and OAuth-discovery state. State any missing evidence honestly.
- Register only after the user explicitly confirms this exact remote server and project. Registration revalidates and re-inspects the URL; an earlier inspection is never trusted as admission evidence.
- Registration is private. Do not claim the server is available to users until fresh readiness succeeds and the user has confirmed adding it to the existing Default plugin.
- Show non-secret setup and authorization URLs only when a Platform MCP tool returns them.
- Use `send_platform_mcp_feedback` only after asking for consent, and never include identifiers, URLs, credentials, payloads, headers, logs, or attachments.

## Workflow

1. Call `list_projects` to verify that the Platform MCP is authenticated and obtain eligible projects. If discovery is unavailable, stop and ask the user to complete or repair AICP OAuth.
2. If the user names a product that could be in the reviewed catalogue, call `search_mcp_catalog` by name before handling the URL. Prefer an exact reviewed candidate when available.
3. Call `inspect_mcp_candidate` with `remote_url` set to the user's exact URL. Do not supply catalogue selectors at the same time.
4. Present the returned bounded evidence. Ask the user to explicitly confirm registering this exact URL in one exact project. If inspection reports an error or unavailable evidence, do not retry unchanged input or claim registration succeeded.
5. After confirmation, call `register_remote_mcp` with the exact project, URL, optional safe display name, and a fresh idempotency key. This re-inspects the URL and creates private project configuration only.
6. If `next_action` is `secure_dashboard_setup_required`, present the exact `dashboard_setup_url`. The user completes authentication or secret entry outside chat; never request the resulting value.
7. Call `get_platform_mcp_onboarding_status` for the selected project. For a non-ready result, follow only its server-provided next action. When it requires provider attachment, ask for explicit confirmation before `attach_platform_mcp_identity_provider`, then present its exact authorization URL.
8. After secure setup or authorization, call `get_platform_mcp_onboarding_status` with `force: true`. Do not rely on stale or inferred readiness.
9. When readiness is current and ready, summarize the selected project and ask the user to confirm adding the server to that project's existing Default plugin.
10. After confirmation, call `add_platform_mcp_to_default_plugin`, then recheck onboarding status and report the server-returned evidence.

OAuth consent, secret entry, and provider authorization are the only expected out-of-agent stops. URL and project selection, registration confirmation, provider-attachment confirmation, and final distribution confirmation stay in the conversation.
