import { WorkbenchPage } from "@/components/page-templates";
import { LogsTools } from "@/components/observe/LogsTools";
import { ObserveViewTabs } from "@/components/observe/ObserveViewTabs";

export function LogsRoot(): JSX.Element {
  return (
    <WorkbenchPage scope="org:admin" tabs={<ObserveViewTabs active="logs" />}>
      <LogsTools />
    </WorkbenchPage>
  );
}
