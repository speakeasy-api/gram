---
name: share-coding-session
description: Share one of the OAuth caller's captured coding-agent sessions through the Speakeasy AI Control Plane as a short-lived, one-read handoff URL, with explicit session selection, lifetime choice, and confirmation.
---

# Share a coding session

Use the authenticated external Speakeasy AI Control Plane connection to help the user choose one of their own visible captured coding sessions and mint a short-lived, burn-after-read handoff URL. The tools enforce the OAuth caller's current organization membership and session ownership; never imply that this workflow can see or share sessions outside what that caller is authorized to list.

If this connection lists `call_platform_tool` instead of the tools named below, find each exact name with `search_platform_tools`, describe it with `describe_platform_tool`, and invoke it with `call_platform_tool` using the same arguments. Tool discovery does not grant access; the call still checks the current authenticated Platform MCP connection.

## 1. Verify access and list sessions

Always begin by successfully calling `list_my_sessions`, even if the user supplied an ID or believes they remember one. Never ask the user to know, copy from elsewhere, or type a session ID from memory. This fresh list is both the access check and the authoritative set from which the user can choose.

If the call is unavailable or authentication is not active, stop and explain that an authenticated Speakeasy AI Control Plane connection is required. Do not request authentication material in chat. If the call is rate-limited, report that and wait for the indicated retry window or ask the user to try later; do not loop.

If `sessions` is empty, say that no captured coding sessions are currently available to this OAuth caller in the organization. Do not infer whether sessions owned by anyone else exist.

## 2. Show a numbered chooser

Present a concise numbered chooser. Every option must show:

- `session_id`, copied exactly from `list_my_sessions`;
- `title` (use a clear fallback such as “Untitled session” if empty); and
- `summary`, labeled **Description** (use “No description available” if empty).

Include project and last activity only when they help distinguish options. Do not include transcript content, working-directory details, or `chat_id` unless the user specifically needs them to disambiguate. Ask the user to choose by number or title; resolve their answer back to the exact displayed `session_id` yourself.

If the answer is ambiguous, show the matching options again and ask. If it refers to a session that was not in the displayed list, call `list_my_sessions` again and present a fresh chooser rather than accepting an unverified ID.

## 3. Choose the exact lifetime

Ask for the link lifetime in seconds. The default is 900 seconds. Values below 60 are clamped to 60; values above 3600 are clamped to 3600. Before confirmation, state the exact effective lifetime after clamping, not only the user's raw request.

Explain all of these consequences plainly:

- the HTTPS URL is a bearer URL, so anyone holding it can read the digest once;
- a chat app, notification service, or other link previewer may consume that one read before the intended recipient opens it; and
- the link expires quickly at the exact effective lifetime just shown.

## 4. Obtain explicit confirmation

Display the exact selected title, exact `session_id`, and exact effective lifetime together. Ask the user to explicitly confirm that exact session and lifetime. A general request such as “share my session,” an earlier choice without the lifetime, or silence is not confirmation.

Do not call `create_session_handoff_link` with `confirmed: true` until this confirmation arrives. Do not read or export session content merely to prepare the confirmation.

If the user changes the session or lifetime, show the revised exact selection and effective lifetime and ask again.

## 5. Mint once

After explicit confirmation, call `create_session_handoff_link` exactly once with:

- the selected `session_id` from the displayed `list_my_sessions` result;
- the chosen `ttl_seconds` (the service applies the same clamp); and
- `confirmed: true`.

This mutation is non-idempotent and additive. Never blindly retry it.

Handle outcomes safely:

- `not_found`: the session may have disappeared or no longer be visible to this OAuth caller. Call `list_my_sessions` again, present a fresh chooser, and obtain a new exact confirmation before minting.
- unavailable: report that session sharing is currently unavailable; do not substitute another sharing mechanism.
- rate limited: report the limit and wait or ask the user to try later; do not retry in a loop.
- confirmation required: repeat the selected displayed title, `session_id`, and exact clamped lifetime, then ask for explicit confirmation again.
- transport interruption, timeout, or other uncertain result after submission: a URL may have been minted even though no response arrived. There is no safe blind reconciliation by repeating this non-idempotent mutation. Report the uncertainty, preserve any definite response evidence, and ask the user before minting another URL. If they agree, refresh `list_my_sessions`, reconfirm the exact session and lifetime, and make one new call.

## 6. Return only the handoff result

On success, return only the HTTPS `url` and `expires_at` from the tool result. Do not invent, transform, shorten, pre-open, preview, or fetch the URL. Do not add Cursor links, client deep links, local-agent commands, or alternate URLs.
