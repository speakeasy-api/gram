---
name: implementing-mcp-provider-setup
description: Use when adding, integrating, changing, or reviewing a provider's MCP catalog setup page, onboarding wizard, app-registration instructions, credential form, or setup save/sign-in/test flow in the dashboard.
---

# Implementing MCP provider setup

Extend approved setup from first entry through connection completion. Reuse existing components and actions; verify progression end to end.

## Establish scope before editing

1. Read the full provider issue, acceptance criteria and reviewed decisions through connected Linear. Search Linear before asking for identifiers. Separate provider work from shared-backend and validation issues. Identify unavailable requirements rather than inventing them.
2. Inspect Git status, branch/base and worktrees. Locate the established working flow (such as Slack/GitHub) and requested provider implementation, including dirty reference files.
3. Briefly state provider/issue, base, reuse files and unresolved requirements. Keep the new diff provider-scoped; preserve inherited providers and fixes.

**Ready to implement:** scope, base, first-time sequence and provider contracts are established. Keep unresolved branches pending; continue authorized work.

## Source precedence and UI approval

Use this order:

1. The user's current instructions and explicit approvals.
2. The provider issue and its reviewed decisions.
3. Current published product/provider setup documentation and verified owning API/backend contracts for behavior.
4. Existing provider React implementation and shared components for reuse; approved design lineage for presentation.

Read the current linked source before asserting support or absence. Cached summaries and excerpts can lag; their omissions do not justify removing approved choices. Compare current sources and ask on unresolved conflicts.

Establish every offered branch against those behavioral sources before wiring it. Demo/prototype branches remain proposed UI until their contracts are verified. Approval to reuse a demo does not establish provisioning, support, authentication or activation capabilities. Keep distinct registry integrations separate; a shared provider name does not make them interchangeable. Resolve conflicts with the user using the exact sources rather than combining them into an invented flow.

Record each branch's authoritative sources and distinguish documented candidate settings, implemented contracts, controlled checks and live provider verification.

Find previous work through `git worktree list`, repository search and linked branches/PRs. Read reference files without editing that checkout. Adapt provider-specific pieces so inherited fixes survive.

When design evidence is needed, follow `setup-design-lineage.json`. Approved appearance does not establish provider outcomes. `designs/research/` is excluded without explicit authorization; missing files do not authorize substitute UI.

**Approval gate:** before changing approved layout, navigation, sequence, interactions, controls or provider behavior, show the concrete difference and obtain explicit approval. Silence is not approval. Continue independent work while waiting; keep dependent UI unchanged. This overrides generic frontend guidance to choose a design when the user is unavailable. Reusing approved UI, wiring verified actions and fixing defects within that behavior do not require renewed approval.

## First-time entry and progression

Trace first-time entry through every intermediate screen and decision to completion. Catalog onboarding includes the actual **Add** action; other entries matter when affected. Compare handlers and user actions against the established working flow and approved React implementation.

Keep each provider choice at its approved step. Preserve approved identity choices and unaffected installation paths. Keep provider setup details—account information, endpoint variables, app registration and OAuth credentials—on the MCP server's Setup page, outside the Add modal. Preserve the modal's existing name, identity and guardrail controls.

If blocked on creating the server without those details, pause that implementation path and ask the user for guidance. Explain the exact contract checked and the missing behavior; continue independent work. Do not add setup fields to the modal or introduce a pre-creation configuration page as a workaround. An existing generic component does not authorize extra steps or relocating fields; restore the approved sequence or obtain approval under the gate above.

## Server creation and connection readiness

Treat creation, configuration, verification and serving as separate transitions. Preserve the approved server-page flow: create the disabled MCP server, open its Setup page, then configure its connection there.

Before declaring missing connection details a creation blocker, trace the existing create/update APIs, disabled-state gates and working provider flows. A required URL at creation does not establish that it must be the final connection URL. Check whether an approved temporary endpoint can be replaced through existing operations while the server stays unconfigured and disabled.

