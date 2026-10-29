# Slack setup integration

GRW-326 contains Slack setup only: the approved four-step flow in the real MCP server details shell. Endpoint matching and catalog handoff recognize only the official Slack MCP endpoint. Other providers keep their existing installation paths and belong to their own setup issues.

## Backend contract

The GRW-324 parent supplies the backend and generated SDK contracts. Credential saving uses `remoteSessions.commitServerIdentityConfiguration` with `targetServerOnly: true`. Saved configuration exposes the Client ID, callback and scopes, never the secret. The saved form is disabled and uses placeholder dots; Update credentials requires a new secret.

Sign-in opens the existing first-party OAuth endpoint. Returning from the provider tab triggers `remoteSessions.testServerConnection`; only `ready` with a tool count verifies sign-in. The explicit connection test remains a separate action and never executes tools. The generated request property is `testServerConnectionRequestBody`, serialized as `{ mcp_server_id }` with the ordinary dashboard session.

Changing apps clears unsaved credentials and authorization/test results. Changing saved permissions revokes the current person's grants on the target issuer before updating scopes. Shared app connections refuse the change and link to identity settings. Continue retries failed permission changes, and cancellation retains the mutation lock until preparation settles. Verification cannot accept a stale result.

Backend failures stay in the relevant action row with retry or correction actions. Complete setup opens Overview. Reopening setup restores choices and saved configuration but requires fresh verification. Session progress contains only non-secret choices and step navigation; credentials, tokens, authorization, connection success and completion are not restored.

## Verification

Focused tests cover Slack state invalidation, manifest/callback consistency, saved credential presentation, backend request serialization, inline recovery, cancellation, connection outcomes, and exclusion of other providers from setup routing. Browser checks use the actual signed-in local dashboard; controlled API tests do not prove live provider failure recovery.

## Surface assessments

The existing Platform MCP connection-status tools retain their own identity contracts; the GRW-324 live diagnostic requires a validated human browser session and intentionally excludes agents and API keys. This UI adds interactive credential entry and OAuth, not a new agent management outcome.

The existing Command K MCP resource entry opens Overview; setup is a transient task within that resource, with no new named resource. No demo credentials or fake readiness are seeded. Setup-guide sidebar visibility remains deferred.
