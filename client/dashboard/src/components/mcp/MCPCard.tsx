import { MoreActions } from "@/components/ui/more-actions";
import { Type } from "@/components/ui/type";
import { UpdatedAt } from "@/components/updated-at";
import { useMcpUrl } from "@/hooks/useToolsetUrl";
import { useRoutes } from "@/routes";
import { ToolsetEntry } from "@gram/client/models/components";
import { Badge } from "@speakeasy-api/moonshine";
import { CheckCircleIcon, LockIcon, XCircleIcon } from "lucide-react";
import { MCPIllustration } from "../sources/SourceCardIllustrations";

export function MCPCard({
  toolset,
  onConfigClick,
}: {
  toolset: ToolsetEntry;
  onConfigClick: () => void;
}) {
  const routes = useRoutes();
  const { url: mcpUrl } = useMcpUrl(toolset);

  const actions = [
    {
      label: "View/Copy Config",
      onClick: onConfigClick,
      icon: "braces" as const,
    },
    {
      label: "Manage Tools",
      onClick: () => routes.mcp.details.goTo(toolset.slug),
      icon: "blocks" as const,
    },
    {
      label: "MCP Settings",
      onClick: () => routes.mcp.details.goTo(toolset.slug),
      icon: "cog" as const,
    },
  ];

  let statusBadge = null;
  if (!toolset.mcpEnabled) {
    statusBadge = (
      <Badge variant="secondary" className="flex items-center gap-1">
        <XCircleIcon className="w-3 h-3" />
        Disabled
      </Badge>
    );
  } else if (toolset.mcpIsPublic) {
    statusBadge = (
      <Badge variant="secondary" className="flex items-center gap-1">
        <CheckCircleIcon className="w-3 h-3 text-green-600" />
        Public
      </Badge>
    );
  } else {
    statusBadge = (
      <Badge variant="secondary" className="flex items-center gap-1">
        <LockIcon className="w-3 h-3" />
        Private
      </Badge>
    );
  }

  return (
    <div
      className="group bg-card text-card-foreground flex flex-col rounded-xl border overflow-hidden hover:border-foreground/20 hover:shadow-md transition-all cursor-pointer"
      onClick={() => routes.mcp.details.goTo(toolset.slug)}
    >
      {/* Illustration header */}
      <div className="h-36 w-full overflow-hidden border-b">
        <MCPIllustration mcpUrl={mcpUrl || ""} toolsetSlug={toolset.slug} />
      </div>

      {/* Content area */}
      <div className="p-4 flex flex-col flex-1">
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
        </div>

        {/* Footer row with status badge and updated time */}
        <div className="flex items-center justify-between gap-2 mt-auto pt-2">
          {statusBadge}
          <UpdatedAt
            date={new Date(toolset.updatedAt)}
            italic={false}
            className="text-xs"
            showRecentness
          />
        </div>
      </div>
    </div>
  );
}