For a temporary endpoint, verify it cannot be probed, used for OAuth or connection testing, or enabled before replacement. Check reloads, direct routes, Settings and gateway entry points; a disabled wizard button alone is insufficient. Keep catalog authentication metadata accurate.

If existing contracts cannot safely preserve the approved sequence, identify the exact missing contract and ask before expanding scope. An API constraint does not authorize moving setup into Catalog or adding pre-creation fields or steps. Tests must exercise the real Catalog Add → MCP server page handoff, not merely a standalone wizard.

## Reuse map

Inspect referenced paths on the active branch.

| Existing surface                                                                                                    | Reuse for                                                                     |
| ------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| `client/dashboard/src/pages/mcp/x/setup/ProviderSetupPage.tsx`                                                      | Page shell, header, step rail, footer, completion to Overview                 |
| `client/dashboard/src/pages/catalog/provider-setup/ProviderTasks.tsx`, `shared.tsx`, `state.ts`, `provider-data.ts` | Provider tasks, shared controls, branching and invalidation                   |
| `client/dashboard/src/pages/catalog/provider-setup/ConnectionTask.tsx`                                              | Dependent action rows, inline failures and correction links                   |
| `client/dashboard/src/pages/catalog/provider-setup/setup-context.ts`                                                | Shared setup state and live status types                                      |
| `client/dashboard/src/pages/mcp/x/setup/useProviderConnection.ts`                                                   | Real credential save, OAuth, verification and stale-operation protection      |
| `client/dashboard/src/pages/mcp/x/setup/provider-setup.ts`                                                          | Exact endpoint matching, installation handoff and non-secret browser progress |
| `server/internal/remotesessions/SETUP.md`                                                                           | Backend contract and identity boundaries                                      |

Preserve provider-specific requirements while sharing layout and actions. Follow `frontend` for components, tokens and implementation standards.

## Integrate real behavior

Read the backend contract before adapting actions. Reuse supported shared operations and generated bindings. Obtain actual callbacks, selected endpoints and provider scopes from their authoritative sources. Persist the selected endpoint as the actual upstream URL. Verify provider-specific OAuth requirements; another provider's settings are not evidence.

Preserve secret-free reads/progress, invalidation when app, environment, server or setup path changes, permission-change authorization invalidation, inline recovery, and rejection of stale asynchronous results. Returning from an OAuth popup is not verified sign-in. Completion requires the real connection result and opens Overview.

Surface unresolved product decisions and missing capabilities as separate scope decisions. Continue independent work whose contracts are established, without implying end-to-end verification or simulating success.

## Verify the extension

Check every issue criterion against code and observed behavior. In the signed-in dashboard, exercise the intended first-time entry and follow the approved sequence to completion, including intermediate controls and handoffs. A direct Setup URL or an already-installed server does not establish that initial experience; report partial coverage explicitly. Cover supported new/existing app paths, callback/scopes and completion gating. Test loading, failure, retry, interrupted progress, invalidation and other identity modes where the change affects them. Spot-check affected providers after shared changes. Use focused tests, dashboard type-check/lint/knip and `gram-playwright-cli`, including a narrow viewport.

Report the exact click path observed and any unverified entry points; claim similarity only for the interactions actually compared. Report controlled-response tests separately from live provider authorization. Assess Platform MCP, Command K and demo-seed implications using their repository skills. Before handoff, compare the diff to the intended base and account for every changed surface. Commit, publish, change Linear, or attach a demo only within the user's authorization.

## Observed mistakes to avoid

- Rebuilding approved HTML while existing provider React code sits in a previous worktree: locate and reuse that code first.
- Carrying all providers into one provider issue: transfer only the requested provider and necessary shared integration.
- Reporting simulated or controlled success as a live connection: label the evidence accurately.
- Claiming equivalent onboarding from shared components or a later screen: compare the user’s first-time progression end to end.
