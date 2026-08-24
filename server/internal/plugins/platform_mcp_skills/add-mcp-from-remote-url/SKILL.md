---
name: add-mcp-from-remote-url
description: Add a user-supplied remote MCP server URL to an explicit AICP project through the Speakeasy AI Control Plane Platform MCP.
---

# Add an MCP from a remote URL

Use this workflow only through the authenticated Speakeasy AI Control Plane (AICP) Platform MCP when the user supplies a remote MCP server URL that is not in the reviewed MCP Catalogue. It follows the same guarded outcome a user completes in the AICP dashboard: inspect the URL, confirm the bounded evidence and project, register privately, finish secure setup, and verify readiness. For a reviewed catalogue entry, use `add-mcp-from-catalog` instead. The package itself grants no organization access.

## Safety rules

- Never ask the user to paste API keys, passwords, access or refresh tokens, OAuth codes, client secrets, secret headers, or MCP credentials into chat. Authentication and headers are configured only through secure AICP dashboard setup.
- Use `inspect_mcp_candidate` as the only read-only inspection step for a user-supplied URL. It returns bounded evidence and does not register or distribute anything.
- Keep the target project explicit. Never infer it from a previous conversation or silently substitute another project.
- Before registration, show the user the returned URL, transport, tool count and names, authentication posture, setup requirement, and OAuth-discovery state. State any missing evidence honestly.
- Register only after the user explicitly confirms this exact remote server and project. Registration revalidates and re-inspects the URL; an earlier inspection is never trusted as admission evidence.
- Registration is private. Do not claim the server is available to users until fresh readiness succeeds and exact-plugin distribution is rollout-enabled.
- Show non-secret setup and authorization URLs only when a Platform MCP tool returns them.
- Use `send_platform_mcp_feedback` only after asking for consent, and never include identifiers, URLs, credentials, payloads, headers, logs, or attachments.

## Workflow

1. Call `list_projects` to verify that the Platform MCP is authenticated and obtain eligible projects. If discovery is unavailable, stop and ask the user to complete or repair AICP OAuth.
2. If the user names a product that could be in the reviewed catalogue, call `search_mcp_catalog` by name before handling the URL. Prefer an exact reviewed candidate when available.
3. Call `inspect_mcp_candidate` with `remote_url` set to the user's exact URL. Do not supply catalogue selectors at the same time.
4. Present the returned bounded evidence. Ask the user to explicitly confirm registering this exact URL in one exact project. If inspection reports an error or unavailable evidence, do not retry unchanged input or claim registration succeeded.
5. After confirmation, call `register_remote_mcp` with the exact project, URL, optional safe display name, and a fresh idempotency key. This re-inspects the URL and creates private project configuration only.
6. If this workflow is running in a managed project assistant, stop after registration and present the returned dashboard setup URL when one exists. The assistant reads only persisted, actor-scoped readiness evidence and does not force provider probes or attach providers.
7. For an external Platform MCP client, if `next_action` is `secure_dashboard_setup_required`, present the exact `dashboard_setup_url`. The user completes authentication or secret entry outside chat; never request the resulting value.
8. For an external Platform MCP client, call `get_mcp_readiness` with the selected project and returned registration ID. For a non-ready result, follow only its server-provided repair action. When it requires provider attachment, ask for explicit confirmation before `attach_platform_mcp_identity_provider`, then present its exact authorization URL.
9. For an external Platform MCP client, after secure setup or authorization, call `get_mcp_readiness` with `force: true`. Do not rely on stale or inferred readiness.
10. For an external Platform MCP client, when readiness is current and ready, report the server-returned evidence. Registration remains private until a separately rollout-gated exact-plugin distribution is available.

OAuth consent, secret entry, and provider authorization are the expected out-of-agent stops. URL and project selection, registration confirmation, and provider-attachment confirmation stay in the conversation.
