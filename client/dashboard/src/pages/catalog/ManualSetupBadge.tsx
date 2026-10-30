import { Badge } from "@/components/ui/Badge";
import {
  Tooltip,
  TooltipContent,
  TooltipPortal,
  TooltipTrigger,
} from "@/components/ui/Tooltip";
import type { CatalogServer } from "./hooks";
import { serverSetupType } from "./hooks/serverMetadata";

const MANUAL_SETUP_TOOLTIP =
  "Connecting requires manual authentication setup. Follow this server's setup guide for the required credentials.";
const UNKNOWN_SETUP_TOOLTIP =
  "Automatic authentication setup has not been confirmed. Check the server's setup guide or discover its sign-in options during setup.";

/** Show setup requirements without treating missing discovery as a rejection. */
export function ManualSetupBadge({
  server,
  className,
}: {
  server: CatalogServer;
  className?: string;
}): JSX.Element | null {
  const setup = serverSetupType(server);
  if (setup === "auto") return null;
  const isManual = setup === "manual";

  return (
    <Tooltip>
      <TooltipTrigger>
        <Badge variant={isManual ? "warning" : "neutral"} className={className}>
          <Badge.Text>{isManual ? "Manual Setup" : "Setup unknown"}</Badge.Text>
        </Badge>
      </TooltipTrigger>
      <TooltipPortal>
        <TooltipContent className="max-w-sm">
          {isManual ? MANUAL_SETUP_TOOLTIP : UNKNOWN_SETUP_TOOLTIP}
        </TooltipContent>
      </TooltipPortal>
    </Tooltip>
  );
}
