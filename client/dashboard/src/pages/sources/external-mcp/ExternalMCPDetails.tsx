import { Page } from "@/components/page-layout";
import {
  ExternalMCPServerCard,
  ExternalMCPServerCardLoading,
} from "@/components/sources/ExternalMCPServerCard";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { useRoutes } from "@/routes";
import {
  useLatestDeployment,
  useListToolsets,
} from "@gram/client/react-query/index.js";
import { Badge } from "@speakeasy-api/moonshine";
import { useMemo } from "react";
import { Navigate, useParams } from "react-router";

export default function ExternalMCPDetails() {
  const { sourceSlug } = useParams<{
    sourceSlug: string;
  }>();
  const routes = useRoutes();

  const { data: deployment, isLoading: isLoadingDeployment } =
    useLatestDeployment();

  // Find the specific external MCP server from the deployment
  const source = useMemo(() => {
    if (!deployment?.deployment) return null;

    return deployment.deployment.externalMcps?.find(
      (mcp) => mcp.slug === sourceSlug,
    );
  }, [deployment, sourceSlug]);

  const { data: toolsets, isLoading: isLoadingToolsets } = useListToolsets();

  // Find the toolset that uses this external MCP source
  const associatedToolset = useMemo(() => {
    if (!toolsets?.toolsets || !source) return undefined;

    return toolsets.toolsets.find((t) =>
      t.toolUrns?.includes(`tools:externalmcp:${source.slug}:proxy`),
    );
  }, [toolsets, source]);

  // If source not found, redirect to home
  if (!isLoadingDeployment && !source) {
    return <Navigate to={routes.home.href()} replace />;
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{ [sourceSlug || ""]: source?.name }}
          skipSegments={["externalmcp"]}
        />
      </Page.Header>

      <Page.Body>
        {/* Header Section with Title and Actions */}
        <div className="flex items-center justify-between mb-4 h-10">
          <div className="flex items-center gap-2">
            <Heading variant="h2" className="normal-case">
              {source?.name || sourceSlug}
            </Heading>
            <Badge variant="neutral">External MCP</Badge>
          </div>
        </div>

        <div className="space-y-6">
          {/* Source Metadata Card */}
          <div className="rounded-lg border bg-card p-6">
            <Type as="h2" className="text-lg font-semibold mb-4">
              Source Information
            </Type>
            <div className="grid grid-cols-2 gap-4">
              <div>
                <Type className="text-sm text-muted-foreground mb-1">Name</Type>
                <Type className="font-medium">{source?.name}</Type>
              </div>
              <div>
                <Type className="text-sm text-muted-foreground mb-1">Slug</Type>
                <Type className="font-medium">{source?.slug}</Type>
              </div>
              <div>
                <Type className="text-sm text-muted-foreground mb-1">Type</Type>
                <Type className="font-medium">External MCP</Type>
              </div>
              <div>
                <Type className="text-sm text-muted-foreground mb-1">
                  Registry ID
                </Type>
                <Type className="font-medium">{source?.registryId}</Type>
              </div>
              <div className="col-span-2">
                <Type className="text-sm text-muted-foreground mb-1">
                  Server Specifier
                </Type>
                <Type className="font-medium">
                  {source?.registryServerSpecifier}
                </Type>
              </div>
              <div>
                <Type className="text-sm text-muted-foreground mb-1">
                  Deployment
                </Type>
                {deployment?.deployment?.id ? (
                  <routes.deployments.deployment.Link
                    params={[deployment.deployment.id]}
                    className="font-medium hover:underline"
                  >
                    {deployment.deployment.id.slice(0, 8)}
                  </routes.deployments.deployment.Link>
                ) : (
                  <Type className="font-medium">None</Type>
                )}
              </div>
            </div>
          </div>

          {/* MCP Server Relationship Card */}
          {isLoadingToolsets ? (
            <ExternalMCPServerCardLoading />
          ) : associatedToolset !== undefined ? (
            <ExternalMCPServerCard toolset={associatedToolset} />
          ) : null}
        </div>
      </Page.Body>
    </Page>
  );
}
