# MCP provider setup

Read this before changing provider instructions, catalog handoff, or the save/sign-in/test flow for Slack, GitHub, or Salesforce. Use the [implementing-mcp-provider-setup skill](../../../../../../.agents/skills/implementing-mcp-provider-setup/SKILL.md) for scope and approval rules. Extend the existing shell and actions; the current provider ticket and explicit user decisions govern requirements.

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

| Connection                                                   | Configuration                                                           | OAuth                                                                                                                                                          |
| ------------------------------------------------------------ | ----------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Native (`com.salesforce/hosted-mcp`)                         | Selected server and production/sandbox determine the upstream URL       | External Client App Consumer Key, public client with PKCE, `mcp_api refresh_token`; production uses `login.salesforce.com`, sandbox uses `test.salesforce.com` |
| Speakeasy-hosted CRM (`com.pulsemcp.mirror/gram-salesforce`) | Fixed hosted endpoint; instance stored separately as `MCP-Instance-URL` | Instance-specific authorization/token endpoints, Consumer Key with optional secret, `api refresh_token`; a supplied secret selects `client_secret_post`        |

The native and hosted records are distinct integrations. The hosted registry's placeholder OAuth metadata and empty registration endpoint do not establish a working dynamic-registration contract; setup uses manual credentials and concrete instance endpoints.

Use the explicit mappings in `salesforce.ts` for all seven native choices. Data 360 places `sandbox` after `data/`; the platform and analytics servers place it before their category. Preserve exact endpoint matching and the selected catalog server/environment when entering setup.

Keep prerequisites specific to the selected server: Data 360 licensing/API version/permissions, Headless 360 beta/API availability, and Tableau Next setup/asset access. Activation is an instruction followed by the actual connection test; the page does not detect licenses, permissions, or activation.

Source writes must reject wrapper drift, conflicting Authorization headers, dynamic/secret or duplicate instance headers, and changes to a source shared by another server. Preserve retryable partial writes and persisted `configurationPending`: a failed credential replacement must not rehydrate an older client as successfully saved. Pending drafts are bound to the source ID, URL, and instance; external source changes return to the current connection choices, while completed partial writes advance the draft baseline for retry. Provider slugs include the issuer so switching environments or instances can create distinct bindings.

The support-assisted branch remains visibly unavailable with a correction action. Wiring it requires an installation URL, a support handoff, and an established completion contract. Keep those requirements separate from the own-app connection flow.

Authoritative references for rechecking Salesforce requirements:

- [Speakeasy SObject setup guide](https://www.speakeasy.com/docs/ai-control-plane/guides/salesforce)
- [External Client App](https://developer.salesforce.com/docs/platform/hosted-mcp-servers/guide/create-external-client-app.html), [OAuth endpoints and PKCE](https://developer.salesforce.com/docs/platform/hosted-mcp-servers/guide/postman.html), [GA OAuth scopes](https://developer.salesforce.com/docs/platform/hosted-mcp-servers/guide/beta-to-ga-transition.html)
- [Data 360](https://developer.salesforce.com/docs/platform/hosted-mcp-servers/guide/data360-mcp.html), [Headless 360](https://developer.salesforce.com/docs/platform/hosted-mcp-servers/guide/headless-360-mcp.html), [Tableau Next](https://developer.salesforce.com/docs/platform/hosted-mcp-servers/references/reference/tableau-next.html), [Tableau Next prerequisites](https://help.salesforce.com/s/articleView?id=005462387&language=en_US&type=1)
- [Hosted CRM registry record](https://github.com/speakeasy-api/mcp-registry/blob/main/data/servers/com.pulsemcp.mirror__gram-salesforce.json)

## Verification and related surfaces

Use the colocated provider tests and the setup integration tests beside `ProviderSetupPage.tsx` and `useProviderConnection.ts`. Cover changed endpoint mappings, credential requirements, invalidation, failed-save retry, stale results, and completion gating; spot-check Slack/GitHub when shared actions change. Inspect affected states in the local dashboard through `gram-playwright-cli`, including a narrow viewport.

Report mocked/controlled checks separately from live provider authorization and authenticated tool discovery. Record run results, account-access gaps, and catalog publication status in the PR or ticket rather than treating them as permanent documentation here.

Platform MCP reuses existing management operations; interactive credential entry and this browser-session-only probe add no agent tool. Command K's registered `useMcpServerCandidates` opens canonical Overview; Setup is an intentionally excluded transient subtask. No new stored data shape requires a demo-seed change, and OAuth credentials/grants/readiness are not seeded. Reassess these decisions if the workflow, API, resources, or permissions change.
