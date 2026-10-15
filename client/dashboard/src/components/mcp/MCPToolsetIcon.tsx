import { useCatalogIconMap } from "../sources/sources-hooks";
import { useLatestDeployment } from "@gram/client/react-query/latestDeployment.js";
import type { ToolsetEntry } from "@gram/client/models/components/toolsetentry.js";
import { useMemo } from "react";
import { Network } from "lucide-react";

export function MCPToolsetIcon({
  toolset,
}: {
  toolset: ToolsetEntry;
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
      className="h-12 w-12 object-contain"
    />
  ) : (
    <Network className="text-muted-foreground h-8 w-8" />
  );
}
