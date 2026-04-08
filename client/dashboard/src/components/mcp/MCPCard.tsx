import { CopyButton } from "@/components/ui/copy-button";
import { DotCard } from "@/components/ui/dot-card";
import { Type } from "@/components/ui/type";
import { useMcpUrl } from "@/hooks/useToolsetUrl";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { ToolsetEntry } from "@gram/client/models/components";
import { useLatestDeployment } from "@gram/client/react-query";
import { ArrowRight, Link2, Network } from "lucide-react";
import { useMemo } from "react";
import { useCatalogIconMap } from "../sources/Sources";
import { ToolCollectionBadge } from "../tool-collection-badge";

export function MCPCard({ toolset }: { toolset: ToolsetEntry }) {
  const routes = useRoutes();
  const { installPageUrl } = useMcpUrl(toolset);
  const catalogIconMap = useCatalogIconMap();
  const { data: deploymentResult } = useLatestDeployment();

  // Check if this toolset uses an external MCP and get its info
  const externalMcpInfo = useMemo(() => {
    const externalMcpUrn = toolset.toolUrns?.find((urn) =>
      urn.includes(":externalmcp:"),
    );
    if (!externalMcpUrn) return null;

    const parts = externalMcpUrn.split(":");
    const slug = parts[2];
    if (!slug) return null;

    const externalMcps = deploymentResult?.deployment?.externalMcps ?? [];
    const matchingMcp = externalMcps.find((mcp) => mcp.slug === slug);

    const logoUrl = matchingMcp?.registryServerSpecifier
      ? catalogIconMap.get(matchingMcp.registryServerSpecifier)
      : undefined;

    return { slug, logoUrl };
  }, [toolset.toolUrns, catalogIconMap, deploymentResult]);

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

  const statusIndicator = (
    <div className="flex items-center gap-2">
      <div className="relative flex h-2.5 w-2.5">
        {toolset.mcpEnabled && (
          <span
            className={cn(
              "animate-ping absolute inline-flex h-full w-full rounded-full opacity-75",
              status.pulseColor,
            )}
          />
        )}
        <span
          className={cn(
            "relative inline-flex rounded-full h-2.5 w-2.5",
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
      onClick={() => routes.mcp.details.goTo(toolset.slug)}
      icon={
        externalMcpInfo?.logoUrl ? (
          <img
            src={externalMcpInfo.logoUrl}
            alt={toolset.name}
            className="w-12 h-12 object-contain"
          />
        ) : (
          <Network className="w-8 h-8 text-muted-foreground" />
        )
      }
    >
      {/* Header row with name */}
      <div className="flex items-start justify-between gap-2 mb-2">
        <Type
          variant="subheading"
          as="div"
          className="truncate flex-1 text-md group-hover:text-primary transition-colors"
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
          <ToolCollectionBadge toolNames={toolset.tools.map((t) => t.name)} />
        </div>
      </div>

      {/* Footer row with status indicator and open link */}
      <div className="flex items-center justify-between gap-2 mt-auto pt-2">
        {statusIndicator}
        <div className="flex items-center gap-1 text-muted-foreground group-hover:text-primary transition-colors text-sm">
          <span>Open</span>
          <ArrowRight className="w-3.5 h-3.5" />
        </div>
      </div>
    </DotCard>
  );
}

export function MCPCardSkeleton() {
  return (
    <DotCard>
      <div className="flex items-start justify-between gap-2 mb-2">
        <div className="h-5 w-2/3 bg-muted rounded animate-pulse" />
        <div className="h-5 w-10 bg-muted rounded-full animate-pulse" />
      </div>
      <div className="flex items-center justify-between gap-2 mt-auto pt-2">
        <div className="flex items-center gap-2">
          <div className="h-2.5 w-2.5 rounded-full bg-muted animate-pulse" />
          <div className="h-3.5 w-12 bg-muted rounded animate-pulse" />
        </div>
        <div className="h-3.5 w-10 bg-muted rounded animate-pulse" />
      </div>
    </DotCard>
  );
}
