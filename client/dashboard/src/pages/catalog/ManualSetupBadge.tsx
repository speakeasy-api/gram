import {
  Badge,
  Tooltip,
  TooltipContent,
  TooltipPortal,
  TooltipTrigger,
} from "@speakeasy-api/moonshine";
import type { PulseMCPServer } from "./hooks";
import { requiresManualSetup } from "./hooks/serverMetadata";

const MANUAL_SETUP_TOOLTIP =
  "This server doesn't support dynamic client registration (DCR). Connecting requires manual auth setup — static OAuth client credentials or API keys.";

/**
 * Flags a catalog server that requires manual auth setup (no DCR support).
 * Renders nothing for automatic / DCR-capable servers, so callers can drop it
 * inline without a guard. Uses the Moonshine badge so it lines up with the
 * neighbouring catalog badges (tool count, Official/Latest).
 */
export function ManualSetupBadge({
  server,
  className,
}: {
  server: PulseMCPServer;
  className?: string;
}): JSX.Element | null {
  if (!requiresManualSetup(server)) return null;

  return (
    <Tooltip>
      <TooltipTrigger>
        <Badge variant="warning" className={className}>
          <Badge.Text>Manual Setup</Badge.Text>
        </Badge>
      </TooltipTrigger>
      <TooltipPortal>
        <TooltipContent className="max-w-sm">
          {MANUAL_SETUP_TOOLTIP}
        </TooltipContent>
      </TooltipPortal>
    </Tooltip>
  );
}
