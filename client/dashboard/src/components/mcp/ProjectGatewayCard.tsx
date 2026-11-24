import speakeasyIcon from "@/assets/speakeasy-icon.svg";
import { SPEAKEASY_RAINBOW } from "@/components/icon-dither";
import { Badge } from "@/components/ui/Badge";
import { cn } from "@/lib/utils";
import { McpKindBadge } from "./McpKindBadge";
import { Text } from "@/components/ui/Text";
import { useRoutes } from "@/routes";
import { ArrowRight } from "lucide-react";
import { McpEntityCard } from "./McpEntityCard";
import type { ProjectGatewayEntryStatus } from "./useProjectGatewayEntry";

const PROJECT_GATEWAY_SUBTITLE = "Every server in this project, at one URL";

export function SpeakeasyMark({
  className,
}: {
  className: string;
}): JSX.Element {
  return <img src={speakeasyIcon} alt="" aria-hidden className={className} />;
}

export function ProjectGatewayCard({
  status,
}: {
  status: ProjectGatewayEntryStatus;
}): JSX.Element {
  const routes = useRoutes();
  const isDisabled = status === "off" || status === "unavailable";
  return (
    <McpEntityCard
      className={cn(
        "cursor-pointer",
        status === "available" && "speakeasy-rainbow-frame",
        isDisabled && "bg-muted/40",
      )}
      ditherPalette={status === "available" ? SPEAKEASY_RAINBOW : undefined}
      onClick={() => routes.mcp.projectGateway.goTo()}
      icon={
        <SpeakeasyMark
          className={cn("size-10", isDisabled && "opacity-50 grayscale")}
        />
      }
    >
      <div className="mb-2 flex items-start justify-between gap-2">
        <Text
          variant="subheading"
          as="div"
          className={cn(
            "text-md group-hover:text-primary flex-1 truncate transition-colors",
            isDisabled && "text-muted-foreground",
          )}
        >
          Project Gateway
        </Text>
      </div>

      <Text small muted className="mt-1 truncate">
        {PROJECT_GATEWAY_SUBTITLE}
      </Text>

      <div className="mt-auto flex items-center justify-between gap-2 pt-2">
        <div className="flex items-center gap-2">
          <McpKindBadge kind="gateway" />
          {status === "off" && (
            <Badge variant="warning">
              <Badge.Text>Off</Badge.Text>
            </Badge>
          )}
          {status === "unavailable" && (
            <Badge variant="warning">
              <Badge.Text>Unavailable</Badge.Text>
            </Badge>
          )}
        </div>
        <div className="text-muted-foreground group-hover:text-primary flex items-center gap-1 text-sm transition-colors">
          <span>Open</span>
          <ArrowRight className="h-3.5 w-3.5" />
        </div>
      </div>
    </McpEntityCard>
  );
}
