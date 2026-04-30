import { RequireScope } from "@/components/require-scope";
import { LogsAgentsContent } from "@/components/observe/LogsAgents";

export default function ChatLogs() {
  return (
    <RequireScope scope="project:read" level="page">
      <LogsAgentsContent />
    </RequireScope>
  );
}
