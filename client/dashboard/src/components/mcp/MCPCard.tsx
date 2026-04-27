import { CopyButton } from "@/components/ui/copy-button";
import { DotCard } from "@/components/ui/dot-card";
import { Button } from "@/components/ui/button";
import { Type } from "@/components/ui/type";
import { useMcpUrl } from "@/hooks/useToolsetUrl";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { ToolsetEntry } from "@gram/client/models/components";
import { useLatestDeployment } from "@gram/client/react-query";
import {
  AlertTriangleIcon,
  ArrowRight,
  Link2,
  Network,
  Package,
} from "lucide-react";
import { useMemo } from "react";
import {
  useCatalogIconMap,
  useExternalMcpOAuthConfigStatus,
} from "../sources/sources-hooks";
import { ToolCollectionBadge } from "../tool-collection-badge";
import { Badge } from "../ui/badge";

export function MCPCard({ toolset }: { toolset: ToolsetEntry }) {
  const routes = useRoutes();
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
      routes.mcp.details.authentication.goTo(toolset.slug);
    } else {
      routes.mcp.details.goTo(toolset.slug);
    }
  };

  // Pulse indicator for status
  const getStatusConfig = () => {
    if (!toolset.mcpEnabled) {
      return {
        color: "bg-red-500",
        pulseColor: "bg-red-400",
        label: "Disabled",
      };
    }
    return {
      color: "bg-green-500",
      pulseColor: "bg-green-400",
      label: toolset.mcpIsPublic ? "Public" : "Private",
    };
  };

  const status = getStatusConfig();
  const installSourceTooltip = toolset.origin?.registrySpecifier
    ? `Installed from ${toolset.origin.registrySpecifier}`
    : undefined;

  const statusIndicator = (
    <div className="flex items-center gap-2">
      <div className="relative flex h-2.5 w-2.5">
        {toolset.mcpEnabled && (
          <span
            className={cn(
              "absolute inline-flex h-full w-full animate-ping rounded-full opacity-75",
              status.pulseColor,
            )}
          />
        )}
        <span
          className={cn(
            "relative inline-flex h-2.5 w-2.5 rounded-full",
            status.color,
          )}
        />
      </div>
      <Type variant="small" muted>
        {status.label}
      </Type>
    </div>
  );

  return (
    <DotCard
      className="cursor-pointer"
      onClick={handleClick}
      overlay={
        oauthStatus === "required-unconfigured" && (
          <div className="absolute bottom-3.5 left-1/2 z-10 -translate-x-1/2">
            <Badge
              variant="outline"
              className="border-warning-foreground bg-warning text-warning-foreground text-xs backdrop-blur-sm"
            >
              <AlertTriangleIcon />
              OAuth Required
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
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              tooltip={installSourceTooltip}
              aria-label={installSourceTooltip}
              onClick={(e) => e.stopPropagation()}
            >
              <Package className="text-muted-foreground group-hover:text-foreground h-4 w-4" />
            </Button>
          )}
          <ToolCollectionBadge toolNames={toolset.tools.map((t) => t.name)} />
        </div>
      </div>

      {/* Footer row with status indicator and open link */}
      <div className="mt-auto flex items-center justify-between gap-2 pt-2">
        {statusIndicator}
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
    </DotCard>
  );
}

export function MCPCardSkeleton() {
  return (
    <DotCard>
      <div className="mb-2 flex items-start justify-between gap-2">
        <div className="bg-muted h-5 w-2/3 animate-pulse rounded" />
        <div className="bg-muted h-5 w-10 animate-pulse rounded-full" />
      </div>
      <div className="mt-auto flex items-center justify-between gap-2 pt-2">
        <div className="flex items-center gap-2">
          <div className="bg-muted h-2.5 w-2.5 animate-pulse rounded-full" />
          <div className="bg-muted h-3.5 w-12 animate-pulse rounded" />
        </div>
        <div className="bg-muted h-3.5 w-10 animate-pulse rounded" />
      </div>
    </DotCard>
  );
}
