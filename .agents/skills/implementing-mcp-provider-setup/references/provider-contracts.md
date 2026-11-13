# Provider contracts

## Slack

Changing saved permissions revokes the target issuer's users' grants before updating scopes. Read-only verification rejects stored write grants. Shared app connections refuse permission changes and point to identity settings. Keep the preparation lock until permission writes settle, including after cancellation. These operations are Slack-specific.

## GitHub

Use organization-owned OAuth app settings for new and existing apps, with the actual callback and Device Flow off. GitHub advertises issuer response parameters; Slack does not. Organization approval and SSO are possible recovery instructions, not causes inferred from the backend's bounded errors. Keep GitHub outside Slack's permission-sync and grant-revocation paths.

## Salesforce

Source: [published Salesforce setup guide](https://www.speakeasy.com/docs/ai-control-plane/guides/salesforce).

The current guide documents **two app methods** against Salesforce-native MCP endpoints: install Speakeasy’s Salesforce application and finish OAuth with support, or create your own External Client App and configure credentials. Both select production/sandbox and one of seven native servers: the four SObject variants, Data 360, Headless 360 (Beta), or Tableau Next. Keep each server’s prerequisites and endpoint mapping. These app methods do not select different MCP implementations.

My Domain handling belongs to `salesforce.ts`: preserve normalization, production/sandbox/Developer Edition distinctions, and the separation from the legacy `MCP-Instance-URL` header. Domain edits invalidate credentials and verification; source-bound progress restores the selected domain. Consult [Salesforce server URLs](https://developer.salesforce.com/docs/platform/hosted-mcp-servers/guide/server-urls.html) when changing endpoint construction.

The installation URL comes from the guide and lives in `salesforce.ts`. Installation alone does not finish OAuth. The support page supplies the selected native endpoint and links to the published support instructions; it stops at that handoff without inventing tickets, activation polling, or a completion signal. The own-app path saves Consumer Key-only credentials with native `mcp_api refresh_token` scopes and uses the shared real sign-in/test actions. The guide explicitly says that compatibility is not yet live-verified with the product; report it accordingly.

The older fixed-endpoint registry integration is a distinct server and retains generic configuration. It is not a hosting-mode alternative within the guide’s native setup.

For a single supported catalog Add with setup enabled, retain Identity and optional Guardrails, then open Setup. Org type and native server selection belong on the guided page; install one catalog endpoint initially. Other identity modes, bulk/headless/plugin callers and rollout-disabled installs retain their existing paths.

Preserve Salesforce progress migrations, configuration fingerprints, shared-source safeguards and guarded rollback. Compensation is not an atomic transaction.

## Snowflake

The native `com.snowflake/cortex-mcp` record uses an account-specific `/api/v2/databases/{database}/schemas/{schema}/mcp-servers/{name}` endpoint. The older hosted SQL integration is a different contract.

The installer uses the exact disabled-only source marker `https://app.snowflake.com/speakeasy-mcp-setup` from `client/dashboard/src/pages/catalog/snowflake-install.ts`. It skips MCP probing and identity auto-configuration for that marker. The backend refuses enabled creation and enablement while the marker remains, and skips default-icon discovery for it. The shared credential-save action replaces it with the validated account-specific URL before committing credentials and enabling private sign-in. Real-source fingerprints scope secret-free progress, and normal Settings/Overview remain available for the created server. Gateway entry retains its destination through the server Setup URL and adds membership after credential configuration. The normal catalog installer owns guardrail creation and partial-install rollback. `SnowflakeTask.tsx` supplies the approved new/existing server instructions.

Use a public `snowflakecomputing.com` account hostname. Government regions, China, PrivateLink upstream hostnames, paths, credentials, and ports are rejected. PrivateLink changes only the generated browser-authorization setting; MCP and token exchange still require public connectivity. Setup provisions no network infrastructure.

Account-scoped OAuth uses `/oauth/authorize`, `/oauth/token-request`, PKCE, required Client ID and primary Client Secret, `client_secret_post`, and `session:role:all`. SQL uses the actual API-provided callback, `OAUTH_USE_SECONDARY_ROLES = NONE`, a non-privileged allowed role, and user defaults. OAuth secret extraction uses uppercase JSON keys. Setup never executes provisioning SQL or calls a Cortex Agent during verification.

Preserve identifier quoting/case, SQL literal escaping, specification delimiter guards, and source-bound secret-free progress. Reject MCP object names consisting only of `.` or `..`, which URL path parsing would normalize away. Account/server edits invalidate credentials and verification. Shared-source checks, fresh-target validation, stale-operation rejection and guarded rollback also protect Snowflake endpoint writes; Snowflake adds no instance header or provisioning capability.

Authoritative references:

- [Speakeasy native setup guide](https://www.speakeasy.com/docs/ai-control-plane/guides/snowflake)
- [Snowflake Cortex Agents MCP](https://docs.snowflake.com/en/user-guide/snowflake-cortex/cortex-agents-mcp)
- [Snowflake custom OAuth clients](https://docs.snowflake.com/en/user-guide/oauth-custom)

## Okta

Sources: [Managed MCP browser setup](https://help.okta.com/mcp/en-us/content/topics/mcpserver/oidc-pkce-browser-based.htm), [client configuration](https://help.okta.com/mcp/en-us/content/topics/mcpserver/mcp-client-configuration-overview.htm), and [client authentication](https://developer.okta.com/docs/api/openapi/okta-oauth/guides/client-auth).

Target native `com.okta/managed-mcp`, not the older hosted integration or self-hosted STDIO server. The organization configures the `/mcp` upstream and org authorization server; Okta API scopes use that server rather than `/oauth2/default`. Accept only supported public `okta.com`, `oktapreview.com`, and `okta-emea.com` org hosts; custom domains and government orgs are excluded. Consult the [MCP overview](https://help.okta.com/mcp/en-us/content/topics/mcpserver/mcpserver.htm) for licensing and Early Access prerequisites; setup neither provisions nor polls them.

Web apps use `client_secret_basic`; Native apps use `none`. Both use S256 PKCE and the API-provided callback. Administrators grant scopes in Okta; setup neither collects nor persists a duplicate list. Credential saves omit the manual scope override so the existing sign-in flow resolves scopes from the resource challenge/metadata. Preserve the approved grant sequence: select Okta API Scopes, grant scopes for the desired tools, then check PKCE on General. App assignments are separate from scope grants and the user’s existing privileges. The plan explains the permission needed to configure the app and grant scopes. No API-token, device-flow, service-app or private-key branch is offered.

`OktaTask.tsx` owns choice-specific Web/Native and new/existing app instructions. Enable Okta MCP uses the standard Continue action; it does not claim verified enablement. A disabled Open Okta action explains the missing valid organization at the point of use.

Okta shares the server-first marker lifecycle with Snowflake. `account-provider.ts` selects its temporary URL, and `provider_setup.go` guards both providers. Replace the marker with the validated org-specific upstream before OAuth configuration; preserve gateway and policy continuation. No instance header or provisioning capability is introduced.
