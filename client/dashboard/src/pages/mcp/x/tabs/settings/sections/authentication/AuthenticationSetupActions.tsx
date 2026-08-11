import { RequireScope } from "@/components/require-scope";
import { Text } from "@/components/ui/Text";
import { Button } from "@/components/ui/Button";
import type { ReactNode } from "react";
import type { ProtectedResourceProbeStatus } from "./useProtectedResourceMetadata";

export function AuthenticationSetupActions({
  probeStatus,
  hasDiscoveredAuthorizationServer,
  onUseDiscovered,
  onStartManual,
  additionalAction,
}: {
  probeStatus: ProtectedResourceProbeStatus;
  // True only when the RFC 9728 probe returned at least one
  // authorization_servers entry; without one there's nothing to seed
  // discovery with even if the probe succeeded.
  hasDiscoveredAuthorizationServer: boolean;
  onUseDiscovered: () => void;
  onStartManual: () => void;
  additionalAction?: ReactNode;
}): JSX.Element {
  const probing = probeStatus === "loading";
  const discoverAvailable =
    probeStatus === "available" && hasDiscoveredAuthorizationServer;

  return (
    <RequireScope scope="mcp:write" level="component">
      <div className="flex flex-wrap items-center gap-2">
        {discoverAvailable ? (
          <Button variant="secondary" onClick={onUseDiscovered}>
            <Button.Text>Use Discovered</Button.Text>
          </Button>
        ) : (
          <Text muted small>
            {probing
              ? "Checking for advertised OAuth metadata…"
              : "OAuth metadata was not advertised by this server."}
          </Text>
        )}
        <Button variant="secondary" onClick={onStartManual}>
          <Button.Text>Configure Manually</Button.Text>
        </Button>
        {additionalAction}
      </div>
    </RequireScope>
  );
}
