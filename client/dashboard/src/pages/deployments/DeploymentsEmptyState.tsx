import { EmptyState } from "@/components/page-layout";
import { Button } from "@/components/ui/Button";
import { useRoutes } from "@/routes";
import { ToolsetsGraphic } from "../toolsets/ToolsetsEmptyState";

export function DeploymentsEmptyState(): JSX.Element {
  const routes = useRoutes();

  return (
    <EmptyState
      heading="No deployments yet"
      description="Browse the catalog to connect an MCP server. Adding a source creates a deployment and generates tools for your project."
      nonEmptyProjectCTA={
        <routes.catalog.Link>
          <Button size="sm">Browse catalog</Button>
        </routes.catalog.Link>
      }
      graphic={<ToolsetsGraphic />}
      graphicClassName="scale-90"
    />
  );
}
