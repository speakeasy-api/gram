import { RequireScope } from "@/components/require-scope";
import { InsightsHooksContent } from "@/components/observe/InsightsHooksContent";

export default function HooksPage() {
  return (
    <RequireScope scope="project:read" level="page">
      <InsightsHooksContent />
    </RequireScope>
  );
}
