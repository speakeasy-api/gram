# Provider setup backend contract

Save through `remoteSessions.commitServerIdentityConfiguration` with
`target_server_only: true`. Manual mode encrypts the supplied client secret and
associates the client with the exact MCP server. Existing-client mode only links
the selected client; it does not accept or replace its secret. A shared
user-session issuer returns HTTP 409 with a safe correction message before
registration or writes, and is checked
again under the issuer lock. Explicitly assign a dedicated issuer through the
existing User Identity/server settings APIs before retrying. Omitting the flag
preserves the existing settings workflow and its shared-binding authorization.

Inspect configured credentials through `listServerClientAssociations`, followed
by `remoteSessionClients.get` for the linked client. The client view exposes the
Client ID, scopes and callback URL, never the stored secret. The linked client
is evidence of saved configuration.
Leave the secret input empty. A scope change saved as a new manual client needs
fresh authorization; an old client's grant cannot authorize its replacement.

Sign in through the existing `GET /mcp/{slug}/connect/first-party` flow. That flow
uses the saved client, verifies OAuth state and persists encrypted user tokens.
Returning from or closing the provider tab is not success. Reload the current
user's remote sessions (including the exact client and user-session issuer), or
run the live test below. Keep credentials and tokens out of browser progress
storage. A disabled server needs its existing private-serving settings enabled
before opening its first-party sign-in route.

Call `POST /rpc/remoteSessions.testServerConnection` with `mcp_server_id` and the
project selector. It requires a validated dashboard session and `mcp:connect` on
the exact target; API keys cannot borrow their creator's user grant. It resolves
only the signed-in user's saved grant, including normal refresh behavior, then
initializes the saved Remote MCP source and lists every tool page. No tools run.
This tests the upstream connection; it can run while public serving is disabled.

The response has `status`, `code`, `message`, and a `tool_count` only on success.
Statuses are `ready`, `authorization_required`, `configuration_required`, and
`failed`. Codes identify safe recovery reasons, including rejected authorization,
unsupported transport, missing headers, timeouts and incomplete tool discovery.
A stored Authorization header returns `authorization_header_conflict`; remove
it before testing User Identity so a server credential cannot substitute for the
signed-in user.
HTTP validation, permission and internal failures use the existing error envelope.
Neither response returns credentials, tokens or raw provider error bodies. No
setup progress or completion record is persisted.

Platform MCP assessment: `get_my_mcp_connection_status` reports the external
caller's stored authorization state; it does not run this live probe. Existing
catalog/provider readiness tools use the initialization/tool-list probe under
their own reviewed identity contracts. Neither is a substitute for verifying one
dashboard user's direct saved grant. This endpoint intentionally requires a
validated browser session and refuses agent and API-key credentials, so neither
external Platform MCP callers nor managed assistants may invoke it. No new tool
is added and the existing tools retain their contracts. Session/API-key refusal,
exact-server permissions, and upstream initialize/list-only behavior are covered
by `serverconnection_test.go`. No demo seed
change is needed because this adds a live diagnostic, with no new stored data.
