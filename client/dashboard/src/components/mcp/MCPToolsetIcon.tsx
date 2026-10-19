import { useCatalogIconMap } from "../sources/sources-hooks";
import { useLatestDeployment } from "@gram/client/react-query/latestDeployment.js";
import type { ToolsetEntry } from "@gram/client/models/components/toolsetentry.js";
import { useMemo } from "react";
import { cn } from "@/lib/utils";
import { Network } from "lucide-react";

export function MCPToolsetIcon({
  toolset,
  className,
  fallbackClassName,
}: {
  toolset: ToolsetEntry;
  className?: string;
  fallbackClassName?: string;
}): JSX.Element {
  const externalMcpSlug = toolset.toolUrns
    ?.find((urn) => urn.includes(":externalmcp:"))
    ?.split(":")[2];
  return (
    <ExternalMcpIcon
      externalMcpSlug={externalMcpSlug}
      alt={toolset.name}
      className={className}
      fallbackClassName={fallbackClassName}
    />
  );
}

/** The catalog logo of the external MCP a hosted server proxies, else a generic icon. */
export function ExternalMcpIcon({
  externalMcpSlug,
  alt,
  className = "h-12 w-12",
  fallbackClassName = "h-8 w-8",
}: {
  externalMcpSlug: string | undefined;
  alt: string;
  className?: string;
  fallbackClassName?: string;
}): JSX.Element {
  const catalogIconMap = useCatalogIconMap();
  const { data: deploymentResult } = useLatestDeployment();

  const externalMcpLogoUrl = useMemo(() => {
    if (!externalMcpSlug) return undefined;

    const matchingMcp = deploymentResult?.deployment?.externalMcps?.find(
      (mcp) => mcp.slug === externalMcpSlug,
    );
    return matchingMcp?.registryServerSpecifier
      ? catalogIconMap.get(matchingMcp.registryServerSpecifier)
      : undefined;
  }, [externalMcpSlug, catalogIconMap, deploymentResult]);

  return externalMcpLogoUrl ? (
    <img
      src={externalMcpLogoUrl}
      alt={alt}
      className={cn("object-contain", className)}
    />
  ) : (
    <Network className={cn("text-muted-foreground", fallbackClassName)} />
  );
}
