import { InlineEmptyState } from "@/components/inline-empty-state";
import { Button } from "@/components/ui/Button";
import { useRoutes } from "@/routes";

export function DeploymentsEmptyState(): JSX.Element {
  const routes = useRoutes();

  return (
    <InlineEmptyState
      icon="history"
      heading="No deployments yet"
      description="Connect an MCP server from the catalog to create your first deployment and generate tools."
      action={
        <routes.catalog.Link>
          <Button size="sm">Browse catalog</Button>
        </routes.catalog.Link>
      }
    />
  );
}
