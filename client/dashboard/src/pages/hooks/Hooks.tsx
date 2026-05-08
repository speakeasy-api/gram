import { RequireScope } from "@/components/require-scope";
import { InsightsToolsContent } from "@/components/observe/InsightsTools";

export default function HooksPage() {
  return (
    <RequireScope scope="project:read" level="page">
      <InsightsToolsContent />
    </RequireScope>
  );
}
