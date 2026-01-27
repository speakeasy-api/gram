import { Page } from "@/components/page-layout";
import {
  ExternalMCPServerCard,
  ExternalMCPServerCardLoading,
} from "@/components/sources/ExternalMCPServerCard";
import { InfoField } from "@/components/sources/InfoField";
import { ExternalMCPIllustration } from "@/components/sources/SourceCardIllustrations";
import { useCatalogIconMap } from "@/components/sources/Sources";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { useRoutes } from "@/routes";
import {
  useLatestDeployment,
  useListToolsets,
} from "@gram/client/react-query/index.js";
import { Badge, Stack } from "@speakeasy-api/moonshine";
import { FileCode, Package, Server, Tag } from "lucide-react";
import { useMemo } from "react";
import { Navigate, useParams } from "react-router";

export default function ExternalMCPDetails() {
  const { sourceSlug } = useParams<{
    sourceSlug: string;
  }>();
  const routes = useRoutes();
  const catalogIconMap = useCatalogIconMap();

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

      <Page.Body fullWidth noPadding>
        {/* Hero Header with Illustration - full width */}
        <div className="relative w-full h-64 overflow-hidden">
          <ExternalMCPIllustration
            logoUrl={catalogIconMap.get(source?.registryServerSpecifier || "")}
            name={source?.name}
            className="scale-200"
          />

          {/* Overlay for text readability */}
          <div className="absolute inset-0 bg-linear-to-t from-foreground/50 via-foreground/20 to-transparent" />
          <div className="absolute bottom-0 left-0 right-0 px-8 py-8 max-w-[1270px] mx-auto w-full">
            <Stack gap={2}>
              <div className="flex items-center gap-3 ml-1">
                <Heading variant="h1" className="text-background">
                  {source?.name || sourceSlug}
                </Heading>
                <Badge variant="neutral">
                  <Badge.Text>External MCP</Badge.Text>
                </Badge>
              </div>
              <div className="flex items-center gap-2 ml-1">
                <Type className="max-w-2xl truncate text-background/70!">
                  {source?.slug}
                </Type>
              </div>
            </Stack>
          </div>
        </div>

        {/* Content Section */}
        <div className="max-w-[1270px] mx-auto px-8 py-8 w-full">
          <div className="space-y-6">
          {/* Source Metadata Card */}
          <div className="rounded-lg border bg-card overflow-hidden">
            <div className="border-b bg-surface-secondary/30 px-6 py-4">
              <Type as="h2" className="text-lg flex items-center gap-2">
                <FileCode className="h-5 w-5 text-muted-foreground" />
                Source Information
              </Type>
            </div>
            <div className="p-6">
              <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                <InfoField
                  icon={Tag}
                  label="Name"
                  value={source?.name}
                />

                <InfoField
                  icon={Tag}
                  label="Slug"
                  value={<Type className="font-mono text-sm">{source?.slug}</Type>}
                />

                <InfoField
                  icon={Package}
                  label="Type"
                  value="External MCP"
                />

                <InfoField
                  icon={Server}
                  label="Registry ID"
                  value={<Type className="font-mono text-sm">{source?.registryId}</Type>}
                />

                <InfoField
                  icon={Server}
                  label="Server Specifier"
                  value={
                    <Type className="font-mono text-sm break-all">
                      {source?.registryServerSpecifier}
                    </Type>
                  }
                  className="md:col-span-2"
                />

                <InfoField
                  icon={Package}
                  label="Deployment"
                  value={
                    deployment?.deployment?.id ? (
                      <routes.deployments.deployment.Link
                        params={[deployment.deployment.id]}
                        className="hover:underline text-primary"
                      >
                        {deployment.deployment.id.slice(0, 8)}
                      </routes.deployments.deployment.Link>
                    ) : (
                      <Type className="text-muted-foreground">None</Type>
                    )
                  }
                />
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
        </div>
      </Page.Body>
    </Page>
  );
}
