import { WorkbenchPage } from "@/components/page-templates";
import { LogsTools } from "@/components/observe/LogsTools";

export function LogsRoot(): JSX.Element {
  return (
    <WorkbenchPage scope="org:admin">
      <LogsTools />
    </WorkbenchPage>
  );
}
