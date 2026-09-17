---
name: add-existing-mcp-servers
description: Use when adding existing remote MCP servers from a local client inventory to an explicit Speakeasy AI Control Plane project, including discovering which servers are missing.
---

# Add existing MCP servers

Add selected supported remote servers to a Speakeasy AI Control Plane (AICP) project without editing local client configuration. Installation grants no access; live authentication, entitlement, membership and administrator checks remain authoritative. All management mutations use authenticated Platform MCP tools, never direct backend APIs.

## 1. Verify access and request discovery permission

Before any local discovery (including asking for manual inventory), successfully call `list_projects` with `limit: 100` through your OWN Speakeasy connection in this Claude Code session. Dashboard state, install intent, another client's authentication, or a prior session's result is not proof of access. It accepts only `limit` (capped at 100), not cursor or search. If unavailable or denied, stop and offer the normal Speakeasy sign-in/setup flow. If `truncated: true`, project discovery is incomplete: stop and hand off project selection to the AICP dashboard rather than inventing pagination or guessing a destination.

Before running `claude mcp list` locally, obtain explicit permission. Require explicit informed consent for the following effects, not merely permission to view a list. Explain that it health-checks approved/enabled servers, can launch stdio processes and contact local/private-network endpoints BEFORE filtering out unsupported entries. Those processes may write files or contact services; do not promise zero process side effects or passive/read-only discovery. Approval of a server in Claude is not consent to run this discovery now. Explain the scope: current CLI user, working directory and configuration scope, not every client, account or workspace. Connection status does not establish remote importability.

Offer a user-sanitized manual inventory instead (non-secret alias, safe endpoint, transport only), without running discovery, if the user declines those effects or local execution is unavailable. Never demand raw output. Do not inspect credential files, expand environment values, or forward raw output to any tool, service or report. Do not invent a no-connect flag. Never edit local configuration, approval settings or credentials to enable discovery. The no-edit rule constrains the agent; it cannot guarantee that the CLI or launched servers have no side effects.

If discovery succeeds but returns no entries, report that no servers were found in the inspected scope. Offer a user-sanitized manual inventory or the normal catalogue path (`add-mcp-from-catalog`) as explicit next choices; do not switch workflows without the user's choice. Do not claim import completion for an empty inventory.

## 2. Sanitize and classify locally

Retain only non-secret alias, exact safe endpoint, declared transport and provenance needed for selection. Never copy local credentials, including headers, tokens, passwords, environment secrets or OAuth state. Do not request secrets in chat.

Only public HTTPS Streamable HTTP endpoints are candidates. Exclude stdio commands, localhost/loopback, private-network or link-local endpoints, unsupported transports (including legacy SSE), fragments, embedded credentials, credential-like query parameters and uncertain URLs. Safe non-secret endpoint query parameters can be meaningful; preserve them. Never manufacture a safe URL by silently stripping credentials or changing its path. Block the entire uncertain item and ask for secure/manual resolution; do not echo sensitive URL components. Server validation remains authoritative, including network safety checks.

Exclude the connected Speakeasy management endpoint itself to prevent recursive import. Establish its identity from trusted connection endpoint metadata or other exact endpoint evidence, not display name alone. If that evidence is unavailable or ambiguous, flag the possible self-reference for manual resolution rather than importing it.

## 3. Confirm selection and check live inventory

Present sanitized candidates and blocked items. Confirm candidate selection and destination from the eligible projects returned above; keep that project's ID and slug paired. Never infer the destination from a local alias or the Default project.

Use `find_mcp` with the selected `project_id` and `limit: 100`, no query or readiness filter. Follow every `next_cursor` using `cursor` with the same project until exhausted. Never combine `query` and `cursor`; a name search is not an exhaustive inventory. If listing fails or pagination cannot finish, do not conclude a candidate is missing.

Use `get_mcp` with `project_id` and returned `mcp_id` to inspect possible matches. Compare exact upstream URL and server-issued source/registration evidence. Deduplicate aliases only by proven identity, not hostname or display name: different paths or meaningful query values may represent different servers. Preserve the alias-to-item mapping for the final report. If identity cannot be proven, block for manual resolution rather than guessing or creating a duplicate. An identity match alone is not an already-present success: apply the model-specific completion evidence in step 6 before classifying it. A matching pending or incomplete registration must not be reported as already present or complete, and must not trigger a duplicate registration.

