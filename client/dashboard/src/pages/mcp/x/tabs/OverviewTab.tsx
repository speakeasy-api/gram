import { CodeBlock } from "@/components/code";
import { DotCard } from "@/components/ui/dot-card";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { useMcpEndpointUrl } from "@/hooks/useToolsetUrl";
import {
  formatRemoteMcpUrlForDisplay,
  remoteMcpRouteParam,
} from "@/lib/sources";
import { useRoutes } from "@/routes";
import type {
  McpEndpoint,
  McpServer,
  ToolsetEntry,
} from "@gram/client/models/components";
import {
  useGetRemoteMcpServer,
  useListToolsets,
} from "@gram/client/react-query/index.js";
import { Badge, Button, Stack } from "@speakeasy-api/moonshine";
import { ArrowRight, Network, Plus, Wrench } from "lucide-react";
import { useMemo, type ReactNode } from "react";

export function OverviewTab({
  mcpServer,
  endpoints,
  isLoadingEndpoints,
  onShowEndpoints,
}: {
  mcpServer: McpServer | undefined;
  endpoints: McpEndpoint[];
  isLoadingEndpoints: boolean;
  onShowEndpoints: () => void;
}) {
  return (
    <div className="mx-auto w-full max-w-[1270px] space-y-8 px-8 py-8">
      <InstallPagesSection
        endpoints={endpoints}
        isLoading={isLoadingEndpoints}
        onShowEndpoints={onShowEndpoints}
      />
      {mcpServer && <SourcesSection mcpServer={mcpServer} />}

      {/* TODO(AGE-2239): wire the install-page branding affordance in once
          mcp_metadata learns about mcp_server_id. */}
    </div>
  );
}

function InstallPagesSection({
  endpoints,
  isLoading,
  onShowEndpoints,
}: {
  endpoints: McpEndpoint[];
  isLoading: boolean;
  onShowEndpoints: () => void;
}) {
  // Custom-domain endpoints render first so the more prominent customer-facing
  // URLs sit above the platform-hosted fallback.
  const sortedEndpoints = useMemo(
    () =>
      [...endpoints].sort((a, b) => {
        const aCustom = a.customDomainId ? 1 : 0;
        const bCustom = b.customDomainId ? 1 : 0;
        return bCustom - aCustom;
      }),
    [endpoints],
  );

  return (
    <section>
      <Heading variant="h4" className="mb-3">
        Client Install
      </Heading>
      <Type muted small className="mb-4">
        Share this page with your users to give simple instructions for getting
        started with your MCP in their client like Cursor or Claude Desktop.
      </Type>
      {isLoading ? (
        <Type muted small>
          Loading endpoints…
        </Type>
      ) : sortedEndpoints.length === 0 ? (
        <Stack gap={2}>
          <Type muted small>
            No endpoints configured yet.
          </Type>
          <div>
            <Button variant="secondary" onClick={onShowEndpoints}>
              <Button.LeftIcon>
                <Plus className="size-4" />
              </Button.LeftIcon>
              <Button.Text>Add Endpoint</Button.Text>
            </Button>
          </div>
        </Stack>
      ) : (
        <Stack gap={3}>
          {sortedEndpoints.map((endpoint) => (
            <InstallPageRow key={endpoint.id} endpoint={endpoint} />
          ))}
        </Stack>
      )}
    </section>
  );
}

function InstallPageRow({ endpoint }: { endpoint: McpEndpoint }) {
  const { installPageUrl } = useMcpEndpointUrl(endpoint);

  return installPageUrl ? (
    <CodeBlock copyable>{installPageUrl}</CodeBlock>
  ) : (
    <Type muted small>
      URL unavailable (custom domain still resolving).
    </Type>
  );
}

