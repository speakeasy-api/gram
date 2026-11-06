# MCP provider setup

Read this before changing provider instructions, catalog handoff, or the save/sign-in/test flow for Slack, GitHub, Salesforce, or Snowflake. Use the [implementing-mcp-provider-setup skill](../../../../../../.agents/skills/implementing-mcp-provider-setup/SKILL.md) for scope and approval rules. Extend the existing shell and actions; the current provider ticket and explicit user decisions govern requirements.

## Owning code

| Concern                                                          | Source                                                                                                                           |
| ---------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| Page shell, rollout gate, completion to Overview                 | [ProviderSetupPage.tsx](../../mcp/x/setup/ProviderSetupPage.tsx)                                                                 |
| Endpoint allowlist, catalog handoff, non-secret progress         | [provider-setup.ts](../../mcp/x/setup/provider-setup.ts)                                                                         |
| Credential saving, OAuth, testing, stale-operation guards        | [useProviderConnection.ts](../../mcp/x/setup/useProviderConnection.ts)                                                           |
| Provider steps, invalidation, completion conditions              | [state.ts](./state.ts)                                                                                                           |
| Provider instructions and connection action rows                 | [ProviderTasks.tsx](./ProviderTasks.tsx), [SalesforceTask.tsx](./SalesforceTask.tsx), [ConnectionTask.tsx](./ConnectionTask.tsx) |
| Salesforce endpoint mappings, prerequisites, OAuth configuration | [salesforce.ts](./salesforce.ts)                                                                                                 |
| Backend identity and connection-test contract                    | [SETUP.md](../../../../../../server/internal/remotesessions/SETUP.md)                                                            |

Read the owning code before editing; this document records safeguards and provider distinctions rather than copying every endpoint or field.

## Shared safeguards

- `providerSetupEnabled` gates the sidebar, direct route, and catalog handoff. Missing/failed reads fail closed; a loading direct route waits before mounting setup. The backend independently gates connection testing.
- Catalog handoff applies to User Identity installations. Service Account and Manual retain their existing paths. Setup can also configure an unconfigured supported server: `deriveIdentityMode` can still report `none` before the first client is linked. Installation choice is not a durable identity mode.
- Save with `remoteSessions.commitServerIdentityConfiguration`, `clientMode: "manual"`, and `targetServerOnly: true`. Preserve dedicated-issuer isolation and fresh target checks. Existing-client linking cannot replace a secret. Read saved configuration through the secret-free association/client queries.
- Obtain callbacks from the new-client or retained-client API. Replacing credentials requires entering any secret needed by that provider; Salesforce public clients can omit it. Keep secrets and tokens out of browser progress.
- Sign in through the first-party OAuth route. Returning from the popup is not authorization. The shared action verifies the current user's exact saved grant; the explicit connection test remains a separate action.
- `testServerConnection` initializes the saved source and lists tools without executing them. Only `ready` with a tool count passes. Keep the generated request body as `testServerConnectionRequestBody` with the exact `mcp_server_id`.
- App/configuration changes invalidate authorization and test results. Preserve generation checks against stale requests, inline retry/correction actions, and completion gating. Complete setup opens Overview; reopening requires fresh verification. Persist only non-secret choices and navigation.
- A stored Authorization header conflicts with User Identity testing. Keep this refusal so shared credentials cannot masquerade as the signed-in user's grant.

## Provider distinctions

### Slack

Changing saved permissions revokes the target issuer's users' grants before updating scopes. Read-only verification rejects stored write grants. Shared app connections refuse permission changes and point to identity settings. Keep the preparation lock until permission writes settle, including after cancellation. These operations are Slack-specific.

### GitHub

Use organization-owned OAuth app settings for new and existing apps, with the actual callback and Device Flow off. GitHub advertises issuer response parameters; Slack does not. Organization approval and SSO are possible recovery instructions, not causes inferred from the backend's bounded errors. Keep GitHub outside Slack's permission-sync and grant-revocation paths.

### Salesforce

