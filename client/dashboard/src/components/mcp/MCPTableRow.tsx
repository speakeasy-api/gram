import { CopyButton } from "@/components/ui/copy-button";
import { DotRow } from "@/components/ui/dot-row";
import { Button } from "@/components/ui/button";
import { Type } from "@/components/ui/type";
import { useMcpUrl } from "@/hooks/useToolsetUrl";
import { useRoutes } from "@/routes";
import { MCPStatusIndicator } from "./MCPStatusIndicator";
import { ToolsetEntry } from "@gram/client/models/components";
import { useLatestDeployment } from "@gram/client/react-query";
import { AlertTriangleIcon, Link2, Network, Package } from "lucide-react";
import { useMemo } from "react";
import { useNavigate } from "react-router";
import {
  useCatalogIconMap,
  useExternalMcpOAuthConfigStatus,
} from "../sources/sources-hooks";
import { ToolCollectionBadge } from "../tool-collection-badge";
import { Badge } from "../ui/badge";

export function MCPTableRow({ toolset }: { toolset: ToolsetEntry }) {
  const routes = useRoutes();
  const navigate = useNavigate();
  const { url: mcpUrl } = useMcpUrl(toolset);
  const catalogIconMap = useCatalogIconMap();
  const { data: deploymentResult } = useLatestDeployment();
  const oauthStatus = useExternalMcpOAuthConfigStatus(toolset.slug);

  const handleClick = () => {
    if (oauthStatus === "required-unconfigured") {
      navigate(`${routes.mcp.details.href(toolset.slug)}#authentication`);
    } else {
      routes.mcp.details.goTo(toolset.slug);
    }
  };

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

  const installSourceTooltip = toolset.origin?.registrySpecifier
    ? `Installed from ${toolset.origin.registrySpecifier}`
    : undefined;

  return (
    <DotRow
      onClick={handleClick}
      icon={
        externalMcpInfo?.logoUrl ? (
          <img
            src={externalMcpInfo.logoUrl}
            alt={toolset.name}
            className="h-6 w-6 object-contain"
          />
        ) : (
          <Network className="text-muted-foreground h-5 w-5" />
        )
      }
    >
      {/* Name */}
      <td className="px-3 py-3">
        <div className="flex items-center gap-2">
          <Type
            variant="subheading"
            as="div"
            className="group-hover:text-primary truncate text-sm transition-colors"
            title={toolset.name}
          >
            {toolset.name}
          </Type>
          {oauthStatus === "required-unconfigured" && (
            <Badge
              variant="outline"
              className="border-warning-foreground bg-warning text-warning-foreground text-xs backdrop-blur-sm"
            >
              <AlertTriangleIcon />
              OAuth Required
            </Badge>
          )}
        </div>
      </td>

      {/* Status */}
      <td className="px-3 py-3">
        <MCPStatusIndicator
          mcpEnabled={toolset.mcpEnabled}
          mcpIsPublic={toolset.mcpIsPublic}
          size="sm"
        />
      </td>

      {/* URL */}
      <td className="max-w-xs px-3 py-3">
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

export function MCPTableRowSkeleton() {
  return (
    <DotRow>
      <td className="px-3 py-3">
        <div className="bg-muted h-4 w-2/3 animate-pulse rounded" />
      </td>
      <td className="px-3 py-3">
        <div className="flex items-center gap-2">
          <div className="bg-muted h-2 w-2 animate-pulse rounded-full" />
          <div className="bg-muted h-3.5 w-12 animate-pulse rounded" />
        </div>
      </td>
      <td className="px-3 py-3">
        <div className="bg-muted h-3.5 w-40 animate-pulse rounded" />
      </td>
      <td className="px-3 py-3">
        <div className="bg-muted h-5 w-10 animate-pulse rounded-full" />
      </td>
    </DotRow>
  );
}
