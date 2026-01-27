import { Type } from "@/components/ui/type";
import { UpdatedAt } from "@/components/updated-at";
import { useMcpUrl } from "@/hooks/useToolsetUrl";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { ToolsetEntry } from "@gram/client/models/components";
import { MCPRobotIllustration } from "../sources/SourceCardIllustrations";

export function MCPCard({ toolset }: { toolset: ToolsetEntry }) {
  const routes = useRoutes();
  const { url: _mcpUrl } = useMcpUrl(toolset);

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
    <div
      className="group bg-card text-card-foreground flex flex-col rounded-xl border overflow-hidden hover:border-foreground/20 hover:shadow-md transition-all cursor-pointer"
      onClick={() => routes.mcp.details.goTo(toolset.slug)}
    >
      {/* Illustration header */}
      <div className="h-36 w-full overflow-hidden border-b">
        <MCPRobotIllustration
          toolsetSlug={toolset.slug}
          className="saturate-[.3] group-hover:saturate-100 transition-all duration-300"
        />
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

        {/* Footer row with status indicator and updated time */}
        <div className="flex items-center justify-between gap-2 mt-auto pt-2">
          {statusIndicator}
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
