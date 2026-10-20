import { useProject, useUser } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import type { Dashboard } from "@gram/client/models/components/dashboard.js";

/**
 * Whether the viewer may change a dashboard: its creator can, and so can
 * anyone with write access to the project, as the server enforces. Anyone
 * else can still duplicate it, which makes a copy they own.
 */
export function useCanEditDashboard(): (dashboard: Dashboard) => boolean {
  const user = useUser();
  const project = useProject();
  const { hasScope } = useRBAC();
  const canWrite = hasScope("project:write", project.id);
  return (dashboard) => canWrite || dashboard.createdByUserId === user.id;
}
