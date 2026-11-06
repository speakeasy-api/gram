# Salesforce setup (GRW-328)

Both Salesforce-native MCP and Speakeasy-hosted Salesforce CRM use the existing
setup shell, credential commit, first-party OAuth, and initialize/tool-list test.
The interactive React demo in the `MCP_Catalog_Setup_Forms` worktree is the
primary requirements reference; the published guide supplies the native SObject
settings. Simulated provider outcomes were not transferred.

## Connection contracts

| Connection                      | Configuration                                                             | OAuth                                                                                                                                   |
| ------------------------------- | ------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| Salesforce-native MCP           | Selected SObject server and production/sandbox determine the upstream URL | External Client App Consumer Key; public client with PKCE; `mcp_api refresh_token`; production or sandbox authorization/token endpoints |
| Speakeasy-hosted Salesforce CRM | Fixed hosted endpoint; Salesforce instance saved as `MCP-Instance-URL`    | Instance-specific authorization/token endpoints; Consumer Key and optional secret; `api refresh_token`                                  |

The native production pattern is
`https://api.salesforce.com/platform/mcp/v1/platform/<SOBJECT-SERVER>`;
sandbox adds `sandbox/` before `platform/`. The implemented choices are
`sobject-reads`, `sobject-mutations`, `sobject-deletes`, and `sobject-all`.
Data 360, Headless 360, and Tableau Next remain visible but unavailable pending
their connection contracts.

The registry record is `com.pulsemcp.mirror/gram-salesforce`, titled Salesforce
CRM, in `data/servers/com.pulsemcp.mirror__gram-salesforce.json`. It describes
the hosted endpoint and required instance header, not the native SObject
endpoints. Its OAuth metadata contains instance placeholders and an empty
registration endpoint despite advertising dynamic registration. Setup uses
manual credentials and concrete instance endpoints instead of assuming DCR.

The native record `com.salesforce/hosted-mcp` version `1.0.0` was published to
the isolated local catalog through Admin MCP on 2026-11-06, with all 14 supplied
endpoints, manual authentication, and the Salesforce guide link. The application
catalog API and browser list both return it. The earlier empty local search
preceded this publication. This local operation does not establish production
catalog availability and is not a seed or source-code change.

## Saving and verification

Connection, environment, server, app, or instance changes invalidate saved and
verified state and clear credentials when crossing connection boundaries.
Source updates preserve the distinction between endpoint and instance header.
They refuse wrapper drift, Authorization-header conflicts, dynamic/secret
instance headers, and modifications to a source shared by another server.
Partial source updates remain retryable; an incomplete credential replacement
cannot rehydrate an older client as successfully saved. Issuer-specific provider
slugs allow production, sandbox, and hosted-instance changes on the same server.

Callbacks come from the existing new-client/retained-client API. Secrets are
never restored or saved in browser progress. Saved public clients do not require
a secret. The existing OAuth action verifies the signed-in user's grant;
completion additionally requires a successful explicit connection test and opens
Overview. Stale asynchronous results cannot complete setup. No tools execute.

Native server activation remains an instruction in Salesforce. There is no
activation polling or simulated readiness: the actual connection test establishes
access. The documented app and server propagation delays are explanatory text.

## Unresolved branches and live verification

The support-assisted app branch is retained with an explicit unavailable state
and a correction action. No verified app-installation URL, support handoff, or
completion contract is available. It cannot complete until that contract exists;
no support orchestration or new backend state was added.

A real local native credential commit returned HTTP 200 with a nonfunctional
placeholder Consumer Key, and completion stayed disabled without sign-in.
This verifies backend public-client saving, not Salesforce authorization.

Live Salesforce OAuth and authenticated tool discovery remain pending an
authorized test account. Focused controlled tests prove request configuration,
failure/retry, invalidation, stale-result rejection, and completion gating;
they do not prove provider authorization. Browser inspection used a real local
server record and local dashboard session, including both connection choices,
the real callback, support gating, and a 400px viewport without horizontal overflow.

## Related surfaces

- Platform MCP: no new tool. This extends interactive credential entry and the
  existing browser-session-only connection probe. Existing management operations
  are reused and no API contract changes.
- Command K: individual server candidates remain covered by
  `useMcpServerCandidates` and open canonical Overview. Setup is an intentionally
  excluded transient task. Route/progress and completion tests cover the
  Salesforce extension; no new resource kind or search action is introduced.
- Demo seed: no new stored entity or schema, and no fake credentials or readiness
  are seeded. The existing staff-only setup rollout stays off in demo defaults.
  Browser fixtures and rollout changes were confined to the isolated local stack.
- Staff Admin MCP: unchanged; no admin workflow, API, or staff permission changes.

## Sources

- [GRW-328](https://linear.app/speakeasy/issue/GRW-328)
- [GRW-323](https://linear.app/speakeasy/issue/GRW-323)
- [GRW-372](https://linear.app/speakeasy/issue/GRW-372)
- [Speakeasy Salesforce guide](https://www.speakeasy.com/docs/ai-control-plane/guides/salesforce)
- [Hosted Salesforce registry record](https://github.com/speakeasy-api/mcp-registry/blob/main/data/servers/com.pulsemcp.mirror__gram-salesforce.json)
- [Salesforce server URLs](https://developer.salesforce.com/docs/platform/hosted-mcp-servers/guide/server-urls.html)
- [External Client App](https://developer.salesforce.com/docs/platform/hosted-mcp-servers/guide/create-external-client-app.html)
- [Native OAuth endpoints and PKCE](https://developer.salesforce.com/docs/platform/hosted-mcp-servers/guide/postman.html)

## Catalog browser follow-up (2026-11-06)

- Started in the published catalog entry, selected SObject Reads Sandbox and
  User Identity, and installed through the normal dialog. The automatic Setup
  handoff retained the sandbox endpoint.
- Traversed app, callback, security, activation and credential steps. Empty
  Consumer Key validation worked. Public-client placeholder saving returned
  HTTP 200; reload retained configuration without restoring verification or
  storing the key in browser progress.
- The real first-party sign-in reached Salesforce's sandbox authorization
  endpoint. Closing the placeholder-client popup kept testing and completion
  disabled. No Salesforce account was authorized.
- Switched the installed connection to hosted CRM, rejected an unsafe instance
  URL, and checked existing-app instructions and hosted scopes. Actual instance
  header creation, endpoint update and placeholder credential saving each
  returned HTTP 200. Completion remained disabled.
- Controlled probe responses separately covered sign-in-only gating, failed
  test/retry, successful test gating and navigation to Overview. These responses
  do not establish Salesforce OAuth or actual tool discovery; no tools ran.
- Production/sandbox endpoint pairs had identical catalog labels. The
  Salesforce-only correction adds explicit environment names, with regression
  coverage. The focused catalog/setup suite passes 228 tests.

Live authorization and authenticated discovery remain pending valid Salesforce
app credentials and an authorized test account. The additional catalog endpoints
for Data 360, Headless 360 and Tableau Next are published, but their setup choices
remain unavailable pending their authentication/prerequisite contracts.
