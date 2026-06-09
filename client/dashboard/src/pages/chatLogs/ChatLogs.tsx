import { RequireScope } from "@/components/require-scope";
import { LogsAgentsContent } from "@/components/observe/LogsAgents";

export default function ChatLogs(): JSX.Element {
  return (
    <RequireScope scope="project:read" level="page">
      <LogsAgentsContent />
    </RequireScope>
  );
}
