import { WorkbenchPage } from "@/components/page-templates";
import { LogsAgentsContent } from "@/components/observe/LogsAgents";

export default function ChatLogs(): JSX.Element {
  return (
    <WorkbenchPage scope="org:admin">
      <LogsAgentsContent />
    </WorkbenchPage>
  );
}
