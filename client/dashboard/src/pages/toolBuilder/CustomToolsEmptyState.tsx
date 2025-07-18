import { EmptyState } from "@/components/page-layout";
import { Button } from "@/components/ui/button";
import { ToolsetsGraphic } from "../toolsets/ToolsetsEmptyState";

export function CustomToolsEmptyState({
  onCreateCustomTool,
}: {
  onCreateCustomTool: () => void;
}) {
  const cta = (
    <Button size="sm" onClick={onCreateCustomTool}>
      Create a custom tool
    </Button>
  );

  return (
    <EmptyState
      heading="No custom tools yet"
      description="Custom tools allow you to easily combine multiple tool calls into a single, reusable tool."
      nonEmptyProjectCTA={cta}
      graphic={<ToolsetsGraphic />}
      graphicClassName="scale-90"
    />
  );
}
