# Slack and GitHub setup integration

The shared setup flow supports Slack and GitHub in the MCP server details shell. GitHub setup includes organization, OAuth-app and credential steps. Endpoint matching and catalog handoff recognize only the official Slack and GitHub MCP endpoints. The Identity modal remains; only User Identity enters setup. Service Account and Manual retain their paths. Other providers remain outside this integration.

## Backend contract

The GRW-324 parent supplies the backend and generated SDK contracts. Credential saving uses `remoteSessions.commitServerIdentityConfiguration` with `targetServerOnly: true`. Saved configuration exposes the Client ID, callback and scopes, never the secret. The saved form is disabled and uses placeholder dots; Update credentials requires a new secret.

Sign-in opens the existing first-party OAuth endpoint. Returning from the provider tab triggers `remoteSessions.testServerConnection`; only `ready` with a tool count verifies sign-in. The explicit connection test remains a separate action and never executes tools. The generated request property is `testServerConnectionRequestBody`, serialized as `{ mcp_server_id }` with the ordinary dashboard session.

Changing apps clears unsaved credentials and authorization/test results. Changing saved Slack permissions revokes every person's grants on the target issuer before updating scopes. Read-only verification rejects broader stored write grants and requires removing the app's existing Slack authorization before retrying. Shared app connections refuse the change and link to identity settings. Continue retries failed permission changes, and cancellation retains the mutation lock until preparation settles. Verification cannot accept a stale result.

Backend failures stay in the relevant action row with retry or correction actions. Complete setup opens Overview. Reopening setup restores choices and saved configuration but requires fresh verification. Session progress contains only non-secret choices and step navigation; credentials, tokens, authorization, connection success and completion are not restored.

## Verification

Focused tests cover Slack and GitHub state invalidation, manifest/callback consistency, saved credential presentation, backend request serialization, inline recovery, cancellation, connection outcomes, GitHub organization validation, both app paths, issuer metadata, first-party sign-in, and exclusion of other providers from setup routing. Browser checks use the actual signed-in local dashboard; controlled API tests do not prove live provider failure recovery.

## Surface assessments

Platform MCP intentionally has no new tool: a signed-in person configures one installed server through interactive credential entry and first-party OAuth. Existing connection-status tools retain their own identity contracts; the GRW-324 live diagnostic requires a validated human browser session and excludes agents and API keys.

Command K’s existing `useMcpServerCandidates`, registered in `useLauncherCandidates`, opens the canonical Overview for the named server. Setup is an intentionally excluded transient subtask. No new entity or data shape requires a demo seed change; existing GitHub catalog/server examples remain available. No OAuth secrets, grants or fake readiness are seeded. Setup-guide sidebar visibility remains deferred.

## GitHub specifics

The organization opens its organization-owned OAuth app settings. New and existing apps use the same credential form and backend save. Instructions use the real new-client callback, or the retained callback when credentials are already saved, and leave Device Flow off. GitHub’s published OAuth metadata advertises issuer response parameters; Slack does not. GitHub never enters the Slack permission-sync or grant-revocation paths.

The backend exposes bounded connection failures rather than distinguishing GitHub organization policy and SSO responses. Inline failures therefore offer the approved organization-approval and SSO instructions as possible corrections without asserting either cause. Credential and app-setting corrections remain available beside them. No simulated success controls are included.

Run `client/dashboard/scripts/github-setup.playwright.txt` through `mise run playwright run-code --filename=...` on an installed GitHub setup page to check both app paths, callback, credential clearing, refresh, completion gating and layouts. Successful live GitHub authorization requires valid OAuth app credentials; controlled-response tests do not establish provider authorization or SSO success.
