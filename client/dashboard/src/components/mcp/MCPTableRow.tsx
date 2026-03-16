import { CopyButton } from "@/components/ui/copy-button";
import { DotRow } from "@/components/ui/dot-row";
import { Type } from "@/components/ui/type";
import { useMcpUrl } from "@/hooks/useToolsetUrl";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { ToolsetEntry } from "@gram/client/models/components";
import { useLatestDeployment } from "@gram/client/react-query";
import { Link2, Network } from "lucide-react";
import { useMemo } from "react";
import { useCatalogIconMap } from "../sources/Sources";
import { ToolCollectionBadge } from "../tool-collection-badge";

export function MCPTableRow({ toolset }: { toolset: ToolsetEntry }) {
  const routes = useRoutes();
  const { url: mcpUrl } = useMcpUrl(toolset);
  const catalogIconMap = useCatalogIconMap();
  const { data: deploymentResult } = useLatestDeployment();

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

  return (
    <DotRow
      onClick={() => routes.mcp.details.goTo(toolset.slug)}
      icon={
        externalMcpInfo?.logoUrl ? (
          <img
            src={externalMcpInfo.logoUrl}
            alt={toolset.name}
            className="w-6 h-6 object-contain"
          />
        ) : (
          <Network className="w-5 h-5 text-muted-foreground" />
        )
      }
    >
      {/* Name */}
      <td className="px-3 py-3">
        <Type
          variant="subheading"
          as="div"
          className="truncate text-sm group-hover:text-primary transition-colors"
          title={toolset.name}
        >
          {toolset.name}
        </Type>
      </td>

      {/* Status */}
      <td className="px-3 py-3">
        <div className="flex items-center gap-2">
          <div className="relative flex h-2 w-2">
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
                "relative inline-flex rounded-full h-2 w-2",
                status.color,
              )}
            />
          </div>
          <Type variant="small" muted>
            {status.label}
          </Type>
        </div>
      </td>

      {/* URL */}
      <td className="px-3 py-3 max-w-xs">
        {mcpUrl ? (
          <div className="flex items-center gap-1.5">
            <Type small muted className="truncate">
              {mcpUrl}
            </Type>
            <CopyButton
              text={mcpUrl}
              size="icon-sm"
              icon={Link2}
              tooltip="Copy MCP URL"
            />
          </div>
        ) : (
          <Type small muted>
            —
          </Type>
        )}
      </td>

      {/* Tools */}
      <td className="px-3 py-3">
        <ToolCollectionBadge toolNames={toolset.tools.map((t) => t.name)} />
      </td>
    </DotRow>
  );
}
