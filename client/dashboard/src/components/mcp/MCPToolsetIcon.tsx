import { useCatalogIconMap } from "../sources/sources-hooks";
import { useLatestDeployment } from "@gram/client/react-query/latestDeployment.js";
import type { ToolsetEntry } from "@gram/client/models/components/toolsetentry.js";
import { useMemo } from "react";
import { cn } from "@/lib/utils";
import { Network } from "lucide-react";

export function MCPToolsetIcon({
  toolset,
  className = "h-12 w-12",
  fallbackClassName = "h-8 w-8",
}: {
  toolset: ToolsetEntry;
  className?: string;
  fallbackClassName?: string;
}): JSX.Element {
  const catalogIconMap = useCatalogIconMap();
  const { data: deploymentResult } = useLatestDeployment();

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

  return externalMcpLogoUrl ? (
    <img
      src={externalMcpLogoUrl}
      alt={toolset.name}
      className={cn("object-contain", className)}
    />
  ) : (
    <Network className={cn("text-muted-foreground", fallbackClassName)} />
  );
}
