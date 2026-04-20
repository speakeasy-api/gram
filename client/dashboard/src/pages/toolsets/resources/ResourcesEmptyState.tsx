import { EmptyState } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@speakeasy-api/moonshine";
import { ToolsetsGraphic } from "../ToolsetsEmptyState";

export function ResourcesEmptyState({
  onAddResources,
}: {
  onAddResources: () => void;
}) {
  const cta = (
    <RequireScope scope="mcp:write" level="section">
      <Button size="sm" onClick={onAddResources}>
        ADD RESOURCES
      </Button>
    </RequireScope>
  );

  return (
    <EmptyState
      heading="No resources yet"
      description="MCP resources can be created through Gram Functions."
      nonEmptyProjectCTA={cta}
      graphic={<ToolsetsGraphic />}
      graphicClassName="scale-90"
    />
  );
}
