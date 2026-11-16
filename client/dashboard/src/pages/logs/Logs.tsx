import { WorkbenchPage } from "@/components/page-templates";
import { LogsTools } from "@/components/observe/LogsTools";
import { useProject } from "@/contexts/Auth";

export function LogsRoot(): JSX.Element {
  const project = useProject();
  return (
    // Every member opens Tool Logs: without log:read the server narrows the
    // rows to their own sessions and their agents, and the page says so.
    <WorkbenchPage scope="project:read" resourceId={project.id}>
      <LogsTools />
    </WorkbenchPage>
  );
}
