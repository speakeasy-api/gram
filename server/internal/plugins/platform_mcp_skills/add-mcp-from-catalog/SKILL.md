---
name: add-mcp-from-catalog
description: Add a reviewed MCP Catalogue server to an explicit AICP project through the Speakeasy AI Control Plane Platform MCP.
---

# Add an MCP from the catalog

Use this workflow only through the authenticated Speakeasy AI Control Plane (AICP) Platform MCP. It follows the same guarded outcome a user would complete manually in the AICP dashboard: select a reviewed MCP, configure it for an explicit project, finish secure setup, and verify readiness. The package itself grants no organization access.

If the user instead supplies a remote MCP URL outside the reviewed catalogue, use the `add-mcp-from-remote-url` workflow.

## Safety rules

- Never ask the user to paste API keys, passwords, access or refresh tokens, OAuth codes, client secrets, secret headers, or MCP credentials into chat.
- Never ask the user to supply an MCP endpoint. Use the server-owned catalog candidate and registration workflow.
- Show non-secret provider and setup URLs only when a Platform MCP tool returns them.
- Keep the chosen project and catalog candidate explicit. Do not infer either from a previous conversation or silently substitute another target.
- Registration is private and does not distribute or publish the MCP.
- Use `send_platform_mcp_feedback` only after asking for consent, and never include identifiers, URLs, credentials, payloads, headers, logs, or attachments.

## Workflow

1. Call `list_projects` to verify that the Platform MCP is authenticated and obtain the eligible projects. If authenticated discovery is unavailable, stop and ask the user to complete or repair AICP OAuth; do not claim that installation succeeded.
2. Call `search_mcp_catalog`. Present the eligible projects and reviewed candidates, then ask the user to choose one exact project and one exact candidate.
3. Call `inspect_mcp_candidate` for the selected candidate. Explain the bounded change and collect only the non-secret configuration fields declared by that result.
4. After explicit confirmation, call `register_catalog_mcp` with the exact selected project, reviewed candidate, declared non-secret configuration, and a fresh idempotency key. Do not distribute it.
5. Call `get_mcp_readiness` with the returned project and registration ID to inspect persisted readiness.
6. Route secure setup from the exact readiness evidence:
   - For `upstream_identity_provider_not_configured`, explain that AICP can attach the one identity provider discovered from the persisted reviewed MCP source. Ask for explicit confirmation, then call `attach_platform_mcp_identity_provider`, present its exact Inspect authorization URL, and wait for the user to use Connect or Authorize.
   - For `upstream_authorization_required`, call `attach_platform_mcp_identity_provider` again with confirmation to retrieve the current server-issued Inspect authorization URL. Present that exact clickable URL and wait for the user to use Connect or Authorize.
   - For any other secure dashboard setup result, present only its exact server-returned setup URL. The user completes OAuth or secret entry outside the agent. Never request the resulting code, token, or secret in chat.
7. After the user completes any secure handoff, call `get_mcp_readiness` with `force: true`. Do not rely on stale or inferred readiness.
8. When readiness is current and ready, report the server-returned evidence. Registration remains private until a separately rollout-gated exact-plugin distribution is available.

OAuth consent and approved secret entry are the expected out-of-agent stops. Project and catalog selection and identity-provider attachment confirmation stay in the conversation.
