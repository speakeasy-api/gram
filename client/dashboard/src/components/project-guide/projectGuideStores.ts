import {
  createDismissedCtaStore,
  createScopedStorageStore,
} from "@/hooks/useDismissedCtaStore";

const startedStore = createDismissedCtaStore("gram-project-guide-started");
const mcpServerStore = createScopedStorageStore<string | undefined>(
  "gram-project-guide-mcp-server",
  undefined,
  (stored) => stored ?? undefined,
  (value) => value ?? null,
);

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

export function useProjectGuideMcpServerSelection(
  orgSlug: string | undefined,
  projectSlug: string | undefined,
): string | undefined {
  return mcpServerStore.useValue(projectGuideScope(orgSlug, projectSlug));
}

export function markProjectGuideMcpServerSelected(
  orgSlug: string,
  projectSlug: string,
  registrySpecifier: string,
): void {
  const scope = projectGuideScope(orgSlug, projectSlug);
  if (scope) mcpServerStore.write(scope, registrySpecifier);
}
