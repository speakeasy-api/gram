import { Badge } from "@/components/ui/Badge";
import {
  Tooltip,
  TooltipContent,
  TooltipPortal,
  TooltipTrigger,
} from "@/components/ui/Tooltip";
import { catalogServerHasGuidedSetup } from "@/pages/mcp/x/setup/provider-setup";
import type { CatalogServer } from "./hooks";
import { serverSetupType } from "./hooks/serverMetadata";

const MANUAL_SETUP_TOOLTIP =
  "Connecting requires manual authentication setup. Follow this server's setup guide for the required credentials.";
const UNKNOWN_SETUP_TOOLTIP =
  "Automatic authentication setup has not been confirmed. Check the server's setup guide or discover its sign-in options during setup.";
const GUIDED_SETUP_TOOLTIP =
  "After you add this server, Speakeasy walks you through connecting it step by step.";

/** Show setup requirements without treating missing discovery as a rejection. */
export function ManualSetupBadge({
  server,
  className,
}: {
  server: CatalogServer;
  className?: string;
}): JSX.Element | null {
  const setup = serverSetupType(server);
  const guided = catalogServerHasGuidedSetup(server);
  if (setup === "auto" && !guided) return null;
  const isManual = setup === "manual";

  let label = "Setup unknown";
  let tooltip = UNKNOWN_SETUP_TOOLTIP;
  if (guided) {
    label = "Guided setup";
    tooltip = GUIDED_SETUP_TOOLTIP;
  } else if (isManual) {
    label = "Manual Setup";
    tooltip = MANUAL_SETUP_TOOLTIP;
  }

  return (
    <Tooltip>
      <TooltipTrigger>
        <Badge
          variant={guided ? "information" : isManual ? "warning" : "neutral"}
          className={className}
        >
          <Badge.Text>{label}</Badge.Text>
        </Badge>
      </TooltipTrigger>
      <TooltipPortal>
        <TooltipContent className="max-w-sm">{tooltip}</TooltipContent>
      </TooltipPortal>
    </Tooltip>
  );
}