Read the current [published Salesforce setup guide](https://www.speakeasy.com/docs/ai-control-plane/guides/salesforce) directly before changing this flow. Cached search excerpts have omitted documented branches and endpoints; their omissions are not evidence that a path is unsupported.

The current guide documents **two app methods** against Salesforce-native MCP endpoints: install Speakeasy’s Salesforce application and finish OAuth with support, or create your own External Client App and configure credentials. Both select production/sandbox and one of seven native servers: the four SObject variants, Data 360, Headless 360 (Beta), or Tableau Next. Keep each server’s prerequisites and endpoint mapping. These app methods do not select different MCP implementations.

The optional My Domain field accepts the org’s Current My Domain URL or hostname: `<org>.my.salesforce.com` for production, or `<org>--<sandbox>.sandbox.my.salesforce.com` for sandbox. Blank keeps standard login/test hosts. Known Salesforce Setup and Lightning page URLs normalize to their matching login origin when the field loses focus. Developer Edition domains retain `.develop` and use the documented `/develop/` MCP segment. A supplied domain sets both OAuth endpoints and the actual upstream MCP URL using Salesforce’s documented `/d/<domain>/[sandbox/|develop/]<server-name>` format. See [Salesforce server URLs](https://developer.salesforce.com/docs/platform/hosted-mcp-servers/guide/server-urls.html) and [OAuth configuration](https://developer.salesforce.com/docs/platform/hosted-mcp-servers/guide/postman.html). This is separate from the legacy `MCP-Instance-URL` header. Domain edits invalidate credentials and verification; saved scoped URLs restore the domain selection.

The installation URL comes from the guide and lives in `salesforce.ts`. Installation alone does not finish OAuth. The support page supplies the selected native endpoint and links to the published support instructions; it stops at that handoff without inventing tickets, activation polling, or a completion signal. The own-app path saves Consumer Key-only credentials with native `mcp_api refresh_token` scopes and uses the shared real sign-in/test actions. The guide explicitly says that compatibility is not yet live-verified with the product; report it accordingly.

The older fixed-endpoint registry integration is a distinct server and retains generic configuration. It is not a hosting-mode alternative within the guide’s native setup. Reuse the shared plan, fields, provider instructions, credentials and connection actions rather than copying prototype layouts.

For a single supported catalog Add with setup enabled, retain Identity and optional Guardrails, then open Setup. Org type and native server selection belong on the guided page; install one catalog endpoint initially. Other identity modes, bulk/headless/plugin callers and rollout-disabled installs retain their existing paths.

Salesforce progress uses revision 4 and persists only non-secret native server/environment/domain, app-method choices and configuration fingerprints. Older snapshots reset; they cannot restore the removed legacy-integration switch. Preserve configuration invalidation, stale-operation rejection, shared-source safeguards and guarded rollback. Compensation is not an atomic transaction.

### Snowflake

The native `com.snowflake/cortex-mcp` record uses an account-specific `/api/v2/databases/{database}/schemas/{schema}/mcp-servers/{name}` endpoint. The older hosted SQL integration is a different contract and stays outside this setup. The catalog dialog collects the four required values only for this exact native record/template. [snowflake-install.ts](../snowflake-install.ts) resolves a concrete URL before both probing and creation; incomplete or invalid values block installation. Other providers retain their current installation behavior. See [snowflake.ts](./snowflake.ts) for hostname validation, endpoint encoding, and complete-statement SQL generation; [SnowflakeTask.tsx](./SnowflakeTask.tsx) extends the approved new/existing server React flow.

Use a public `snowflakecomputing.com` account hostname. Government regions, China, PrivateLink upstream hostnames, paths, credentials, and ports are rejected. PrivateLink changes only the generated browser-authorization setting; MCP and token exchange still require public connectivity. Setup provisions no network infrastructure.

Account-scoped OAuth uses `/oauth/authorize`, `/oauth/token-request`, PKCE, required Client ID and primary Client Secret, `client_secret_post`, and `session:role:all`. SQL uses the actual API-provided callback, `OAUTH_USE_SECONDARY_ROLES = NONE`, a non-privileged allowed role, and user defaults. OAuth secret extraction uses uppercase JSON keys. SQL is displayed for the owner to execute; incomplete statements are withheld. Setup never executes provisioning SQL or calls a Cortex Agent during verification.

Preserve identifier quoting/case, SQL literal escaping, specification delimiter guards, and source-bound secret-free progress. Account/server edits invalidate credentials and verification. The shared source safeguards described above also protect Snowflake endpoint writes; Snowflake adds no instance header or backend capability.

Authoritative references:

- [Speakeasy native setup guide](https://www.speakeasy.com/docs/ai-control-plane/guides/snowflake)
- [Snowflake Cortex Agents MCP](https://docs.snowflake.com/en/user-guide/snowflake-cortex/cortex-agents-mcp)
- [Snowflake custom OAuth clients](https://docs.snowflake.com/en/user-guide/oauth-custom)

## Verification and related surfaces

Use the colocated provider tests and the setup integration tests beside `ProviderSetupPage.tsx` and `useProviderConnection.ts`. Cover changed endpoint mappings, credential requirements, invalidation, failed-save retry, stale results, and completion gating; spot-check Slack/GitHub when shared actions change. Inspect affected states in the local dashboard through `gram-playwright-cli`, including a narrow viewport.

Report mocked/controlled checks separately from live provider authorization and authenticated tool discovery. Record run results, account-access gaps, and catalog publication status in the PR or ticket rather than treating them as permanent documentation here.

Platform MCP decision: keep the existing `inspect_mcp_candidate` and `register_catalog_mcp` tools for an authenticated project administrator to inspect and register the native Snowflake source with declared non-secret URL values. Registration returns the secure dashboard continuation when manual setup remains. No Snowflake-specific tool is added: server/access SQL is owner-run guidance, OAuth Client Secret entry must stay outside agent transcripts, and sign-in plus connection testing must prove the current browser user’s exact grant. This PR adds no provisioning execution or agent credential-handling contract. A registered source proves registration only; completion requires `remoteSessions.testServerConnection` to return `ready` with a tool count for the exact server and browser user. Command K's registered `useMcpServerCandidates` opens canonical Overview; Setup is an intentionally excluded transient subtask. No new stored data shape requires a demo-seed change, and OAuth credentials/grants/readiness are not seeded. Reassess these decisions if the workflow, API, resources, or permissions change.
