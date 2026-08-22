---
name: add-mcp-from-remote-url
description: Register a user-supplied remote MCP server URL as a private source in an explicit AICP project through the Speakeasy AI Control Plane Platform MCP.
---

# Add an MCP from a remote URL

Use this workflow only through the authenticated Speakeasy AI Control Plane (AICP) Platform MCP, when the user has a remote MCP server URL that is not in the reviewed MCP Catalogue — a vendor's hosted server or a private deployment. It follows the guarded probe, evidence, confirm, register path: verify that the URL is a real MCP server, show the user exactly what the probe observed, obtain their explicit confirmation, and only then register the server privately for an explicit project. For servers that are in the reviewed catalogue, use the add-mcp-from-catalog workflow instead. The package itself grants no organization access.

## Safety rules

- Never ask the user to paste API keys, passwords, access or refresh tokens, OAuth codes, client secrets, secret headers, or MCP credentials into chat. Authentication and headers for a remote server are configured only in the AICP dashboard.
- A user-supplied MCP endpoint URL is accepted by exactly one tool: the read-only `probe_remote_mcp`. Never pass the raw user-supplied URL to any other tool, and never construct, guess, or modify a probe receipt. Exact server-issued identities returned by Platform MCP tools — including a `catalog_ref` that is the server's own normalized URL — are safe to pass back to the tools that ask for them.
- Register only with the exact server-issued `probe_receipt` returned by `probe_remote_mcp`, and only after the user has explicitly confirmed the probe evidence, including its gaps.
- Present the probe evidence honestly: server identity, tool count and names, auth posture, and every gap the probe reports. Absence of evidence is not evidence of absence.
- Respect enforcement outcomes honestly. When a result reports `blocked_pending_approval`, say that the organization's MCP approval enforcement blocks this server until an administrator approves it at the returned dashboard approvals URL. Never present a blocked server as ready, and never retry or rework a registration to get around enforcement.
- Keep the chosen project explicit. Do not infer it from a previous conversation or silently substitute another target.
- Show non-secret provider and setup URLs only when a Platform MCP tool returns them.
- Use `send_platform_mcp_feedback` only after asking for consent, and never include identifiers, URLs, credentials, payloads, headers, logs, or attachments.

## Workflow

1. Call `list_projects` to verify that the Platform MCP is authenticated and obtain the eligible projects. If authenticated discovery is unavailable, stop and ask the user to complete or repair AICP OAuth; do not claim that installation succeeded.
2. Ask the user for the exact remote MCP server URL and the exact target project. If the user names a product or server that may exist in the reviewed catalogue, search `search_mcp_catalog` by that name only — never pass the URL to it. A reviewed catalogue entry is preferable, and the add-mcp-from-catalog workflow covers it.
3. Call `probe_remote_mcp` with the user's URL. The probe refuses dead URLs, unreachable hosts, denied egress targets, and endpoints that are not MCP servers. Report a refusal as the bounded fact it states and do not retry unchanged input.
4. Show the returned evidence to the user — normalized URL, server name and version, tool count and names, auth posture, and every reported gap — and ask them to explicitly confirm registering this exact server for the chosen project. Do not proceed on silence or inference.
5. After explicit confirmation, call `register_remote_mcp_for_project` with the exact project, the server-issued `probe_receipt`, and a fresh idempotency key. Probe receipts expire after roughly ten minutes; when a receipt has expired, re-probe and re-confirm the fresh evidence instead of reusing old evidence.
6. If the registration reports `blocked_pending_approval`, explain that organization MCP approval enforcement blocks this server until an administrator approves it at the returned `dashboard_approvals_url`, and stop the guided flow there. After an administrator approves it, `get_platform_mcp_onboarding_status` reflects the current state.
7. Otherwise call `get_platform_mcp_onboarding_status` to check authenticated readiness. Any required header or credential entry for the remote server happens only on its Authentication settings page in the AICP dashboard, never in chat. To hand the user that page, call `get_setup_handoff` with the exact `project_slug`, `registration_id`, `provider_key`, and `catalog_ref` returned by `register_remote_mcp_for_project`, then present the returned `setup_url` as an exact clickable link.
8. Route secure setup from the exact readiness evidence:
   - For `upstream_identity_provider_not_configured`, explain that AICP can attach the identity provider it discovers live from the persisted remote server URL. Ask for explicit confirmation, then call `attach_platform_mcp_identity_provider` with `confirmed: true`, present its exact Inspect authorization URL, and wait for the user to use Connect or Authorize.
   - For `upstream_authorization_required`, after the user has explicitly confirmed provider attachment, call `attach_platform_mcp_identity_provider` again with `confirmed: true` to retrieve the current server-issued Inspect authorization URL. Present that exact clickable URL and wait for the user to use Connect or Authorize.
   - If it reports that no identity-provider metadata was discovered, call `get_setup_handoff` and present the server's Authentication settings page link instead.
   - For any other non-ready state, follow only the exact setup action or URL that state returns — never proceed past a non-ready state, and never invent a next step for it. Present or follow a setup link only when it is an absolute `https` URL with no credentials and no fragment.

   The user completes OAuth or secret entry outside the agent. Never request the resulting code, token, or secret in chat.

9. After the user completes any secure handoff, call `get_platform_mcp_onboarding_status` with a forced fresh check. Do not rely on stale or inferred readiness.
10. When readiness is current and ready, summarize the target and ask the user to confirm adding the server to the selected project's existing Default plugin. After confirmation, call `add_platform_mcp_to_default_plugin`, then recheck onboarding status and report the evidence returned by the server.

OAuth consent and approved secret entry are the only expected out-of-agent stops. URL and project selection, probe-evidence confirmation, identity-provider attachment confirmation, and final distribution confirmation stay in the conversation.
