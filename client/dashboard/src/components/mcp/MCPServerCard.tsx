import { useIconDither } from "@/components/icon-dither";
import { Card } from "@/components/ui/Card";
import { Text } from "@/components/ui/Text";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { useRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { Badge } from "@/components/ui/Badge";
import { AlertTriangleIcon, ArrowRight, Package } from "lucide-react";
import { Link } from "react-router";
import { SourceMcpIcon } from "@/components/sources/SourceCard";
import { useHostedMcpOAuthConfigStatus } from "../sources/sources-hooks";
import { ExternalMcpIcon } from "./MCPToolsetIcon";
import {
  hostedToolCountLabel,
  MCP_SERVER_KIND_LABEL,
  mcpServerDisplayName,
  mcpServerHref,
  type McpServerKind,
} from "./mcp-server-kind";

/** One card per mcp_servers row in the /mcp grid, whatever its backend. */
export function MCPServerCard({
  server,
  kind,
}: {
  server: McpServer;
  kind: McpServerKind;
}): JSX.Element {
  const routes = useRoutes();
  const { canvasRef, start, stop } = useIconDither();
  const hosted = kind === "hosted";
  const toolset = hosted ? server.toolset : undefined;
  const oauthStatus = useHostedMcpOAuthConfigStatus({
    toolset,
    userSessionIssuerId: server.userSessionIssuerId,
  });
  const needsOAuth = oauthStatus === "required-unconfigured";
  const name = mcpServerDisplayName(server);
  const installSourceTooltip = toolset?.registrySpecifier
    ? `Installed from ${toolset.registrySpecifier}`
    : undefined;

  return (
    <Link
      to={mcpServerHref(routes, server, oauthStatus)}
      onMouseEnter={start}
      onMouseLeave={stop}
      className="focus-visible:ring-ring block h-full no-underline focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none"
    >
      <Card.Entity
        iconRailClassName="isolate"
        iconTileClassName="icon-hover-pulse"
        overlay={
          <>
            <canvas
              ref={canvasRef}
              aria-hidden="true"
              className="pointer-events-none absolute inset-0 -z-10 size-full"
            />
            {needsOAuth && (
              <div className="absolute bottom-3.5 left-1/2 z-10 -translate-x-1/2">
                <Badge variant="warning">
                  <Badge.LeftIcon>
                    <AlertTriangleIcon />
                  </Badge.LeftIcon>
                  <Badge.Text>OAuth Required</Badge.Text>
                </Badge>
              </div>
            )}
          </>
        }
        icon={
          hosted ? (
            <ExternalMcpIcon
              externalMcpSlug={toolset?.externalMcpSlugs[0]}
              alt={name}
            />
          ) : (
            <SourceMcpIcon
              mcpServerId={server.id}
              className="h-8 w-8 object-contain"
            />
          )
        }
      >
        <div className="mb-2 flex items-start justify-between gap-2">
          <Text
            variant="subheading"
            as="div"
            className="text-md group-hover:text-primary flex-1 truncate transition-colors"
            title={name}
          >
            {name}
          </Text>
          {installSourceTooltip && (
            <SimpleTooltip tooltip={installSourceTooltip}>
              <span aria-label={installSourceTooltip} className="p-1">
                <Package className="text-muted-foreground group-hover:text-foreground h-4 w-4" />
              </span>
            </SimpleTooltip>
          )}
        </div>

        {/* Description, else origin or tool count; the slug when there is no summary. */}
        {toolset ? (
          <Text small muted className="mt-1 line-clamp-2">
            {toolset.description?.trim() ||
              (toolset.registrySpecifier
                ? `From ${toolset.registrySpecifier}`
                : hostedToolCountLabel(toolset))}
          </Text>
        ) : (
          <Text small muted className="mt-1 truncate font-mono">
            {server.slug ?? "No slug yet"}
          </Text>
        )}

        <div className="mt-auto flex items-center justify-between gap-2 pt-2">
          <div className="flex items-center gap-2">
            <Badge variant="neutral">
              <Badge.Text>{MCP_SERVER_KIND_LABEL[kind]}</Badge.Text>
            </Badge>
          </div>
          {needsOAuth ? (
            <div className="text-warning flex items-center gap-1 text-sm">
              <span>Set up</span>
              <ArrowRight className="h-3.5 w-3.5" />
            </div>
          ) : (
            <div className="text-muted-foreground group-hover:text-primary flex items-center gap-1 text-sm transition-colors">
              <span>Open</span>
              <ArrowRight className="h-3.5 w-3.5" />
            </div>
          )}
        </div>
      </Card.Entity>
    </Link>
  );
}

export function MCPServerCardSkeleton(): JSX.Element {
  return (
    <Card.Entity>
      <div className="mb-2 flex items-start justify-between gap-2">
        <div className="bg-muted h-5 w-2/3 animate-pulse" />
        <div className="bg-muted h-5 w-10 animate-pulse rounded-full" />
      </div>
      <div className="mt-auto flex items-center justify-between gap-2 pt-2">
        <div className="flex items-center gap-2">
          <div className="bg-muted h-2.5 w-2.5 animate-pulse rounded-full" />
          <div className="bg-muted h-3.5 w-12 animate-pulse" />
        </div>
        <div className="bg-muted h-3.5 w-10 animate-pulse" />
      </div>
    </Card.Entity>
  );
}
