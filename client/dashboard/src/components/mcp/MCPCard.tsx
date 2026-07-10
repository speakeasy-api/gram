import { CopyButton } from "@/components/ui/copy-button";
import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { useMcpUrl } from "@/hooks/useToolsetUrl";
import { useRoutes } from "@/routes";
import { MCPStatusIndicator } from "./MCPStatusIndicator";
import { ToolsetEntry } from "@gram/client/models/components/toolsetentry.js";
import { useLatestDeployment } from "@gram/client/react-query/latestDeployment.js";
import {
  AlertTriangleIcon,
  ArrowRight,
  Link2,
  Network,
  Package,
} from "lucide-react";
import { useMemo } from "react";
import { useNavigate } from "react-router";
import {
  useCatalogIconMap,
  useExternalMcpOAuthConfigStatus,
} from "../sources/sources-hooks";
import { ToolCollectionBadge } from "../tool-collection-badge";
import { Badge, Button } from "@/components/ui/moonshine";

export function MCPCard({ toolset }: { toolset: ToolsetEntry }): JSX.Element {
  const routes = useRoutes();
  const navigate = useNavigate();
  const { installPageUrl } = useMcpUrl(toolset);
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
      void navigate(`${routes.mcp.details.href(toolset.slug)}#authentication`);
    } else {
      routes.mcp.details.goTo(toolset.slug);
    }
  };

  const installSourceTooltip = toolset.origin?.registrySpecifier
    ? `Installed from ${toolset.origin.registrySpecifier}`
    : undefined;

  // External MCP "proxy" servers can't enumerate their tools until a user
  // authenticates against them, so hide the misleading "No Tools" badge and
  // surface the visible (non-proxy) tools only.
  const visibleToolNames = toolset.tools
    .filter((t) => !(t.type === "externalmcp" && t.name.endsWith(":proxy")))
    .map((t) => t.name);
  const isExternalMcpProxy = toolset.tools.some(
    (t) => t.type === "externalmcp" && t.name.endsWith(":proxy"),
  );

  return (
    <Card
      className="cursor-pointer"
      onClick={handleClick}
      overlay={
        oauthStatus === "required-unconfigured" && (
          <div className="absolute bottom-3.5 left-1/2 z-10 -translate-x-1/2">
            <Badge variant="warning">
              <Badge.LeftIcon>
                <AlertTriangleIcon />
              </Badge.LeftIcon>
              <Badge.Text>OAuth Required</Badge.Text>
            </Badge>
          </div>
        )
      }
      icon={
        externalMcpLogoUrl ? (
          <img
            src={externalMcpLogoUrl}
            alt={toolset.name}
            className="h-12 w-12 object-contain"
          />
        ) : (
          <Network className="text-muted-foreground h-8 w-8" />
        )
      }
    >
      {/* Header row with name */}
      <div className="mb-2 flex items-start justify-between gap-2">
        <Type
          variant="subheading"
          as="div"
          className="text-md group-hover:text-primary flex-1 truncate transition-colors"
          title={toolset.name}
        >
          {toolset.name}
        </Type>
        <div className="flex items-center gap-1">
          {installPageUrl && (
            <CopyButton
              text={installPageUrl}
              size="icon-sm"
              icon={Link2}
              tooltip="Copy install page URL"
            />
          )}
          {installSourceTooltip && (
            <SimpleTooltip tooltip={installSourceTooltip}>
              <Button
                type="button"
                variant="tertiary"
                size="sm"
                aria-label={installSourceTooltip}
                onClick={(e) => e.stopPropagation()}
              >
                <Package className="text-muted-foreground group-hover:text-foreground h-4 w-4" />
              </Button>
            </SimpleTooltip>
          )}
          <ToolCollectionBadge
            toolNames={visibleToolNames}
            emptyLabel={isExternalMcpProxy ? null : undefined}
          />
        </div>
      </div>

      {/* Footer row with status indicator and open link */}
      <div className="mt-auto flex items-center justify-between gap-2 pt-2">
        <MCPStatusIndicator
          mcpEnabled={toolset.mcpEnabled}
          mcpIsPublic={toolset.mcpIsPublic}
        />
        {oauthStatus === "required-unconfigured" ? (
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
    </Card>
  );
}

export function MCPCardSkeleton(): JSX.Element {
  return (
    <Card>
      <div className="mb-2 flex items-start justify-between gap-2">
        <Skeleton className="h-5 w-2/3" />
        <Skeleton className="h-5 w-10" />
      </div>
      <div className="mt-auto flex items-center justify-between gap-2 pt-2">
        <div className="flex items-center gap-2">
          <Skeleton className="h-2.5 w-2.5 rounded-full" />
          <Skeleton className="h-3.5 w-12" />
        </div>
        <Skeleton className="h-3.5 w-10" />
      </div>
    </Card>
  );
}
