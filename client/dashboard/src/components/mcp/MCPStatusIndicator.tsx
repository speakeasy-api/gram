import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import type { MetaMcpServerVisibility } from "@gram/client/models/components/metamcpserver.js";

interface MCPStatusIndicatorProps {
  mcpEnabled: boolean | undefined;
  mcpIsPublic: boolean | undefined;
  /**
   * The server authenticates its clients against its own upstream
   * authorization server. Optional because the toolset-backed callers have no
   * such state; without it an upstream server reads as "Private", which is
   * the one thing it is not.
   */
  mcpIsUpstream?: boolean;
  size?: "sm" | "md";
  className?: string;
}

interface StatusConfig {
  color: string;
  pulseColor: string;
  label: string;
  animate: boolean;
}

function getStatusConfig(
  mcpEnabled: boolean | undefined,
  mcpIsPublic: boolean | undefined,
  mcpIsUpstream: boolean | undefined,
): StatusConfig {
  if (!mcpEnabled) {
    return {
      color: "bg-destructive",
      pulseColor: "bg-destructive/60",
      label: "Disabled",
      animate: false,
    };
  }
  // Checked before the public/private split, which this axis does not sit on:
  // the server is reachable without a Gram credential, but it is not open.
  if (mcpIsUpstream) {
    return {
      color: "bg-information-default",
      pulseColor: "bg-information-default",
      label: "Upstream auth",
      animate: true,
    };
  }
  if (!mcpIsPublic) {
    return {
      color: "bg-muted-foreground/60",
      pulseColor: "bg-muted-foreground/40",
      label: "Private",
      animate: true,
    };
  }
  return {
    color: "bg-success-default",
    pulseColor: "bg-success-default",
    label: "Public",
    animate: true,
  };
}

// A gateway's closest equivalent of server visibility: whether it serves at
// all, and whether callers must sign in first.
function getGatewayStatusConfig(
  visibility: MetaMcpServerVisibility,
  requiresSignIn: boolean,
): StatusConfig {
  if (visibility === "disabled") {
    return {
      color: "bg-destructive",
      pulseColor: "bg-destructive/60",
      label: "Disabled",
      animate: false,
    };
  }
  if (requiresSignIn) {
    return {
      color: "bg-muted-foreground/60",
      pulseColor: "bg-muted-foreground/40",
      label: "Sign-in required",
      animate: true,
    };
  }
  // "Anonymous" matches the gateway overview's Authentication tile wording.
  return {
    color: "bg-success-default",
    pulseColor: "bg-success-default",
    label: "Anonymous",
    animate: true,
  };
}

function StatusDotLabel({
  status,
  size = "md",
  className,
}: {
  status: StatusConfig;
  size?: "sm" | "md";
  className?: string;
}): JSX.Element {
  const dotSize = size === "sm" ? "h-2 w-2" : "h-2.5 w-2.5";

  return (
    <div className={cn("flex items-center gap-2", className)}>
      <div className={cn("relative flex", dotSize)}>
        {status.animate && (
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

export function MCPStatusIndicator({
  mcpEnabled,
  mcpIsPublic,
  mcpIsUpstream,
  size = "md",
  className,
}: MCPStatusIndicatorProps): JSX.Element {
  return (
    <StatusDotLabel
      status={getStatusConfig(mcpEnabled, mcpIsPublic, mcpIsUpstream)}
      size={size}
      className={className}
    />
  );
}

export function GatewayStatusIndicator({
  visibility,
  requiresSignIn,
  size = "md",
  className,
}: {
  visibility: MetaMcpServerVisibility;
  requiresSignIn: boolean;
  size?: "sm" | "md";
  className?: string;
}): JSX.Element {
  return (
    <StatusDotLabel
      status={getGatewayStatusConfig(visibility, requiresSignIn)}
      size={size}
      className={className}
    />
  );
}
