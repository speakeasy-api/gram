import { useIconConfetti } from "@/components/icon-confetti";
import { Card } from "@/components/ui/Card";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { useRoutes } from "@/routes";
import { ToolsetEntry } from "@gram/client/models/components/toolsetentry.js";
import { useLatestDeployment } from "@gram/client/react-query/latestDeployment.js";
import { AlertTriangleIcon, ArrowRight, Network, Package } from "lucide-react";
import { useMemo } from "react";
import {
  useCatalogIconMap,
  useExternalMcpOAuthConfigStatus,
} from "../sources/sources-hooks";
import { Badge } from "@/components/ui/Badge";

export function MCPCard({ toolset }: { toolset: ToolsetEntry }): JSX.Element {
  const routes = useRoutes();
  const { canvasRef, start, stop } = useIconConfetti();
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

  const installSourceTooltip = toolset.origin?.registrySpecifier
    ? `Installed from ${toolset.origin.registrySpecifier}`
    : undefined;

  return (
    <div onMouseEnter={start} onMouseLeave={stop} className="h-full">
      <Card.Entity
        className="cursor-pointer"
        onClick={handleClick}
        iconRailClassName="isolate"
        iconTileClassName="icon-hover-pulse"
        overlay={
          <>
            <canvas
              ref={canvasRef}
              aria-hidden="true"
              className="pointer-events-none absolute inset-0 -z-10 size-full"
            />
            {oauthStatus === "required-unconfigured" && (
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
          <Text
            variant="subheading"
            as="div"
            className="text-md group-hover:text-primary flex-1 truncate transition-colors"
            title={toolset.name}
          >
            {toolset.name}
          </Text>
          <div className="flex items-center gap-1">
            {installSourceTooltip && (
              <Button
                type="button"
                variant="tertiary"
                size="sm"
                tooltip={installSourceTooltip}
                aria-label={installSourceTooltip}
                onClick={(e) => e.stopPropagation()}
              >
                <Package className="text-muted-foreground group-hover:text-foreground h-4 w-4" />
              </Button>
            )}
          </div>
        </div>

        {/* What this server is, in the author's words where they wrote any —
            otherwise what it carries, which at least tells them apart. */}
        <Text small muted className="mt-1 line-clamp-2">
          {toolset.description?.trim() ||
            (toolset.origin?.registrySpecifier
              ? `From ${toolset.origin.registrySpecifier}`
              : `${toolset.tools.length} ${toolset.tools.length === 1 ? "tool" : "tools"}`)}
        </Text>

        {/* Footer row with status indicator and open link */}
        <div className="mt-auto flex items-center justify-between gap-2 pt-2">
          <div className="flex items-center gap-2">
            {/* Hosted here means Speakeasy serves the tools itself, as against
                the remote and gateway cards beside it. */}
            <Badge variant="neutral">
              <Badge.Text>Hosted</Badge.Text>
            </Badge>
          </div>
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
      </Card.Entity>
    </div>
  );
}

export function MCPCardSkeleton(): JSX.Element {
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
