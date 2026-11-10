---
name: implementing-mcp-provider-setup
description: Use when adding, integrating, changing, or reviewing a provider's MCP catalog setup page, onboarding wizard, app-registration instructions, credential form, or setup save/sign-in/test flow in the dashboard.
---

# Implementing MCP provider setup

The implemented MCP setup page is the approved UI pattern. Extend that implementation for the requested provider. An approved mock is design evidence, not a reason to rebuild UI that already exists.

## Establish scope before editing

1. Read the provider's full Linear issue, acceptance criteria, and relevant decisions. Search connected Linear before asking for an issue number. Separate the provider issue from shared-backend work and end-to-end validation issues. If the issue cannot be read, identify that gap; do not invent its requirements.
2. Inspect Git status, the requested branch/base, and existing worktrees. Locate the current Slack/GitHub integration and any existing implementation for the requested provider, including uncommitted code in a reference worktree. Inspect reference checkouts read-only and preserve their changes. A source checkout is not a patch to copy wholesale.
3. State the provider/issue, intended base, implementation files to reuse, and any unresolved requirement in a short update. Provider-only scope applies to the new diff; preserve inherited providers and fixes.

**Ready to implement:** the issue has been read, the source implementation and base have been identified, and the intended changes fit the approved flow, and required provider contracts are verified. Leave unresolved branches unimplemented pending decisions. Continue ordinary integration without asking for the same approval again.

## Source precedence and UI approval

Use this order:

1. The user's current instructions and explicit approvals.
2. The provider issue and its reviewed decisions.
3. Existing provider React implementation plus the implemented shared setup shell.
4. Approved design lineage, only to resolve a specific gap or check fidelity.

Find previous work through `git worktree list`, repository search, and the issue's linked branches/PRs. If implementation lives on another branch, inspect its committed files without switching or editing that checkout. Inspect relevant dirty source files read-only, whether discovered or identified by the user. Adapt the provider-specific pieces onto the current implementation so inherited fixes survive.

When design evidence is needed, locate `setup-design-lineage.json` and follow its current approved references. Preserve the distinction between approved appearance and simulated provider outcomes. `designs/research/` is excluded unless the user explicitly authorizes it. Missing files do not authorize inventing a substitute UI.

**Approval gate:** before introducing a different layout, navigation, step sequence, interaction pattern, new control, or provider behavior not established by the issue and approved implementation, show the concrete proposed difference and obtain explicit approval. Silence is not approval. Investigate contracts and continue independent work while waiting; keep the dependent UI unchanged. This gate takes precedence over generic frontend guidance to choose a design when the user is unavailable. Reusing approved UI, wiring real actions, and fixing defects within that behavior do not require renewed design approval. If sources conflict, identify the conflict and ask which governs that part.

## Reuse map

Paths are relative to the repository; inspect their current contents rather than assuming every provider is present on the active branch.

| Existing surface                                                                                                    | Reuse for                                                                     |
| ------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| `client/dashboard/src/pages/mcp/x/setup/ProviderSetupPage.tsx`                                                      | Page shell, header, step rail, footer, completion to Overview                 |
| `client/dashboard/src/pages/catalog/provider-setup/ProviderTasks.tsx`, `shared.tsx`, `state.ts`, `provider-data.ts` | Provider tasks, shared controls, branching and invalidation                   |
| `client/dashboard/src/pages/catalog/provider-setup/ConnectionTask.tsx`                                              | Dependent action rows, inline failures and correction links                   |
| `client/dashboard/src/pages/catalog/provider-setup/setup-context.ts`                                                | Shared setup state and live status types                                      |
| `client/dashboard/src/pages/mcp/x/setup/useProviderConnection.ts`                                                   | Real credential save, OAuth, verification and stale-operation protection      |
| `client/dashboard/src/pages/mcp/x/setup/provider-setup.ts`                                                          | Exact endpoint matching, installation handoff and non-secret browser progress |
| `server/internal/remotesessions/SETUP.md`                                                                           | Backend contract and identity boundaries                                      |

Keep provider-specific requirements inside that pattern: GitHub's organization step is not a requirement for every provider; Slack's four-step sequence is not a template that removes another provider's required tasks. Preserve the existing Identity modal and installation paths outside the issue's requested handoff. Follow the `frontend` skill for components, tokens and implementation standards, without restarting design exploration for the approved flow.

## Integrate real behavior

Read the backend contract before adapting actions. Reuse supported shared operations and generated bindings. Obtain actual callbacks, selected endpoints and provider scopes from their authoritative sources. Persist the selected endpoint as the actual upstream URL. Verify provider-specific OAuth requirements; another provider's settings are not evidence.

Preserve secret-free reads/progress, invalidation when app, environment, server or setup path changes, permission-change authorization invalidation, inline recovery, and rejection of stale asynchronous results. Returning from an OAuth popup is not verified sign-in. Completion requires the real connection result and opens Overview.

Provider-side activation, support handoff or credential requirements represented only by simulated outcomes need a verified implementation contract. Surface unresolved product decisions rather than creating success controls or guessing backend support. A missing backend capability is a separate scope decision.

## Verify the extension

Check every issue criterion against code and observed behavior. Cover new/existing app paths, actual callback and selected scopes, invalidation, loading/failure/retry, interrupted progress, and completion gating. Spot-check existing providers when shared code changes. Use focused tests and the repository's dashboard type-check, lint and knip checks as appropriate; inspect the actual signed-in dashboard with `gram-playwright-cli`, including a narrow viewport.

Report controlled-response tests separately from live provider authorization. Assess Platform MCP, Command K and demo-seed implications using their repository skills. Before handoff, compare the diff to the intended base and account for every changed surface. Commit, publish, change Linear, or attach a demo only within the user's authorization.

## Observed mistakes to avoid

- Rebuilding approved HTML while existing provider React code sits in a previous worktree: locate and reuse that code first.
- Carrying all providers into one provider issue: transfer only the requested provider and necessary shared integration.
- Taking a new-flow design exercise as permission to redesign the shared shell: use the approval gate above.
- Reporting simulated or controlled success as a live connection: label the evidence accurately.
