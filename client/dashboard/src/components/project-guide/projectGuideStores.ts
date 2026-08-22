import { createDismissedCtaStore } from "@/hooks/useDismissedCtaStore";

const startedStore = createDismissedCtaStore("gram-project-guide-started");

export function useProjectGuideStarted(
  projectSlug: string | undefined,
): boolean {
  return startedStore.useDismissed(projectSlug);
}

export function markProjectGuideStarted(projectSlug: string): void {
  startedStore.write(projectSlug, true);
}
