import { RequireScope } from "@/components/require-scope";
import { InsightsContent } from "@/components/observe/InsightsContent";

export default function ObservabilityOverview() {
  return (
    <RequireScope scope="project:read" level="page">
      <InsightsContent />
    </RequireScope>
  );
}
