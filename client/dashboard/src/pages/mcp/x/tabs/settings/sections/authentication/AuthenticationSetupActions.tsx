import { RequireScope } from "@/components/require-scope";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Button } from "@speakeasy-api/moonshine";
import type { ProtectedResourceProbeStatus } from "./useProtectedResourceMetadata";

export function AuthenticationSetupActions({
  probeStatus,
  hasDiscoveredAuthorizationServer,
  onUseDiscovered,
  onStartManual,
}: {
  probeStatus: ProtectedResourceProbeStatus;
  // True only when the RFC 9728 probe returned at least one
  // authorization_servers entry; without one there's nothing to seed
  // discovery with even if the probe succeeded.
  hasDiscoveredAuthorizationServer: boolean;
  onUseDiscovered: () => void;
  onStartManual: () => void;
}): JSX.Element {
  const probing = probeStatus === "loading";
  const discoverAvailable =
    probeStatus === "available" && hasDiscoveredAuthorizationServer;

  const discoverButton = (
    <Button
      variant="secondary"
      disabled={!discoverAvailable || probing}
      onClick={onUseDiscovered}
    >
      <Button.Text>Use Discovered</Button.Text>
    </Button>
  );

  return (
    <RequireScope scope="mcp:write" level="component">
      <div className="flex flex-wrap gap-2">
        {discoverAvailable ? (
          discoverButton
        ) : (
          <Tooltip>
            <TooltipTrigger asChild>
              {/* Disabled native buttons don't fire pointer events, so the
                  tooltip never opens on hover without a wrapper. Adding the
                  button role + aria-disabled lets assistive tech announce
                  the wrapper as a disabled button rather than an unlabelled
                  focusable region. */}
              <span
                role="button"
                aria-disabled="true"
                tabIndex={0}
                aria-label={
                  probing
                    ? "Discover unavailable: probing remote URL for OAuth protected resource metadata"
                    : "Discover unavailable: OAuth protected resource metadata not advertised"
                }
              >
                {discoverButton}
              </span>
            </TooltipTrigger>
            <TooltipContent>
              {probing
                ? "Probing the remote URL for OAuth protected resource metadata..."
                : "OAuth protected resource metadata unavailable"}
            </TooltipContent>
          </Tooltip>
        )}
        <Button variant="secondary" onClick={onStartManual}>
          <Button.Text>Configure Manually</Button.Text>
        </Button>
      </div>
    </RequireScope>
  );
}
