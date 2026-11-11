import {
  StatTile,
  StatTileGroup,
  StatTileSkeleton,
} from "@/components/chart/stat-tile";
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
      <section>
        <div className="flex flex-col gap-6">
          {/* The header shows the URL; only its absence needs saying here. */}
          {!mcpUrl && !isLoadingEndpoints ? (
            <Text muted small>
              No address yet. Add one in Settings so clients can connect.
            </Text>
          ) : null}

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
                  servable === rows.length ? undefined : `${servable} servable`
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
      </section>

      <GatewayMembersSection metaMcpServer={metaMcpServer} />

      <GatewayActivitySection
        scope={{
          kind: "gateway",
          metaMcpServerId: metaMcpServer.id,
          memberRows: rows,
          isLoadingMembers: isLoading,
        }}
      />
    </>
  );
}
