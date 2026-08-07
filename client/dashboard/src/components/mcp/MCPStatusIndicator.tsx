import { Text } from "@/components/ui/Text";
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
      color: "bg-destructive",
      pulseColor: "bg-destructive/60",
      label: "Disabled",
    };
  }
  if (!mcpIsPublic) {
    return {
      color: "bg-muted-foreground/60",
      pulseColor: "bg-muted-foreground/40",
      label: "Private",
    };
  }
  return {
    color: "bg-success-default",
    pulseColor: "bg-success-default",
    label: "Public",
  };
}

export function MCPStatusIndicator({
  mcpEnabled,
  mcpIsPublic,
  size = "md",
  className,
}: MCPStatusIndicatorProps): JSX.Element {
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
      <Text variant="small" muted>
        {status.label}
      </Text>
    </div>
  );
}
