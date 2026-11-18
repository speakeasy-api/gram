# Provider contracts

Use this reference for differences between implemented providers. The [developer code map](../../../../client/dashboard/src/pages/catalog/provider-setup/README.md) locates owning helpers and tests; the [backend contract](../../../../server/internal/remotesessions/SETUP.md) owns shared API semantics. Verify upstream requirements against the linked primary sources when changing them. Product guides may still describe manual setup; merged code and reviewed decisions establish the guided journey.

| Provider   | Guided integration and branches                                                         | Credential / OAuth differences                                             |
| ---------- | --------------------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| Slack      | Official `https://mcp.slack.com/mcp`; new/existing app and read/all permissions         | Client ID + secret; Slack-only scope synchronization and grant revocation  |
| GitHub     | Official `https://api.githubcopilot.com/mcp`; new/existing organization-owned OAuth app | Client ID + secret; Device Flow off; no Slack permission synchronization   |
| Salesforce | Native Salesforce servers; own External Client App or Speakeasy app/support handoff     | Own app uses Consumer Key only; support handoff is not verified completion |
| Snowflake  | `com.snowflake/cortex-mcp`; existing/new native server                                  | Account OAuth, required ID + primary secret, PKCE, `client_secret_post`    |
| Okta       | `com.okta/managed-mcp`; new/existing Web or Native app                                  | Web: ID + secret / `client_secret_basic`; Native: ID / `none`; both PKCE   |

Registry names alone do not establish compatibility: match the supported remote template or endpoint. Native Salesforce, Snowflake and Okta are distinct from their older integrations. Generic Manual/Shared credential resolution of account-specific URL templates is outside the guided User Identity contract; do not claim those paths are implemented by this wizard.

## Slack

Changing saved permissions revokes the target issuer's users' grants before updating scopes. Read-only verification rejects stored write grants. Shared app connections refuse permission changes and point to identity settings. Keep the preparation lock until permission writes settle, including after cancellation. These operations are Slack-specific.

## GitHub

Use organization-owned OAuth app settings for new and existing apps, with the actual callback and Device Flow off. GitHub advertises issuer response parameters; Slack does not. Organization approval and SSO are possible recovery instructions, not causes inferred from the backend's bounded errors. Keep GitHub outside Slack's permission-sync and grant-revocation paths.

## Salesforce

