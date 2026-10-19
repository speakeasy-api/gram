import { SourceMcpIcon } from "@/components/sources/SourceCard";
import { Badge } from "@/components/ui/Badge";
import { CopyButton } from "@/components/ui/CopyButton";
import { DotRow } from "@/components/ui/DotRow";
import { Text } from "@/components/ui/Text";
import { useResolvedMcpServerUrl } from "@/hooks/useToolsetUrl";
import { useRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import type { MetaMcpServer } from "@gram/client/models/components/metamcpserver.js";
import { ExternalMcpIcon } from "./MCPToolsetIcon";
import {
  hostedToolCountLabel,
  MCP_SERVER_KIND_LABEL,
  mcpServerDisplayName,
  mcpServerHref,
  type McpServerKind,
} from "./mcp-server-kind";
import { AlertTriangleIcon, Link2, Network } from "lucide-react";
import { useHostedMcpOAuthConfigStatus } from "../sources/sources-hooks";

/** Table rows of the /mcp listing; gateways and servers share Name, Kind, Address, Contents. */

const CELL = "px-3 py-3";

function NameCell({
  name,
  children,
}: {
  name: string;
  children?: React.ReactNode;
}): JSX.Element {
  return (
    <td className={CELL}>
      <div className="flex items-center gap-2">
        <Text
          variant="subheading"
          as="div"
          className="group-hover:text-primary min-w-0 truncate text-sm transition-colors"
          title={name}
        >
          {name}
        </Text>
        {children}
      </div>
    </td>
  );
}

function AddressCell({ url }: { url: string | undefined }): JSX.Element {
  return (
    <td className={`max-w-xs ${CELL}`}>
      {url ? (
        // The copy button must sit above the row's click handler.
        <div
          className="relative z-20 flex items-center gap-1.5"
          onClick={(e) => e.stopPropagation()}
        >
          <Text small muted className="truncate font-mono text-xs">
            {url.replace(/^https?:\/\//, "")}
          </Text>
          <CopyButton
            text={url}
            size="sm"
            icon={Link2}
            tooltip="Copy MCP URL"
          />
        </div>
      ) : (
        <Text small muted>
          —
        </Text>
      )}
    </td>
  );
}

/** A Gateway Endpoint: a meta MCP server fronting member servers. */
export function GatewayTableRow({
  gateway,
  endpoints,
  isLoadingEndpoints,
}: {
  gateway: MetaMcpServer;
  endpoints: McpEndpoint[];
  isLoadingEndpoints: boolean;
}): JSX.Element {
  const routes = useRoutes();
  const memberCount = gateway.memberCount ?? 0;
  const { mcpUrl } = useResolvedMcpServerUrl(endpoints, isLoadingEndpoints);
  return (
    <DotRow
      href={routes.mcp.gateway.overview.href(gateway.id)}
      ariaLabel={`Open gateway ${gateway.name}`}
      icon={<Network className="text-muted-foreground h-5 w-5" />}
    >
      <NameCell name={gateway.name} />
      <td className={CELL}>
        <Badge variant="neutral">Gateway</Badge>
      </td>
      <AddressCell url={mcpUrl} />
      <td className={CELL}>
        <Text small muted>
          {`${memberCount} ${memberCount === 1 ? "member" : "members"}`}
        </Text>
      </td>
    </DotRow>
  );
}

/** One row per mcp_servers row, whatever its backend. */
export function MCPServerTableRow({
  server,
  kind,
}: {
  server: McpServer;
  kind: McpServerKind;
}): JSX.Element {
  const routes = useRoutes();
  const hosted = kind === "hosted";
  const toolset = hosted ? server.toolset : undefined;
  const oauthStatus = useHostedMcpOAuthConfigStatus({
    toolset,
    userSessionIssuerId: server.userSessionIssuerId,
  });
  const name = mcpServerDisplayName(server);
  return (
    <DotRow
      href={mcpServerHref(routes, server, oauthStatus)}
      ariaLabel={`Open MCP server ${name}`}
      icon={
        hosted ? (
          <ExternalMcpIcon
            externalMcpSlug={toolset?.externalMcpSlugs[0]}
            alt={name}
            className="h-6 w-6"
            fallbackClassName="h-5 w-5"
          />
        ) : (
          <SourceMcpIcon
            mcpServerId={server.id}
            className="h-5 w-5 object-contain"
          />
        )
      }
    >
      <NameCell name={name}>
        {oauthStatus === "required-unconfigured" && (
          <Badge variant="warning">
            <Badge.LeftIcon>
              <AlertTriangleIcon />
            </Badge.LeftIcon>
            <Badge.Text>OAuth Required</Badge.Text>
          </Badge>
        )}
      </NameCell>
      <td className={CELL}>
        <Badge variant="neutral">{MCP_SERVER_KIND_LABEL[kind]}</Badge>
      </td>
      {server.primaryEndpointUrl || hosted ? (
        <AddressCell url={server.primaryEndpointUrl} />
      ) : (
        <td className={CELL}>
          <Text
            small
            muted
            className="truncate font-mono text-xs"
            title={server.slug ?? undefined}
          >
            {server.slug ?? "No slug yet"}
          </Text>
        </td>
      )}
      <td className={CELL}>
        <Text small muted>
          {hostedToolCountLabel(toolset) ?? "—"}
        </Text>
      </td>
    </DotRow>
  );
}

export function MCPTableRowSkeleton(): JSX.Element {
  return (
    <DotRow>
      <td className={CELL}>
        <div className="bg-muted h-4 w-2/3 animate-pulse" />
      </td>
      <td className={CELL}>
        <div className="bg-muted h-5 w-14 animate-pulse rounded-full" />
      </td>
      <td className={CELL}>
        <div className="bg-muted h-3.5 w-40 animate-pulse" />
      </td>
      <td className={CELL}>
        <div className="bg-muted h-3.5 w-12 animate-pulse" />
      </td>
    </DotRow>
  );
}
