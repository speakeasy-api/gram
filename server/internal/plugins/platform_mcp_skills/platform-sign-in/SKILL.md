---
name: platform-sign-in
description: Sign in to the Speakeasy AI Control Plane (AICP) Platform MCP when its tools are missing, a Platform call fails with an authentication error, or the user asks to log in or connect. Use before any other AICP workflow when the Platform tools are not available.
---

# Sign in to the Platform MCP

Installing this plugin adds the Platform MCP server but does not sign the user in. Until the user signs in through this client, the server lists no tools and every call fails. Some clients, including the Codex desktop app, do not open the sign-in page on their own, so the agent must tell the user how to start it.

## 1. Decide whether sign-in is needed

Sign-in is needed when any of these is true:

- no Platform tool is in your tool list (for example `list_projects`, or `search_platform_tools` and `call_platform_tool`);
- a Platform call fails with an authentication error, such as `unauthorized`, `401`, `OAuth authorization required`, or "not logged in";
- the user asks to sign in, log in, connect, or reconnect the Platform MCP.

If Platform tools are listed, call `list_projects`. If it succeeds, the user is already signed in; say so and continue with the original request. A `forbidden` refusal or "Platform MCP is not enabled for this organization" is not a sign-in problem: signing in again does not fix it. Tell the user that an organization administrator must grant access or enable the Platform MCP.

## 2. Start sign-in for this client

The sign-in must happen in the client you are running in. A dashboard session, a browser login, or another client's connection does not authorize this one. Use the steps for your client:

- **Codex (desktop app or CLI):** sign-in runs with the command `codex mcp login platform`. It opens the browser and waits for the user to finish.
  - Offer to run the command for the user. It needs network access and a local browser callback, so request to run it outside the sandbox and wait for the user's approval. Never run it without asking.
  - If the user prefers to run it, give them the exact command. On macOS, when `codex` is not on the `PATH`, the command is `/Applications/Codex.app/Contents/Resources/codex mcp login platform`.
  - If Codex reports that no server named `platform` exists, run `codex mcp list`, find the Platform MCP entry that this plugin added, and use that exact name.
  - The user can also open Settings, then MCP servers, and choose Authenticate next to the Platform MCP.
- **Claude Code:** ask the user to run `/mcp`, select the Platform MCP server, and choose Authenticate.
- **Cursor:** ask the user to open Cursor Settings, find the Platform MCP server in the MCP list, and choose its sign-in or connect action.
- **Any other client:** ask the user to open the client's MCP server settings and authenticate the Platform MCP server there.

In the browser, the user signs in to the Speakeasy AI Control Plane, selects the organization if asked, and approves the connection. All of this happens outside the chat.

## 3. Confirm the connection

After the user says sign-in is complete, call `list_projects` again.

- If it succeeds, report that the Platform MCP is connected and continue with the user's original request.
- If the Platform tools still do not appear, ask the user to start a new conversation or restart the client, so it reconnects with the new sign-in. Then call `list_projects` again. Do not report success until a call succeeds.
- If sign-in fails or the browser shows an error, report the error text the user sees and stop. Do not loop over sign-in attempts.

## Safety

- Never ask the user to paste access or refresh tokens, OAuth codes, client secrets, API keys, or passwords into the chat, and never read the client's credential files.
- Do not edit the client's MCP configuration to add headers or tokens.
- Signing in grants only what the user's live organization membership and roles allow. A successful sign-in does not prove access to any project or tool.
