import { useToolsetAuthStatus } from "@/hooks/useToolsetAuthStatus";
import { cn } from "@/lib/utils";
import type { ToolsetEntry } from "@gram/client/models/components";
import {
  Badge,
  Tooltip,
  TooltipContent,
  TooltipPortal,
  TooltipTrigger,
} from "@speakeasy-api/moonshine";
import { AlertCircle, CheckCircle } from "lucide-react";

export function OAuthStatusBadge({ toolset }: { toolset: ToolsetEntry }) {
  const {
    requiresOAuth,
    oauthConnected,
    missingEnvVarCount,
    hasAuthRequirements,
    isComplete,
    isLoading,
  } = useToolsetAuthStatus(toolset);

  if (!hasAuthRequirements || isLoading) {
    return null;
  }

  const issues: string[] = [];
  if (missingEnvVarCount > 0) {
    issues.push(
      `${missingEnvVarCount} env var${missingEnvVarCount === 1 ? "" : "s"} missing`,
    );
  }
  if (requiresOAuth && !oauthConnected) {
    issues.push("OAuth not connected");
  }

  const tooltipContent = isComplete
    ? "All authentication configured"
    : issues.join(", ");

  return (
    <Tooltip>
      <TooltipTrigger>
        <Badge
          variant={isComplete ? "success" : "warning"}
          className={cn(isComplete && "bg-card")}
        >
          <Badge.LeftIcon>
            {isComplete ? (
              <CheckCircle className="size-3" />
            ) : (
              <AlertCircle className="size-3" />
            )}
          </Badge.LeftIcon>
          <Badge.Text>Auth</Badge.Text>
        </Badge>
      </TooltipTrigger>
      <TooltipPortal>
        <TooltipContent side="bottom">{tooltipContent}</TooltipContent>
      </TooltipPortal>
    </Tooltip>
  );
}
