import {
  StatTile,
  StatTileGroup,
  StatTileSkeleton,
} from "@/components/chart/stat-tile";
import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/Button";
import { CopyButton } from "@/components/ui/CopyButton";
import { Text } from "@/components/ui/Text";
import { useResolvedMcpServerUrl } from "@/hooks/useToolsetUrl";
import { useRoutes } from "@/routes";
import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import type { MetaMcpServer } from "@gram/client/models/components/metamcpserver.js";
import { ArrowRight } from "lucide-react";
import { MembersStatusTable } from "./GatewayMembersTab";
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
  const routes = useRoutes();
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
              <div className="border-border bg-card flex items-center gap-2 border px-3 py-2">
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

      <Page.Section>
        <Page.Section.Title area="" className="text-display-xs">
          Members
        </Page.Section.Title>
        <Page.Section.CTA>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => routes.mcp.gateway.members.goTo(metaMcpServer.id)}
          >
            <Button.Text>Manage members</Button.Text>
            <Button.RightIcon>
              <ArrowRight className="size-4" />
            </Button.RightIcon>
          </Button>
        </Page.Section.CTA>
        <Page.Section.Description>
          The order agents see in list_servers. Status reflects what the backend
          can attest; live upstream health lands with the proxied runtime.
        </Page.Section.Description>
        <Page.Section.Body>
          <MembersStatusTable rows={rows} isLoading={isLoading} />
        </Page.Section.Body>
      </Page.Section>
    </>
  );
}
