import { RequireScope } from "@/components/require-scope";
import { LogsContent } from "@/components/observe/LogsContent";

export default function LogsPage() {
  return (
    <RequireScope scope="project:read" level="page">
      <LogsContent />
    </RequireScope>
  );
}
