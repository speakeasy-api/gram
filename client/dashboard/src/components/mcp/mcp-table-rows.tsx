import { SourceMcpIcon } from "@/components/sources/SourceCard";
import { Badge } from "@/components/ui/Badge";
import { CopyButton } from "@/components/ui/CopyButton";
import { DotRow } from "@/components/ui/DotRow";
import { Text } from "@/components/ui/Text";
import { useMcpUrl } from "@/hooks/useToolsetUrl";
import { mcpServerRouteParam } from "@/lib/sources";
import { useRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { MetaMcpServer } from "@gram/client/models/components/metamcpserver.js";
import type { ToolsetEntry } from "@gram/client/models/components/toolsetentry.js";
import { useLatestDeployment } from "@gram/client/react-query/latestDeployment.js";
import { AlertTriangleIcon, Link2, Network } from "lucide-react";
import { useMemo } from "react";
import {
  useCatalogIconMap,
  useExternalMcpOAuthConfigStatus,
} from "../sources/sources-hooks";

/**
 * The table rows of the /mcp listing: one per backend kind, mirroring the
 * three cards of the grid view. All three fill the same four columns — Name,
 * Kind, Address, Contents — so the kinds line up in one table.
 *
 * TODO(AGE-1902): collapse into one row once Hosted (toolset-backed) servers
 * also source from mcp_servers.
 */

const CELL = "px-3 py-3";

function NameCell({
  name,
  title,
  children,
}: {
  name: string;
  title?: string;
  children?: React.ReactNode;
}): JSX.Element {
  return (
    <td className={CELL}>
      <div className="flex items-center gap-2">
        <Text
          variant="subheading"
          as="div"
          className="group-hover:text-primary min-w-0 truncate text-sm transition-colors"
          title={title ?? name}
        >
          {name}
        </Text>
        {children}
      </div>
    </td>
  );
}

/** A Hosted server: a toolset Speakeasy serves itself. */
export function MCPTableRow({
  toolset,
}: {
  toolset: ToolsetEntry;
}): JSX.Element {
  const routes = useRoutes();
  const { url: mcpUrl } = useMcpUrl(toolset);
  const catalogIconMap = useCatalogIconMap();
  const { data: deploymentResult } = useLatestDeployment();
  const oauthStatus = useExternalMcpOAuthConfigStatus(toolset.slug);

  const externalMcpLogoUrl = useMemo(() => {
    const externalMcpUrn = toolset.toolUrns?.find((urn) =>
      urn.includes(":externalmcp:"),
    );
    const slug = externalMcpUrn?.split(":")[2];
    if (!slug) return undefined;
    const matchingMcp = deploymentResult?.deployment?.externalMcps?.find(
      (mcp) => mcp.slug === slug,
    );
    return matchingMcp?.registryServerSpecifier
      ? catalogIconMap.get(matchingMcp.registryServerSpecifier)
      : undefined;
  }, [toolset.toolUrns, catalogIconMap, deploymentResult]);

  const handleClick = () => {
    if (oauthStatus === "required-unconfigured") {
      routes.mcp.details.authentication.goTo(toolset.slug);
    } else {
      routes.mcp.details.goTo(toolset.slug);
    }
  };

  // Same reading as MCPCard: a proxy-only toolset can't count its tools until
  // someone signs in, so neither "1 tool" nor "0 tools" would be true.
  const isProxyOnly =
    toolset.tools.length > 0 &&
    toolset.tools.every(
      (tool) => tool.type === "externalmcp" && tool.name.endsWith(":proxy"),
    );
  const toolCount = toolset.tools.filter(
    (tool) => !(tool.type === "externalmcp" && tool.name.endsWith(":proxy")),
  ).length;

  return (
    <DotRow
      onClick={handleClick}
      icon={
        externalMcpLogoUrl ? (
          <img
            src={externalMcpLogoUrl}
            alt={toolset.name}
            className="h-6 w-6 object-contain"
          />
        ) : (
          <Network className="text-muted-foreground h-5 w-5" />
        )
      }
    >
      <NameCell name={toolset.name}>
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
        <Badge variant="neutral">Hosted</Badge>
      </td>
      <td className={`max-w-xs ${CELL}`}>
        {mcpUrl ? (
          // The copy button must sit above the row's click handler.
          <div
            className="relative z-20 flex items-center gap-1.5"
            onClick={(e) => e.stopPropagation()}
          >
            <Text small muted className="truncate font-mono text-xs">
              {mcpUrl.replace(/^https?:\/\//, "")}
            </Text>
            <CopyButton
              text={mcpUrl}
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
      <td className={CELL}>
        <Text small muted>
          {isProxyOnly
            ? "Tools listed once you sign in"
            : `${toolCount} ${toolCount === 1 ? "tool" : "tools"}`}
        </Text>
      </td>
    </DotRow>
  );
}

/** A Gateway Endpoint: a meta MCP server fronting member servers. */
export function GatewayTableRow({
  gateway,
}: {
  gateway: MetaMcpServer;
}): JSX.Element {
  const routes = useRoutes();
  const memberCount = gateway.memberCount ?? 0;
  return (
    <DotRow
      onClick={() => routes.mcp.gateway.overview.goTo(gateway.id)}
      icon={<Network className="text-muted-foreground h-5 w-5" />}
    >
      <NameCell name={gateway.name} />
      <td className={CELL}>
        <Badge variant="neutral">Gateway</Badge>
      </td>
      {/* A gateway's address is minted per endpoint, not per gateway, so
          there is nothing to show at the listing level. */}
      <td className={CELL}>
        <Text small muted>
          —
        </Text>
      </td>
      <td className={CELL}>
        <Text small muted>
          {`${memberCount} ${memberCount === 1 ? "member" : "members"}`}
        </Text>
      </td>
    </DotRow>
  );
}

/** A remote, tunneled, or unproxied server: an mcp_servers row. */
export function MCPServerTableRow({
  server,
}: {
  server: McpServer;
}): JSX.Element {
  const routes = useRoutes();
  const kindLabel = server.unproxiedMcpServerId
    ? "Unproxied"
    : server.tunneledMcpServerId
      ? "Tunneled"
      : "Remote";
  return (
    <DotRow
      onClick={() => routes.mcp.x.overview.goTo(mcpServerRouteParam(server))}
      icon={
        <SourceMcpIcon
          mcpServerId={server.id}
          className="h-5 w-5 object-contain"
        />
      }
    >
      <NameCell
        name={server.name || "MCP Server"}
        title={server.name ?? undefined}
      />
      <td className={CELL}>
        <Badge variant="neutral">{kindLabel}</Badge>
      </td>
      <td className={CELL}>
        <Text small muted className="truncate font-mono text-xs">
          {server.slug ?? "No slug yet"}
        </Text>
      </td>
      <td className={CELL}>
        <Text small muted>
          —
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
