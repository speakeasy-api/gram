import { EmptyState } from "@/components/page-layout";
import { Button } from "@speakeasy-api/moonshine";
import { useRoutes } from "@/routes";
import { ToolsetsGraphic } from "./ToolsetsEmptyState";

export function ToolsetEmptyState({ toolsetSlug }: { toolsetSlug: string }) {
  const routes = useRoutes();

  const cta = (
    <routes.toolsets.toolset.update.Link params={[toolsetSlug]}>
      <Button size="sm">
        ADD TOOLS
      </Button>
    </routes.toolsets.toolset.update.Link>
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
