import { useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { ServiceError } from "@gram/client/models/errors/serviceerror";
import { useOrgMemoryOverview } from "@gram/client/react-query/orgMemoryOverview.js";

/** The window the memory overview (and so this probe) covers. */
export const ORG_MEMORY_OVERVIEW_DAYS = 30;

export type OrgMemoryStatus = "loading" | "enabled" | "disabled";

function isNotFound(error: Error | null): boolean {
  if (!error) return false;
  if (error instanceof ServiceError) return error.statusCode === 404;
  return (
    "statusCode" in error &&
    (error as { statusCode: unknown }).statusCode === 404
  );
}

/**
 * Whether Organizational Memory is on for this organization and the caller
 * can read it on the current project. The server decides: memory follows the
 * staff admin Business Memory Extraction setting, and every memory endpoint
 * answers 404 while it is off. The overview is the probe because it is cheap
 * and isn't audited; the memory page reads the same cached result.
 *
 * Errors other than 404 count as enabled, so an outage shows the page's error
 * state rather than silently hiding the tab.
 */
export function useOrgMemoryStatus(): {
  status: OrgMemoryStatus;
  overview: ReturnType<typeof useOrgMemoryOverview>;
} {
  const project = useProject();
  const { hasAnyScope, isLoading: grantsLoading } = useRBAC();
  const canRead = hasAnyScope(["memory:read"], project.id);
  const overview = useOrgMemoryOverview(
    { days: ORG_MEMORY_OVERVIEW_DAYS },
    undefined,
    { enabled: canRead, retry: false, throwOnError: false },
  );

  let status: OrgMemoryStatus;
  if (grantsLoading) status = "loading";
  else if (!canRead) status = "disabled";
  else if (overview.isPending) status = "loading";
  else if (isNotFound(overview.error)) status = "disabled";
  else status = "enabled";
  return { status, overview };
}

/** Shorthand for {@link useOrgMemoryStatus} when only "show it?" matters. */
export function useOrgMemoryEnabled(): boolean {
  return useOrgMemoryStatus().status === "enabled";
}
