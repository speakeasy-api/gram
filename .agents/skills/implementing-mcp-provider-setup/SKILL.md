---
name: implementing-mcp-provider-setup
description: Use when adding, integrating, changing, or reviewing a provider's MCP catalog setup page, onboarding wizard, app-registration instructions, credential form, or setup save/sign-in/test flow in the dashboard.
---

# Implementing MCP provider setup

Extend the approved Catalog Add → server Setup → verified connection flow. Slack, GitHub, Salesforce, Snowflake and Okta are implemented; choose the relevant existing implementation before adding another.

## Read the owning reference

| Task                                                                 | Reference                                                                                                          |
| -------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------ |
| Locate components, installers, state and focused tests               | [Developer code map and extension checklist](../../../client/dashboard/src/pages/catalog/provider-setup/README.md) |
| Resolve provider endpoint, app, scopes or authentication differences | [Provider contracts](references/provider-contracts.md), relevant provider section                                  |
| Change credential saving, authorization or connection testing        | [Backend contract](../../../server/internal/remotesessions/SETUP.md)                                               |
| Change shared components or styling                                  | `frontend` skill                                                                                                   |

## 1. Establish scope and evidence

Read the provider issue, acceptance criteria and reviewed decisions. Inspect Git status, branch/base and existing implementation; treat dirty reference checkouts as read-only. State the exact catalog integration, reuse files, supported branches and missing contracts. Keep provider work separate from backend, catalog publication and rollout work.

Use current user instructions and approvals, then reviewed decisions to establish intended behavior. Use owning APIs and merged code for implemented behavior, current provider documentation for upstream requirements, and approved design lineage for appearance. Published product guides may describe an older manual flow: identify conflicts rather than replacing merged behavior or silently dropping approved choices. Read linked sources directly before asserting support or absence. Documented settings, controlled checks and live verification are distinct evidence.

**Approval gate:** changes to approved layout, navigation, sequence, interactions, controls or provider behavior require a concrete proposal and explicit approval. Silence is not approval. Wiring verified actions and fixing defects within approved behavior need no renewed approval. Continue independent work when one path is blocked. This gate overrides generic frontend guidance to choose a design without the user.

Follow `setup-design-lineage.json` for presentation; `designs/research/` requires explicit authorization. Designs establish presentation, not provisioning or activation capabilities.

## 2. Preserve the installed-server journey

Trace actual Catalog Add through name, identity and optional guardrails to the installed server's Setup page. Provider configuration belongs there; preserve external app-registration and support handoffs. Guided setup applies to User Identity. Preserve other identity modes and bulk/headless callers; an unconfigured guided server may initially derive identity mode `none`.

Treat creation, configuration, verification and serving as separate transitions. For account-specific endpoints, follow the existing disabled-server installation path. Before declaring a missing final URL a creation blocker, inspect create/update APIs and disabled-state guards. An approved temporary endpoint must skip probing, automatic identity configuration and icon discovery. Replace it before OAuth or testing; backend guards must reject enabled creation and enablement while it remains. Cover direct routes, reload, Settings and gateway entry. Preserve installer guardrails, rollback and catalog authentication metadata.

If the contract cannot safely preserve this sequence, identify the exact missing behavior and ask for guidance. A pre-creation wizard or extra Add-modal fields require approval.

## 3. Reuse connection actions and write usable instructions

Extend shared behavior when another provider needs it; keep provider contracts isolated. Use the backend contract for save/read/sign-in/test semantics. Preserve dedicated-issuer safeguards, exact-server/user checks, API-provided callbacks and the generated `testServerConnectionRequestBody` binding. Keep `providerSetupEnabled` fail-closed at catalog, sidebar and direct route; the backend independently gates testing.

Persist only non-secret choices and navigation. Configuration/source changes invalidate authorization and test results; stale operations cannot restore readiness. Returning from OAuth is not authorization evidence. Reopening requires fresh verification. Complete only after `ready` with a tool count, to the approved Overview, gateway or policy destination. Salesforce's support branch terminates at its documented handoff.

Use numbered actions beside the relevant fields or SQL, after prerequisites. Explain who supplies each value. Show instructions and headings before inputs are filled; incomplete SQL can withhold copying with a reason while instructions remain visible. Preserve inline correction, cancellation and retry. Distinguish save failure, partial configuration and post-save handoff failure so recovery targets the failed operation.

## 4. Verify and hand off

Use the code map's verification matrix for affected behavior and shared-provider regressions. Test actual entry paths, changed configuration, interrupted progress, correction destinations and retained values, not just standalone task rendering. Use `gram-playwright-cli` for browser checks, including narrow layouts. Report controlled API/OAuth checks separately from live provider authorization and authenticated discovery; never execute upstream tools to verify setup.

Assess Platform MCP, Command K and demo seed through their skills. Registration or stored authorization is not a live connection test. Existing inspect/register tools can provide secure dashboard continuation; owner-run SQL and browser secret entry do not imply agent provisioning. Command K indexes canonical Overview; reassess exclusions when navigation changes. Seed new stored data when needed, never secrets or grants.

Compare the diff to its intended base. Account for each changed surface, criterion, verification result, blocker and catalog publication requirement. Commit, push, change Linear or attach demos within user authorization. Customer guides and FDE enablement remain rollout work under GRW-331.
