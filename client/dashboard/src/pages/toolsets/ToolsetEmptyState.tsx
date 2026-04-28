import { EmptyState } from "@/components/page-layout";
import { Button } from "@speakeasy-api/moonshine";
import { ToolsetsGraphic } from "./ToolsetsEmptyState";

export function ToolsetEmptyState({
  onAddTools,
}: {
  toolsetSlug: string;
  onAddTools?: () => void;
}) {
  const cta = onAddTools ? (
    <Button size="sm" onClick={onAddTools}>
      ADD TOOLS
    </Button>
  ) : undefined;

  return (
    <EmptyState
      heading="No tools yet"
      description="Add some tools to get started."
      nonEmptyProjectCTA={cta}
      graphic={<ToolsetsGraphic />}
      graphicClassName="scale-90"
    />
  );
}