Source: [published Salesforce setup guide](https://www.speakeasy.com/docs/ai-control-plane/guides/salesforce).

The implemented flow has **two app methods** against Salesforce-native MCP endpoints: install Speakeasy’s Salesforce application and finish OAuth with support, or create your own External Client App and configure credentials. Both select production/sandbox and one of seven native servers: the four SObject variants, Data 360, Headless 360 (Beta), or Tableau Next. Keep each server’s prerequisites and endpoint mapping. These app methods do not select different MCP implementations.

My Domain handling belongs to `salesforce.ts`: preserve normalization, production/sandbox/Developer Edition distinctions, and the separation from the legacy `MCP-Instance-URL` header. Domain edits invalidate credentials and verification; source-bound progress restores the selected domain. Consult [Salesforce server URLs](https://developer.salesforce.com/docs/platform/hosted-mcp-servers/guide/server-urls.html) when changing endpoint construction.

The installation URL comes from the guide and lives in `salesforce.ts`. Installation alone does not finish OAuth. The support page supplies the selected native endpoint and links to the published support instructions; it stops at that handoff without inventing tickets, activation polling, or a completion signal. The own-app path saves Consumer Key-only credentials with native `mcp_api refresh_token` scopes and uses the shared real sign-in/test actions. Consumer Key-only compatibility needs live verification evidence; implemented credential serialization alone does not establish it.

The older fixed-endpoint registry integration is a distinct server and retains generic configuration. It is not a hosting-mode alternative within the guide’s native setup.

Org type and native server selection belong on the Salesforce Setup page; install one catalog endpoint initially.

Preserve Salesforce progress migrations, configuration fingerprints, shared-source safeguards and guarded rollback. Compensation is not an atomic transaction.

## Snowflake

The native `com.snowflake/cortex-mcp` record uses an account-specific `/api/v2/databases/{database}/schemas/{schema}/mcp-servers/{name}` endpoint. The older hosted SQL integration is a different contract.

Use a public `snowflakecomputing.com` account hostname. Government regions, China, PrivateLink upstream hostnames, paths, credentials, and ports are rejected. PrivateLink changes only the generated browser-authorization setting; MCP and token exchange still require public connectivity. Setup provisions no network infrastructure.

Account-scoped OAuth uses `/oauth/authorize`, `/oauth/token-request`, PKCE, required Client ID and primary Client Secret, `client_secret_post`, and `session:role:all`. SQL uses the actual API-provided callback, `OAUTH_USE_SECONDARY_ROLES = NONE`, a non-privileged allowed role, and user defaults. OAuth secret extraction uses uppercase JSON keys. Setup never executes provisioning SQL or calls a Cortex Agent during verification.

Preserve identifier quoting/case, SQL literal escaping and specification delimiter guards. Reject MCP object names consisting only of `.` or `..`, which URL path parsing would normalize away. Creator grants reject `PUBLIC`, including quoted/case variants; runtime roles remain non-privileged. Snowflake adds no instance header or provisioning capability.

Authoritative references:

- [Speakeasy native setup guide](https://www.speakeasy.com/docs/ai-control-plane/guides/snowflake)
- [Snowflake Cortex Agents MCP](https://docs.snowflake.com/en/user-guide/snowflake-cortex/cortex-agents-mcp)
- [Snowflake custom OAuth clients](https://docs.snowflake.com/en/user-guide/oauth-custom)

## Okta

Sources: [Managed MCP browser setup](https://help.okta.com/mcp/en-us/content/topics/mcpserver/oidc-pkce-browser-based.htm), [client configuration](https://help.okta.com/mcp/en-us/content/topics/mcpserver/mcp-client-configuration-overview.htm), and [client authentication](https://developer.okta.com/docs/api/openapi/okta-oauth/guides/client-auth).

Target native `com.okta/managed-mcp`, not the older hosted integration or self-hosted STDIO server. The organization configures the `/mcp` upstream and org authorization server; Okta API scopes use that server rather than `/oauth2/default`. Accept only supported public `okta.com`, `oktapreview.com`, and `okta-emea.com` org hosts; custom domains and government orgs are excluded. Consult the [MCP overview](https://help.okta.com/mcp/en-us/content/topics/mcpserver/mcpserver.htm) for licensing and Early Access prerequisites; setup neither provisions nor polls them.

Web apps use `client_secret_basic`; Native apps use `none`. Both use S256 PKCE and the API-provided callback. Administrators grant scopes in Okta; setup neither collects nor persists a duplicate list. Credential saves omit the manual scope override so the existing sign-in flow resolves scopes from the resource challenge/metadata. Preserve the approved grant sequence: select Okta API Scopes, grant scopes for the desired tools, then check PKCE on General. App assignments are separate from scope grants and the user’s existing privileges. The plan explains the permission needed to configure the app and grant scopes. No API-token, device-flow, service-app or private-key branch is offered.

`OktaTask.tsx` owns choice-specific Web/Native and new/existing app instructions. Enable Okta MCP uses the standard Continue action; it does not claim verified enablement. A disabled Open Okta action explains the missing valid organization at the point of use.

Okta adds no instance header or provisioning capability.

## Keep provider mutations isolated

`useProviderConnection.ts` selects provider connections explicitly for Salesforce, Snowflake and Okta through `selectedProviderConnection`; Slack and GitHub return no selected connection and use their fixed provider configuration. Salesforce owns `MCP-Instance-URL` parsing and mutation. Other providers preserve unrelated headers, including duplicate, secret and request-mapped headers. The shared Authorization-header conflict check still applies to User Identity verification.

For endpoint, domain, app-type or permission edits, verify both provider-specific invalidation and the shared progress/source fingerprints. Saved configuration is not a saved authorization or test pass. Record live evidence separately for each branch, account type and endpoint tested; a merged implementation or controlled OAuth response is not a live compatibility result.
