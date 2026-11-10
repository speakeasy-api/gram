import { useListMCPCatalog } from "@/pages/catalog/hooks";
import { CatalogLogo } from "@/pages/catalog/CatalogLogo";
import { primaryExternalMcpSlug } from "../sources/external-mcp-oauth";
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
  return (
    <ExternalMcpIcon
      externalMcpSlug={primaryExternalMcpSlug(toolset.toolUrns)}
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
  const { data: catalog } = useListMCPCatalog();
  const { data: deploymentResult } = useLatestDeployment();

  const externalMcpLogoUrl = useMemo(() => {
    if (!externalMcpSlug) return undefined;

    const matchingMcp = deploymentResult?.deployment?.externalMcps?.find(
      (mcp) => mcp.slug === externalMcpSlug,
    );
    return matchingMcp?.registryServerSpecifier
      ? catalog?.servers.find(
          (s) => s.registrySpecifier === matchingMcp.registryServerSpecifier,
        )
      : undefined;
  }, [externalMcpSlug, catalog, deploymentResult]);

  return externalMcpLogoUrl ? (
    <CatalogLogo
      server={externalMcpLogoUrl}
      alt={alt}
      className={cn("object-contain", className)}
      fallback={
        <Network className={cn("text-muted-foreground", fallbackClassName)} />
      }
    />
  ) : (
    <Network className={cn("text-muted-foreground", fallbackClassName)} />
  );
}
