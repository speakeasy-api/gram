import { createDismissedCtaStore } from "@/hooks/useDismissedCtaStore";

const startedStore = createDismissedCtaStore("gram-project-guide-started");

function projectGuideScope(
  orgSlug: string | undefined,
  projectSlug: string | undefined,
): string | undefined {
  if (!orgSlug || !projectSlug) return undefined;
  return `${orgSlug}:${projectSlug}`;
}

export function useProjectGuideStarted(
  orgSlug: string | undefined,
  projectSlug: string | undefined,
): boolean {
  return startedStore.useDismissed(projectGuideScope(orgSlug, projectSlug));
}

export function markProjectGuideStarted(
  orgSlug: string,
  projectSlug: string,
): void {
  const scope = projectGuideScope(orgSlug, projectSlug);
  if (scope) startedStore.write(scope, true);
}
