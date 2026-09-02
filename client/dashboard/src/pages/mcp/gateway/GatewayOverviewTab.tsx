import {
  StatTile,
  StatTileGroup,
  StatTileSkeleton,
} from "@/components/chart/stat-tile";
import { Page } from "@/components/page-layout";
import { CopyButton } from "@/components/ui/CopyButton";
import { Text } from "@/components/ui/Text";
import { useResolvedMcpServerUrl } from "@/hooks/useToolsetUrl";
import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import type { MetaMcpServer } from "@gram/client/models/components/metamcpserver.js";
import { GatewayActivitySection } from "./GatewayActivitySection";
import { GatewayMembersSection } from "./GatewayMembersSection";
import { useGatewayMemberRows } from "./useGatewayMemberRows";

export function GatewayOverviewTab({
  metaMcpServer,
  endpoints,
  isLoadingEndpoints,
}: {
  metaMcpServer: MetaMcpServer;
  endpoints: McpEndpoint[];
  isLoadingEndpoints: boolean;
}): JSX.Element {
  const { mcpUrl } = useResolvedMcpServerUrl(endpoints, isLoadingEndpoints);
  const { rows, isLoading } = useGatewayMemberRows(metaMcpServer.id);

  // Only members the gateway can actually dispatch to count as served;
  // unproxied and slugless ones sit in the list permanently excluded.
  const servable = rows.filter(
    (row) =>
      row.classification === "hosted" || row.classification === "proxied",
  ).length;

  return (
    <>
      <Page.Section>
        <Page.Section.Title>{metaMcpServer.name}</Page.Section.Title>
        <Page.Section.Description>
          Agents connect to this one URL and work through list_servers,
          describe_server, describe_tools, and execute_tool instead of every
          member's full catalog.
        </Page.Section.Description>
        <Page.Section.Body>
          <div className="flex flex-col gap-6">
            {mcpUrl ? (
              <div className="border-border bg-muted/30 flex items-center gap-3 border px-4 py-3">
                <span className="border-border bg-background text-muted-foreground shrink-0 border px-1.5 py-0.5 font-mono text-[10px] tracking-widest uppercase">
                  mcp
                </span>
                <Text className="min-w-0 flex-1 truncate font-mono text-sm">
                  {mcpUrl}
                </Text>
                <CopyButton text={mcpUrl} size="xs" tooltip="Copy URL" />
              </div>
            ) : (
              <Text muted small>
                {isLoadingEndpoints
                  ? "Loading…"
                  : "No address yet. Add one in Settings so clients can connect."}
              </Text>
            )}

            <StatTileGroup>
              {isLoading ? (
                <StatTileSkeleton />
              ) : (
                <StatTile
                  title="Members"
                  value={rows.length}
                  format="number"
                  icon="network"
                  subtext={
                    servable === rows.length
                      ? undefined
                      : `${servable} servable`
                  }
                  tooltip="MCP servers this gateway fronts."
                />
              )}
              {isLoadingEndpoints ? (
                <StatTileSkeleton />
              ) : (
                <StatTile
                  title="Addresses"
                  value={endpoints.length}
                  format="number"
                  icon="link"
                  tooltip="URLs clients can connect to, including custom domains."
                />
              )}
              <StatTile
                title="Authentication"
                value={0}
                displayValue={
                  metaMcpServer.userSessionIssuerId ? "Required" : "Anonymous"
                }
                icon="shield"
                tooltip={
                  metaMcpServer.userSessionIssuerId
                    ? "Clients authenticate with Speakeasy before connecting."
                    : "Anyone who knows the URL can connect."
                }
              />
            </StatTileGroup>
          </div>
        </Page.Section.Body>
      </Page.Section>

      <GatewayActivitySection
        metaMcpServerId={metaMcpServer.id}
        memberRows={rows}
      />

      <GatewayMembersSection metaMcpServer={metaMcpServer} />
    </>
  );
}
