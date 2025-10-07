import { EmptyState } from "@/components/page-layout";
import { Button } from "@speakeasy-api/moonshine";
import { ToolsetsGraphic } from "./ToolsetsEmptyState";

export function ToolsetEmptyState({
  onAddTools,
}: {
  toolsetSlug: string;
  onAddTools: () => void;
}) {
  const cta = (
    <Button size="sm" onClick={onAddTools}>
      ADD TOOLS
    </Button>
  );

  return (
    <EmptyState
      heading="This toolset is empty"
      description="Add some tools to your toolset to get started."
      nonEmptyProjectCTA={cta}
      graphic={<ToolsetsGraphic />}
      graphicClassName="scale-90"
    />
  );
}
