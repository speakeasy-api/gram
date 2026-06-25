import { Badge } from "@/components/ui/badge";
import type { PulseMCPServer } from "./hooks";
import { requiresManualSetup } from "./hooks/serverMetadata";

const MANUAL_SETUP_TOOLTIP =
  "This server doesn't support automatic client registration (DCR). Connecting requires manual auth setup — static OAuth client credentials or API keys.";

/**
 * Flags a catalog server that requires manual auth setup (no DCR support).
 * Renders nothing for automatic / DCR-capable servers, so callers can drop it
 * inline without a guard.
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
    <Badge
      variant="warning"
      size="sm"
      tooltip={MANUAL_SETUP_TOOLTIP}
      className={className}
    >
      Manual Setup
    </Badge>
  );
}