function SourcesSection({ mcpServer }: { mcpServer: McpServer }) {
  const isRemoteBacked = !!mcpServer.remoteMcpServerId;
  const isToolsetBacked = !!mcpServer.toolsetId;

  if (!isRemoteBacked && !isToolsetBacked) {
    return null;
  }

  return (
    <section>
      <Heading variant="h4" className="mb-3">
        Sources
      </Heading>
      <Type muted small className="mb-4">
        {isRemoteBacked
          ? "This MCP server is backed by a remote MCP server."
          : "This MCP server is backed by built sources."}
      </Type>
      {isRemoteBacked && mcpServer.remoteMcpServerId && (
        <RemoteSourceCard remoteMcpServerId={mcpServer.remoteMcpServerId} />
      )}
      {isToolsetBacked && mcpServer.toolsetId && (
        <ToolsetSourceCard toolsetId={mcpServer.toolsetId} />
      )}
    </section>
  );
}

function RemoteSourceCard({
  remoteMcpServerId,
}: {
  remoteMcpServerId: string;
}) {
  const routes = useRoutes();
  const { data: remoteMcpServer, isLoading } = useGetRemoteMcpServer(
    { id: remoteMcpServerId },
    undefined,
    { throwOnError: false },
  );

  if (isLoading || !remoteMcpServer) {
    return (
      <SourceSkeletonCard
        icon={<Network className="text-muted-foreground h-8 w-8" />}
      />
    );
  }

  const trimmedName = remoteMcpServer.name?.trim();
  const urlDisplay = formatRemoteMcpUrlForDisplay(remoteMcpServer.url);

  return (
    <routes.sources.source.Link
      params={["remotemcp", remoteMcpRouteParam(remoteMcpServer)]}
      className="block hover:no-underline"
    >
      <SourceCardBody
        icon={<Network className="text-muted-foreground h-8 w-8" />}
        title={trimmedName || urlDisplay}
        subtitle={trimmedName ? urlDisplay : undefined}
        badgeLabel="Remote MCP"
      />
    </routes.sources.source.Link>
  );
}

function ToolsetSourceCard({ toolsetId }: { toolsetId: string }) {
  const routes = useRoutes();
  const { data: toolsetsResult, isLoading } = useListToolsets();
  const toolset = toolsetsResult?.toolsets.find(
    (t: ToolsetEntry) => t.id === toolsetId,
  );

  if (isLoading || !toolset) {
    return (
      <SourceSkeletonCard
        icon={<Wrench className="text-muted-foreground h-8 w-8" />}
      />
    );
  }

  const displayName = toolset.name?.trim() || toolset.slug;

  return (
    <routes.mcp.details.Link
      params={[toolset.slug]}
      className="block hover:no-underline"
    >
      <SourceCardBody
        icon={<Wrench className="text-muted-foreground h-8 w-8" />}
        title={displayName}
        badgeLabel="Toolset"
      />
    </routes.mcp.details.Link>
  );
}

function SourceCardBody({
  icon,
  title,
  subtitle,
  badgeLabel,
}: {
  icon: ReactNode;
  title: string;
  subtitle?: string;
  badgeLabel: string;
}) {
  return (
    <DotCard icon={icon}>
      <div className="mb-2 flex items-start justify-between gap-2">
        <div className="min-w-0 flex-1">
          <Type
            variant="subheading"
            as="div"
            className="text-md group-hover:text-primary truncate transition-colors"
            title={title}
          >
            {title}
          </Type>
          {subtitle && (
            <Type as="div" muted small className="truncate" title={subtitle}>
              {subtitle}
            </Type>
          )}
        </div>
      </div>
      <div className="mt-auto flex items-center justify-between gap-2 pt-2">
        <Badge variant="neutral">
          <Badge.Text>{badgeLabel}</Badge.Text>
        </Badge>
        <div className="text-muted-foreground group-hover:text-primary flex items-center gap-1 text-sm transition-colors">
          <span>Open</span>
          <ArrowRight className="h-3.5 w-3.5" />
        </div>
      </div>
    </DotCard>
  );
}

function SourceSkeletonCard({ icon }: { icon: React.ReactNode }) {
  return (
    <DotCard icon={icon}>
      <div className="bg-muted mb-2 h-5 w-1/3 animate-pulse rounded" />
      <div className="mt-auto flex items-center justify-between gap-2 pt-2">
        <div className="bg-muted h-5 w-20 animate-pulse rounded-full" />
        <div className="bg-muted h-4 w-12 animate-pulse rounded" />
      </div>
    </DotCard>
  );
}
