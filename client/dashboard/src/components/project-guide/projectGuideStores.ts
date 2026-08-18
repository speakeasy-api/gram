import { createDismissedCtaStore } from "@/hooks/useDismissedCtaStore";

// Two independent per-project flags on the same localStorage-backed store
// helper the onboarding CTA uses, so both surfaces re-render on a write
// without a shared React context.
//
// `started` exists because the guide's own steps create an MCP server and a
// risk policy — the very things the gate reads as "this project has something".
// Without it, the gate would flip to the dashboard mid-journey and unmount the
// guide before the user reaches the payoff. It is written on the user's first
// real interaction with a journey, never on render: on-render would flag every
// empty project on first load and stop distinguishing started from seen.
const startedStore = createDismissedCtaStore("gram-project-guide-started");
const dismissedStore = createDismissedCtaStore("gram-project-guide-dismissed");

export function useProjectGuideStarted(
  projectSlug: string | undefined,
): boolean {
  return startedStore.useDismissed(projectSlug);
}

export function useProjectGuideDismissed(
  projectSlug: string | undefined,
): boolean {
  return dismissedStore.useDismissed(projectSlug);
}

export function markProjectGuideStarted(projectSlug: string): void {
  startedStore.write(projectSlug, true);
}

/**
 * Dismissing clears the started flag too: the guide is off screen, so there is
 * no in-progress run left for the gate to protect.
 */
export function dismissProjectGuide(projectSlug: string): void {
  dismissedStore.write(projectSlug, true);
  startedStore.write(projectSlug, false);
}

export function restoreProjectGuide(projectSlug: string): void {
  dismissedStore.write(projectSlug, false);
}
