---
name: implementing-mcp-provider-setup
description: Use when adding, integrating, changing, or reviewing a provider's MCP catalog setup page, onboarding wizard, app-registration instructions, credential form, or setup save/sign-in/test flow in the dashboard.
---

# Implementing MCP provider setup

Extend the approved setup from Catalog Add through verified connection. Reuse working provider code and shared actions.

## Establish scope and sources

1. Read the full provider issue, acceptance criteria and reviewed decisions through Linear. Separate provider work, shared backend work and unavailable contracts.
2. Inspect Git status, branch/base and worktrees. Locate existing Slack/GitHub flows, requested provider code and dirty reference files. Read reference checkouts without editing them; preserve inherited fixes.
3. State the provider/issue, base, reuse files and unresolved requirements. Keep changes provider-scoped; continue independent work when one branch is blocked.

Use sources in this order: current user instructions and approvals → issue and reviewed decisions → current published provider/product guides and owning API contracts → working React implementation and approved design lineage.

Read linked sources directly before asserting support or absence. Cached omissions do not justify removing choices. Distinguish separate registry integrations even when they share a provider name. Record each branch’s sources and separate documented settings, implemented contracts, mocked checks and live verification. On unresolved conflicts, show the exact evidence and ask the user.

Approved appearance establishes presentation, not provisioning, support, authentication or activation capabilities. Follow `setup-design-lineage.json` for presentation; `designs/research/` requires explicit authorization. Missing designs do not authorize substitute UI.

**Approval gate:** changes to approved layout, navigation, sequence, interactions, controls or provider behavior require a concrete proposal and explicit approval. Silence is not approval. Continue independent work while waiting. Wiring verified actions and fixing defects within approved behavior need no renewed approval. This gate overrides generic frontend guidance to choose a design without the user.

## Locate implementation

Use the [code map](../../../client/dashboard/src/pages/catalog/provider-setup/README.md) to locate owning files and tests on the active branch. Before provider-specific changes, read the relevant [provider contract](references/provider-contracts.md): Slack permissions, GitHub registration, Salesforce app/domain/source handling, Snowflake endpoints, SQL and OAuth, or Okta organization/app/PKCE configuration. Follow `frontend` for shared components and styling.

## Preserve first-time progression

Trace the actual Catalog **Add** action through every intermediate screen and choice to completion. Compare handlers with the working provider implementation, not just a standalone wizard.

Keep the Add modal’s existing name, identity and guardrail controls. Keep the provider’s configuration fields and instructions on the actual MCP server’s Setup page; follow its documented external app-registration and support handoffs. Preserve unaffected identity modes, bulk/headless callers and provider choices at their approved steps.

Treat creation, configuration, verification and serving as separate transitions: create the disabled MCP server, open its Setup page, then configure it. Before declaring missing endpoint details a creation blocker, inspect existing create/update APIs and disabled-state gates. A required URL need not be the final connection URL.

For an approved temporary endpoint, verify that probing, OAuth, connection testing and enablement cannot use it before replacement. Cover reload, direct routes, Settings and gateway entry; a disabled wizard button alone is insufficient. Keep catalog authentication metadata accurate.

If contracts cannot preserve this sequence safely, pause that path and ask for guidance with the exact missing behavior. Continue independent work. Modal fields or a pre-creation catalog wizard are not authorized workarounds.

## Write actionable instructions

Use the current guide to write numbered actions for the current page. Place actions after their prerequisites and beside the field or SQL they use. Explain where values come from or whether users choose them. Keep copy focused on the next action.

Show instructions and section headings before inputs are filled. SQL disclosure is separate: “View SQL” may collapse command text; incomplete commands may disable copying with a reason. Preserve save, sign-in, test and completion validation. Verify empty, invalid and completed states.

## Integrate supported actions

Read `server/internal/remotesessions/SETUP.md`; it owns save, grant and test contracts. Keep `providerSetupEnabled` fail-closed across catalog entry, sidebar and direct route; the backend independently gates testing. Guided handoff applies to User Identity. An unconfigured server can derive identity mode `none` before linking a client.

Save through `commitServerIdentityConfiguration` with `targetServerOnly: true`; preserve dedicated-issuer isolation and fresh target checks. Existing-client linking cannot replace secrets. Read saved configuration through secret-free queries; persist only non-secret choices and navigation. Obtain callbacks from new-client or retained-client APIs. Persist the selected upstream URL and verify provider-specific OAuth requirements rather than copying another provider’s settings.

Preserve configuration/source fingerprints, invalidation, permission-change authorization invalidation, inline retry/correction and stale-operation rejection. Refuse User Identity verification against a stored Authorization header. Returning from an OAuth popup does not prove authorization: verify the current user’s exact saved grant.

Completion requires `testServerConnection` to return `ready` with a tool count for the exact server and browser user. Initialize and list tools without invoking them; use the generated `testServerConnectionRequestBody` binding. Complete to the approved Overview, gateway or policy destination. Reopening requires fresh verification.

## Verify and hand off

Check every issue criterion against code and observed behavior. Exercise actual Catalog Add → server Setup, supported new/existing app branches, callbacks/scopes, loading, failure, retry, interrupted progress, source changes and completion. Spot-check affected identity modes and shared providers. Use focused tests, dashboard type-check/lint/knip and `gram-playwright-cli`, including a narrow viewport. Report the exact click path and unverified entry points; distinguish mocks from live provider authorization/discovery.

Assess Platform MCP, Command K and demo-seed through their skills. Source registration is not verified setup; existing inspect/register tools can return a secure dashboard continuation. Owner-run SQL and browser secret entry do not establish agent provisioning or credential-handling capabilities. Command K indexes canonical Overview; reassess exclusions when navigation changes. Seed new stored data when needed, never secrets or grants.

Before handoff, compare the diff to its intended base and account for every changed surface. Record test results, access blockers and catalog publication status in the PR or ticket. Commit, push, change Linear or attach demos only within user authorization.
