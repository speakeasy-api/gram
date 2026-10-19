import speakeasyIcon from "@/assets/speakeasy-icon.svg";
import { SPEAKEASY_RAINBOW } from "@/components/icon-dither";
import { Badge } from "@/components/ui/Badge";
import { Text } from "@/components/ui/Text";
import { useRoutes } from "@/routes";
import { ArrowRight } from "lucide-react";
import { McpEntityCard } from "./McpEntityCard";

const TEAM_GATEWAY_SUBTITLE = "Every server your team can reach, at one URL";

export function SpeakeasyMark({
  className,
}: {
  className: string;
}): JSX.Element {
  return <img src={speakeasyIcon} alt="" aria-hidden className={className} />;
}

export function TeamGatewayCard({ isOff }: { isOff: boolean }): JSX.Element {
  const routes = useRoutes();
  return (
    <McpEntityCard
      className="speakeasy-rainbow-frame cursor-pointer"
      ditherPalette={SPEAKEASY_RAINBOW}
      onClick={() => routes.mcp.teamGateway.goTo()}
      icon={<SpeakeasyMark className="size-10" />}
    >
      <div className="mb-2 flex items-start justify-between gap-2">
        <Text
          variant="subheading"
          as="div"
          className="text-md group-hover:text-primary flex-1 truncate transition-colors"
        >
          Team Gateway
        </Text>
      </div>

      <Text small muted className="mt-1 truncate">
        {TEAM_GATEWAY_SUBTITLE}
      </Text>

      <div className="mt-auto flex items-center justify-between gap-2 pt-2">
        <div className="flex items-center gap-2">
          <Badge variant="neutral">
            <Badge.Text>Gateway</Badge.Text>
          </Badge>
          {isOff && (
            <Badge variant="warning">
              <Badge.Text>Off</Badge.Text>
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
