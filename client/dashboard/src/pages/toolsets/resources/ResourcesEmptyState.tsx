import { EmptyState } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/button";
import { ToolsetsGraphic } from "../ToolsetsEmptyState";

export function ResourcesEmptyState({
  onAddResources,
}: {
  onAddResources: () => void;
}): JSX.Element {
  const cta = (
    <RequireScope scope="mcp:write" level="component">
      <Button size="sm" onClick={onAddResources}>
        ADD RESOURCES
      </Button>
    </RequireScope>
  );

  return (
    <EmptyState
      heading="No resources yet"
      description="MCP resources can be created through functions."
      nonEmptyProjectCTA={cta}
      graphic={<ToolsetsGraphic />}
      graphicClassName="scale-90"
    />
  );
}
