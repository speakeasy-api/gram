import { Badge } from "@/components/ui/Badge";
import { Card } from "@/components/ui/Card";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useActiveDeployment } from "@/hooks/toolTypes";
import { Toolset } from "@/lib/toolTypes";
import { useRoutes } from "@/routes";
import type { DeploymentExternalMCP } from "@gram/client/models/components/deploymentexternalmcp.js";
import { Server } from "lucide-react";
import type { ReactNode } from "react";
import { Link } from "react-router";

interface ServerTabContentProps {
  toolset: Toolset;
}

// An external MCP toolset carries `tools:externalmcp:<slug>:<tool>` URNs; the
// slug is how the deployment names the source.
function externalMcpSlug(toolUrns: string[] | undefined): string | undefined {
  const urn = toolUrns?.find((candidate) =>
    candidate.includes(":externalmcp:"),
  );
  return urn?.split(":")[2] || undefined;
}

function originLabel(source: DeploymentExternalMCP): string {
  return source.registryId ? "Catalog" : "External MCP";
}

function DetailField({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}) {
  return (
    <div>
      <Text small muted className="mb-1 block">
        {label}
      </Text>
      {children}
    </div>
  );
}

// Where the server came from, as the deployment records it. Shown only when
// the deployment named this server: a refused or failed read is said to be
// one, and a deployment without the server says nothing rather than
// guessing "External MCP".
function OriginFields({
  source,
  deploymentId,
  deploymentHref,
  isLoading,
  isError,
}: {
  source: DeploymentExternalMCP | undefined;
  deploymentId: string | undefined;
  deploymentHref: string | undefined;
  isLoading: boolean;
  isError: boolean;
}): JSX.Element | null {
  if (isLoading) return null;
  if (isError) {
    return (
      <DetailField label="Origin">
        <Text muted>
          Unavailable. The project's deployment could not be read.
        </Text>
      </DetailField>
    );
  }
  if (!source || !deploymentId || !deploymentHref) return null;
  return (
    <>
      <div className="flex flex-wrap gap-x-16 gap-y-4">
        <DetailField label="Created from">
          <Badge variant="neutral">
            <Badge.Text>{originLabel(source)}</Badge.Text>
          </Badge>
        </DetailField>
        <DetailField label="Origin ID">
          <Text className="font-mono text-sm break-all">
            {source.registryId || "—"}
          </Text>
        </DetailField>
      </div>
      <DetailField label="Server specifier">
        <Text className="font-mono text-sm break-all">
          {source.registryServerSpecifier || "—"}
        </Text>
      </DetailField>
      <DetailField label="Deployment">
        <Link
          to={deploymentHref}
          className="font-mono text-sm underline underline-offset-2"
        >
          {deploymentId.slice(0, 8)}
        </Link>
      </DetailField>
    </>
  );
}

export function ServerTabContent({
  toolset,
}: ServerTabContentProps): JSX.Element {
  const routes = useRoutes();
  // The page is gated on mcp:read; the deployment needs project:read, so a
  // viewer without it gets a refused read here, which must not render as
  // origin facts. Unauthorized reads never throw, so the refusal lands in
  // isError rather than the page's boundary.
  const {
    data: deploymentResult,
    isLoading: isDeploymentLoading,
    isError: isDeploymentError,
  } = useActiveDeployment({ throwOnError: false });
  const deployment = deploymentResult?.deployment;

  // Find the external MCP tool to display its metadata
  const externalMcpTool = toolset.rawTools.find(
    (t) => t.externalMcpToolDefinition !== undefined,
  );

  if (
    !externalMcpTool ||
    externalMcpTool.externalMcpToolDefinition === undefined
  ) {
    return (
      <div className="text-muted-foreground">
        No external MCP server configured.
      </div>
    );
  }

  const tool = externalMcpTool.externalMcpToolDefinition;

  // Origin details live on the deployment's externalMcps entry, not on the
  // tool, so they were only ever shown on the retired source page.
  const slug = externalMcpSlug(toolset.toolUrns);
  const source = deployment?.externalMcps?.find(
    (candidate) => candidate.slug === slug,
  );

  return (
    <Stack direction="vertical" gap={6}>
      <Card>
        <Card.Title>
          <Stack direction="horizontal" gap={3} align="center">
            <div className="bg-primary/10 flex h-10 w-10 items-center justify-center">
              <Server className="text-primary h-5 w-5" />
            </div>
            <Stack gap={1}>
              <Text variant="subheading">External MCP Server</Text>
              <Text small muted>
                {tool.slug}
              </Text>
            </Stack>
          </Stack>
        </Card.Title>
        <Card.Description>
          <Stack direction="vertical" gap={4} className="mt-4">
            <DetailField label="Remote URL">
              <Text className="font-mono text-sm">{tool.remoteUrl}</Text>
            </DetailField>
            {tool.requiresOauth && (
              <DetailField label="Authentication">
                <Text>OAuth required</Text>
              </DetailField>
            )}
            <OriginFields
              source={source}
              deploymentId={deployment?.id}
              deploymentHref={
                deployment
                  ? routes.deployments.deployment.href(deployment.id)
                  : undefined
              }
              isLoading={isDeploymentLoading}
              isError={isDeploymentError}
            />
          </Stack>
        </Card.Description>
      </Card>
    </Stack>
  );
}
