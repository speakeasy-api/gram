import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";

interface MCPStatusIndicatorProps {
  mcpEnabled: boolean | undefined;
  mcpIsPublic: boolean | undefined;
  size?: "sm" | "md";
  className?: string;
}

function getStatusConfig(
  mcpEnabled: boolean | undefined,
  mcpIsPublic: boolean | undefined,
) {
  if (!mcpEnabled) {
    return {
      color: "bg-red-500",
      pulseColor: "bg-red-400",
      label: "Disabled",
    };
  }
  return {
    color: "bg-green-500",
    pulseColor: "bg-green-400",
    label: mcpIsPublic ? "Public" : "Private",
  };
}

export function MCPStatusIndicator({
  mcpEnabled,
  mcpIsPublic,
  size = "md",
  className,
}: MCPStatusIndicatorProps) {
  const status = getStatusConfig(mcpEnabled, mcpIsPublic);
  const dotSize = size === "sm" ? "h-2 w-2" : "h-2.5 w-2.5";

  return (
    <div className={cn("flex items-center gap-2", className)}>
      <div className={cn("relative flex", dotSize)}>
        {mcpEnabled && (
          <span
            className={cn(
              "absolute inline-flex h-full w-full animate-ping rounded-full opacity-75",
              status.pulseColor,
            )}
          />
        )}
        <span
          className={cn(
            "relative inline-flex rounded-full",
            dotSize,
            status.color,
          )}
        />
      </div>
      <Type variant="small" muted>
        {status.label}
      </Type>
    </div>
  );
}
