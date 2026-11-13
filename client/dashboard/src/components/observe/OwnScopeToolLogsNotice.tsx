import { RequestScopeAccess } from "@/components/require-scope";
import { Alert } from "@/components/ui/Alert";
import { useProject } from "@/contexts/Auth";

/**
 * Above the Tool Logs list for a member without log:read: the server narrows
 * their reads to their own activity, and this says so before the rows do.
 */
export function OwnScopeToolLogsNotice(): JSX.Element {
  const project = useProject();
  return (
    // The quiet variant: this is the page's normal state for most members,
    // not a warning, so it earns a hairline and an icon but no color.
    <Alert title={null} className="shrink-0">
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
        <span>
          Showing your sessions and your agents&apos; activity. Ask an admin for
          the View tool logs permission to see every member&apos;s.
        </span>
        <RequestScopeAccess scopes={["log:read"]} resourceId={project.id} />
      </div>
    </Alert>
  );
}
