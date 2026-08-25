import { InlineEmptyState } from "@/components/inline-empty-state";
import { Button } from "@/components/ui/Button";
import { useRoutes } from "@/routes";
import { Link } from "react-router";

export function DeploymentsEmptyState(): JSX.Element {
  const routes = useRoutes();

  return (
    <InlineEmptyState
      icon="history"
      heading="No deployments yet"
      description="Connect an MCP server from the catalog to create your first deployment and generate tools."
      action={
        <Button asChild size="sm">
          <Link to={routes.catalog.href()}>Browse catalog</Link>
        </Button>
      }
    />
  );
}
