import { WorkbenchPage } from "@/components/page-templates";
import { LogsTools } from "@/components/observe/LogsTools";
import { useProject } from "@/contexts/Auth";

export function LogsRoot(): JSX.Element {
  const project = useProject();
  return (
    <WorkbenchPage scope="log:read" resourceId={project.id}>
      <LogsTools />
    </WorkbenchPage>
  );
}
