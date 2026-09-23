# Slack workspace authorization

Organization administrators manage workspace authorizations under Identity, in the Slack workspaces preview tab. The organization-targeted PostHog key `claude-tag-support` gates both the dashboard and server. Missing keys, disabled providers, and evaluation errors fail closed. Target the PostHog organization group key using the organization slug. Create the rollout key before release; the code does not create remote flags.

Configure a dedicated directory app with `GRAM_SLACK_DIRECTORY_CLIENT_ID` and `GRAM_SLACK_DIRECTORY_CLIENT_SECRET`. Empty values and `unset` leave authorization unavailable. Register `<SERVER_URL>/slack-directory/callback` with Slack. The server requires HTTPS outside local development. The app requests the bot scopes `users:read` and `users:read.email`. It accepts workspace installations, including a workspace within Enterprise Grid, and rejects organization-wide installations. OAuth exchange and `auth.test` must agree on the workspace ID.

Credentials are encrypted together in a versioned bundle. Access tokens, refresh tokens, token type, and expiry remain server-side. This change retains rotation material but does not run a refresh worker. An expired token requires reconnection. No remote Slack app settings are changed by this implementation. Disconnect clears Gram's stored credentials; it does not uninstall the Slack app or change project runtime installations.

OAuth state lasts ten minutes and is bound to the organization, user, and session. Completion consumes it atomically. Reconnection and disconnection preserve the durable workspace row and replace its generation. Each begin captures the visible generations so an authorization started before another administrator's disconnect cannot restore access. Concurrent first connections to the same workspace require the later completion to start again. Mutation and audit commit together.

Connected means authorization succeeded and its stored credentials have not expired. It does not claim a directory was synced or that invocations are protected. This service never modifies memberships or identity mappings. External Slack Connect users are outside this version's scope.

## Platform MCP assessment

The outcome is browser consent for a specific organization's Slack workspace, performed by an organization administrator. Existing `attach_platform_mcp_identity_provider` manages project MCP sign-in provider attachment and does not represent this connection. No Platform MCP tool is added: this release requires a live browser session bound to the consent state and has no directory inspection outcome yet. The service, provider protocol, and browser callback tests establish authorization and credential boundaries. Directory inspection tools should be assessed with the sync and mapping API work.

## Local evidence

The shared demo seed contains two synthetic workspace history records without usable credentials. One is disconnected and one requires authorization. Seed screenshots do not establish a successful Slack OAuth exchange. Provider tests use a deterministic local HTTP server to verify the Slack protocol, including workspace mismatch, rejected scopes, enterprise installation, and rotating tokens. Live Slack verification additionally requires the dedicated app credentials and a registered callback.
