import { RequireScope } from "@/components/require-scope";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import { useOrgRoutes } from "@/routes";
import type { OrganizationGateway } from "@gram/client/models/components/organizationgateway.js";
import { useState } from "react";
import type { OrganizationGatewaySetting } from "./useOrganizationGatewaySetting";
import { Link } from "react-router";

/**
 * The on/off switch. The page starts an access review when turning on;
 * turning off asks first, because
 * it cuts off every connected client at once. Members see it disabled with the
 * admin-only reason.
 */
export function OrganizationGatewaySwitch({
  on,
  setOn,
  isPending,
}: Pick<OrganizationGatewaySetting, "setOn" | "isPending"> & {
  on: boolean;
}): JSX.Element {
  const [confirming, setConfirming] = useState(false);
  return (
    <>
      <RequireScope
        scope="org:admin"
        level="component"
        reason="Only organization admins can change this setting."
      >
        <Switch
          checked={on}
          onCheckedChange={(checked) =>
            checked ? setOn(true) : setConfirming(true)
          }
          disabled={isPending}
          aria-label="Organization Gateway"
        />
      </RequireScope>
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title="Turn off Organization Gateway?"
        description={
          <>
            This removes your team&apos;s access to their servers through the
            Organization Gateway. Clients connected to it stop working right
            away.
            <br />
            <br />
            Each MCP server keeps working at its own URL. You can turn the
            gateway back on at any time.
          </>
        }
        confirmLabel="Turn off"
        pendingLabel="Turning off…"
        isPending={isPending}
        onConfirm={() =>
          setOn(false, { onSuccess: () => setConfirming(false) })
        }
      />
    </>
  );
}

/** The setup card whose visit prepares the organization-wide sign-in. */
const GATEWAY_SETUP_TASK = "anthropic-admin-controls";

/**
 * Explains why a gateway that is switched on still isn't served, and where to
 * fix it, so the switch never reads as on for a URL that answers 404.
 */
export function GatewayNotServed({
  reason,
}: {
  reason: OrganizationGateway["unavailableReason"];
}): JSX.Element | null {
  const orgRoutes = useOrgRoutes();
  switch (reason) {
    // Speakeasy creates the organization-wide sign-in when an organization
    // admin opens this setup card, so that is where to send them.
    case "no_organization_issuer":
      return (
        <Text small className="text-warning mt-3">
          Not available yet: it needs an organization-wide sign-in. An
          organization admin creates it by opening{" "}
          <Link
            to={`${orgRoutes.setup.href()}?task=${GATEWAY_SETUP_TASK}`}
            className="underline"
          >
            Set up Anthropic admin controls
          </Link>{" "}
          in setup.
        </Text>
      );
    // No dashboard page removes an organization-wide sign-in.
    case "multiple_organization_issuers":
      return (
        <Text small className="text-warning mt-3">
          Not available: the organization has more than one organization-wide
          sign-in, and the gateway needs exactly one. Contact support to resolve
          this.
        </Text>
      );
    case "custom_domain_only":
      return (
        <Text small className="text-warning mt-3">
          Not available: your custom domain only allows access from listed IP
          addresses, and the gateway isn&apos;t served there. Change this in{" "}
          <Link to={orgRoutes.domains.href()} className="underline">
            Network Access
          </Link>
          .
        </Text>
      );
    // The switch itself already reads off, and a disabled organization
    // never reaches its settings.
    case "turned_off":
    case "organization_disabled":
    case undefined:
      return null;
  }
  return (
    <Text small className="text-warning mt-3">
      Gateway status unavailable. Refresh the page for updated details.
    </Text>
  );
}
