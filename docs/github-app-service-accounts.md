# GitHub App service accounts

A customer-owned GitHub App can supply a shared upstream identity for a remote MCP server. This is an installation-token provider, not an OAuth `client_credentials` grant or a static header.

## Configure

1. Create a GitHub App and give it only the repository permissions the MCP tools need.
2. Install the app on the required repositories. Record its App ID and Installation ID, and generate an RSA private key in GitHub.
3. As a Speakeasy organization administrator, open the MCP server's **Settings → Identity → Service Account → GitHub App**. The same fields are available during catalog and URL setup.
4. Enter the GitHub instance URL, App ID, Installation ID, and private key in PEM format. GitHub.com is the default. For GitHub Enterprise Server, enter the instance origin, such as `https://git.example.com`, not its `/api/v3` URL.
5. Save. Speakeasy verifies that the key can obtain an installation token before storing it encrypted. Saving replaces this server's other upstream identity associations, but does not delete shared credentials. Existing connections must reconnect.
6. Grant the intended callers permission to use this auth client (`mcp:auth`) through the existing credential access controls. Configuring an app does not grant use of it.
7. Reconnect and test a read-only tool against an installed repository.

The MCP server must accept GitHub installation access tokens. Credential verification proves the app/installation exchange, not that every upstream tool supports the token or its permissions.

## Enterprise and network access

GitHub.com uses `https://api.github.com`; enterprise instances use `<instance>/api/v3`. The API origin and the MCP origin are separate. Tokens are bound to the configured MCP origin and are never redirected elsewhere. Both destinations use the existing outbound network protections. A private enterprise deployment still needs a permitted network path and a trusted TLS certificate; this feature does not bypass either requirement.

## Rotation and failures

Paste a replacement PEM key and save to rotate it. The private key is write-only. Tokens are cached per process, with tenant, auth-client, and credential-version isolation, and renewed before expiration. Concurrent misses share one exchange per process. An upstream 401 can trigger one renewal; a just-minted rejected token is not minted repeatedly. A disabled or detached client fails closed rather than falling back to another identity.

Do not paste a generated installation token into the static credential form. It expires, and static headers do not renew it.

## Integration notes

- Auth-client category: `service_account`; provider storage: `mcp_auth_client_github_apps`. No synthetic OAuth registration or issuer.
- Setup and settings share the GitHub App fields. Provider credentials are never returned by read APIs or audit snapshots.
- Platform MCP's `get_mcp_connection_settings` reports `credential_provider`. Its `configured` status means an enabled choice is offered, as for other providers; it is not a live token, header-freshness, or connectivity check. Runtime checks can still require setup or reconnection. Existing shared-auth-client access tools remain applicable. Private-key provisioning stays in the authenticated dashboard/API rather than adding a tool that puts the PEM in agent conversations.
- Command K searches service-account display names across all result pages for authorized organization readers. Results are project-scoped and open the attached server’s identity settings. Credential names remain local to fuzzy search and are not sent to the intent service.
- The demo contains a disabled GitHub App with an unusable placeholder key. It must never mint tokens. Replace credentials and explicitly enable the client to test an actual installation.
