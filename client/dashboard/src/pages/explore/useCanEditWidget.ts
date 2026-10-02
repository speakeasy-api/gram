import { useProject, useUser } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import type { Widget } from "@gram/client/models/components/widget.js";

/**
 * Whether the viewer may change a widget: its creator can, and so can anyone
 * with write access to the project, as the server enforces. Anyone else can
 * still duplicate it, which is how a widget is shared.
 */
export function useCanEditWidget(): (widget: Widget) => boolean {
  const user = useUser();
  const project = useProject();
  const { hasScope } = useRBAC();
  const canWrite = hasScope("project:write", project.id);
  return (widget) => canWrite || widget.createdByUserId === user.id;
}
