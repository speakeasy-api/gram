import { RequireScope } from "@/components/require-scope";
import { AgentSessionsContent } from "@/components/observe/AgentSessionsContent";

export default function ChatLogs() {
  return (
    <RequireScope scope="project:read" level="page">
      <AgentSessionsContent />
    </RequireScope>
  );
}