## 4. Inspect missing candidates and confirm the exact batch

Call `inspect_mcp_candidate` with `remote_url` for each missing safe candidate; do not also supply `provider_key` or `catalog_ref`. Present its `canonical_url`, `transport`, `tool_names`/`tool_count`, `trust`, `authentication`, `oauth_discovery`, `requires_dashboard_setup`, `setup_category` and `actions` where returned. Distinguish observed tools from missing evidence; an empty tool list or authentication requirement is not proof of readiness. Unsupported, denied or inconclusive candidates remain blocked pending the returned guidance.

Obtain explicit confirmation of the exact inspected batch and project before any write: show each approved endpoint, transport, optional display name, evidence and outstanding setup needs, plus already-present and blocked items. If inspection changes the endpoint, disclose it and reconfirm; do not silently substitute. Catalogue substitutions require separate confirmation and the reviewed catalogue workflow, not an automatic replacement. Changed project, candidate or configuration requires fresh inspection as applicable and renewed confirmation.

## 5. Add confirmed supported items independently

For each confirmed missing item, call `register_remote_mcp` with `project_slug`, the confirmed `remote_url`, optional `display_name`, and a caller-generated `idempotency_key`. Keep one key per logical operation. Preserve all logical-operation inputs and the same idempotency key on retries, including after timeouts or uncertain write outcomes. Do not generate a new key merely because the result was lost. A changed operation requires renewed confirmation and a fresh key.

Apply returned repair/retry guidance and rate limits; do not retry permanent denial unchanged or bypass validation. Continue independent items after failure. Retain non-secret receipts, `registration_id`, `canonical_url`, `next_action` and any `dashboard_setup_url` for verification and handoff. A successful response alone is not completion.

## 6. Verify every selected item live

Re-read the selected project's complete inventory with `find_mcp` pagination and `get_mcp` for matching entries. Verify all selected items, including already-present entries and uncertain write outcomes, against the exact identity and project. Do not use cached preflight results as final evidence. Reconcile uncertain writes before retrying with their original inputs/key; if still unverified, report uncertainty rather than success.

For `model: platform_managed`, require a returned registration ID, `registration.status: registered` and `registration.components_complete: true` in addition to the exact project and endpoint match before reporting added, already present or complete. A missing registration, pending status or incomplete components means blocked/unverified; use returned repair guidance or a dashboard handoff, not duplicate creation. For an exact matching remote entry with `model: dashboard_managed` and no `registration`, report already present (dashboard-managed), not newly registered by this workflow. Do not require or invent a registration record for dashboard-managed entries; their `readiness.state: unsupported` is not evidence of a failed import or of working authentication. Other models or inconclusive identity/evidence remain blocked for manual resolution.

Report added / already present / blocked / failed per item, with alias mappings and evidence or a reason. Every selected supported server must be confirmed present in the selected project's live inventory to claim completion. Zero selections is not success. If any item remains blocked, failed or unverified, report partial completion and next steps, not unqualified success.

## 7. Keep authentication separate

Report registration and authentication/readiness separately: “added to the project” does not mean connected, authorized, working or distributed. Offer exact server-returned Speakeasy setup/authorization links as clickable links, including `dashboard_setup_url`; never reconstruct or invent them. If no link is returned, say so and offer a manual dashboard handoff.

Skip provider attachment for anonymous servers. Offer attachment only when inspection reports an authentication requirement and advertises a supported identity provider through its authentication/OAuth discovery evidence, and a registration ID is available. For direct remote inspection, require `authentication: authentication_required` and `oauth_discovery: available_dcr` before offering attachment; `available` alone or `incomplete` does not establish support for this dynamic-registration flow. These are prerequisites, not a guarantee: the attachment tool still validates the supported provider and may return repair guidance. Authentication required with no supported provider evidence means a secure/manual setup handoff, not a speculative attachment call. Retain separate explicit consent for provider attachment. Only after those evidence checks and that consent, `attach_platform_mcp_identity_provider` takes `project_slug`, returned `registration_id` and `confirmed: true`; present its exact returned `provider_url` and `authorization_url`. Secret entry and provider sign-in belong in that secure browser flow, never in chat or tool arguments. Do not force readiness, provider attachment or distribution to finish import. Leave local config unchanged; do not migrate credentials or remove local entries.
