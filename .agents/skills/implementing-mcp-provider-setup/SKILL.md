---
name: implementing-mcp-provider-setup
description: Use when adding, integrating, changing, or reviewing a provider's MCP catalog setup page, onboarding wizard, app-registration instructions, credential form, or setup save/sign-in/test flow in the dashboard.
---

# Implementing MCP provider setup

Extend the approved first-time provider setup flow from the user’s intended entry through successful connection. Reuse the working implementation and actions; sharing components alone does not prove the complete experience matches.

## Establish scope before editing

1. Read the full provider issue, acceptance criteria and reviewed decisions through connected Linear. Search Linear before asking for identifiers. Separate provider work from shared-backend and validation issues. Identify unavailable requirements rather than inventing them.
2. Inspect Git status, branch/base and worktrees. Locate the established working flow (such as Slack/GitHub) and requested provider implementation, including dirty reference files.
3. Briefly state provider/issue, base, reuse files and unresolved requirements. Keep the new diff provider-scoped; preserve inherited providers and fixes.

**Ready to implement:** issue, base, existing implementation, first-time entry sequence and provider contracts are established. Keep unresolved branches pending; continue authorized integration without asking for the same approval again.

## Source precedence and UI approval

Use this order:

1. The user's current instructions and explicit approvals.
2. The provider issue and its reviewed decisions.
3. Existing provider React implementation plus the implemented shared setup shell.
4. Approved design lineage, only to resolve a specific gap or check fidelity.

Find previous work through `git worktree list`, repository search and linked branches/PRs. Read committed or dirty reference files without switching or editing that checkout. Adapt provider-specific pieces onto the current implementation so inherited fixes survive.

When design evidence is needed, follow `setup-design-lineage.json`. Approved appearance does not establish provider outcomes. `designs/research/` is excluded without explicit authorization; missing files do not authorize substitute UI.

**Approval gate:** before introducing a different layout, navigation, step sequence, interaction pattern, new control, or provider behavior not established by the issue and approved implementation, show the concrete proposed difference and obtain explicit approval. Silence is not approval. Investigate contracts and continue independent work while waiting; keep the dependent UI unchanged. This gate takes precedence over generic frontend guidance to choose a design when the user is unavailable. Reusing approved UI, wiring real actions, and fixing defects within that behavior do not require renewed design approval. If sources conflict, identify the conflict and ask which governs that part.

## First-time entry and progression

Before editing, trace the intended user’s first-time entry through every intermediate screen and decision to completion. For catalog onboarding this includes the **Add** action users actually take; other entry points matter when the task affects them. Compare the actual handlers and user actions against the established working flow and approved provider React implementation.

Keep each provider choice at its approved step. Preserve approved identity choices and unaffected installation paths. An existing generic component does not authorize extra steps or relocating fields; restore the approved sequence or obtain approval under the gate above.

## Reuse map

| Existing surface                                                                                                    | Reuse for                                                                     |
| ------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| `client/dashboard/src/pages/mcp/x/setup/ProviderSetupPage.tsx`                                                      | Page shell, header, step rail, footer, completion to Overview                 |
| `client/dashboard/src/pages/catalog/provider-setup/ProviderTasks.tsx`, `shared.tsx`, `state.ts`, `provider-data.ts` | Provider tasks, shared controls, branching and invalidation                   |
| `client/dashboard/src/pages/catalog/provider-setup/ConnectionTask.tsx`                                              | Dependent action rows, inline failures and correction links                   |
| `client/dashboard/src/pages/catalog/provider-setup/setup-context.ts`                                                | Shared setup state and live status types                                      |
| `client/dashboard/src/pages/mcp/x/setup/useProviderConnection.ts`                                                   | Real credential save, OAuth, verification and stale-operation protection      |
| `client/dashboard/src/pages/mcp/x/setup/provider-setup.ts`                                                          | Exact endpoint matching, installation handoff and non-secret browser progress |
| `server/internal/remotesessions/SETUP.md`                                                                           | Backend contract and identity boundaries                                      |

Keep provider-specific tasks: GitHub's organization step is not universal, and Slack's shorter sequence does not remove another provider's requirements. Follow `frontend` for components, tokens and implementation standards; preserve the approved flow.

## Integrate real behavior

Read the backend contract before adapting actions. Reuse supported shared operations and generated bindings. Obtain actual callbacks, selected endpoints and provider scopes from their authoritative sources. Persist the selected endpoint as the actual upstream URL. Verify provider-specific OAuth requirements; another provider's settings are not evidence.

Preserve secret-free reads/progress, invalidation when app, environment, server or setup path changes, permission-change authorization invalidation, inline recovery, and rejection of stale asynchronous results. Returning from an OAuth popup is not verified sign-in. Completion requires the real connection result and opens Overview.

Provider-side activation, support handoff or credential requirements represented only by simulated outcomes need a verified implementation contract. Surface unresolved product decisions rather than creating success controls or guessing backend support. A missing backend capability is a separate scope decision.

## Verify the extension

Check every issue criterion against code and observed behavior. In the signed-in dashboard, exercise the intended first-time entry and follow the approved sequence to completion, including intermediate controls and handoffs. A direct Setup URL or an already-installed server does not establish that initial experience; report partial coverage explicitly. Cover the required app-registration paths, callback/scopes and completion gating. Test retry, interrupted progress, invalidation and other identity modes where the change affects them. Spot-check affected providers after shared changes. Use focused tests, dashboard type-check/lint/knip and `gram-playwright-cli`, including a narrow viewport.

Report the exact click path observed and any unverified entry points; claim similarity only for the interactions actually compared. Report controlled-response tests separately from live provider authorization. Assess Platform MCP, Command K and demo-seed implications using their repository skills. Before handoff, compare the diff to the intended base and account for every changed surface. Commit, publish, change Linear, or attach a demo only within the user's authorization.

## Observed mistakes to avoid

- Rebuilding approved HTML while existing provider React code sits in a previous worktree: locate and reuse that code first.
- Carrying all providers into one provider issue: transfer only the requested provider and necessary shared integration.
- Taking a new-flow design exercise as permission to redesign the shared shell: use the approval gate above.
- Reporting simulated or controlled success as a live connection: label the evidence accurately.
- Claiming equivalent onboarding from shared components or a later screen: compare the user’s first-time progression end to end.
