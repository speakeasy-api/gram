import { RequireScope } from "@/components/require-scope";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { useRBAC } from "@/hooks/useRBAC";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { useOrgRoutes } from "@/routes";
import type { OrganizationGateway } from "@gram/client/models/components/organizationgateway.js";
import { useState } from "react";
import type { OrganizationGatewaySetting } from "./useOrganizationGatewaySetting";
import { Link } from "react-router";

/**
 * The on/off switch. Turning on asks first and recommends reviewing MCP
 * assignments; turning off asks first, because it cuts off every connected
 * client at once. Members see it disabled with the
 * admin-only reason.
 */
export function OrganizationGatewaySwitch({
  on,
  setOn,
  isPending,
}: Pick<OrganizationGatewaySetting, "setOn" | "isPending"> & {
  on: boolean;
}): JSX.Element {
  const [confirming, setConfirming] = useState<"on" | "off" | null>(null);
  const onOpenChange = (open: boolean) => {
    if (!open) setConfirming(null);
  };
  return (
    <>
      <RequireScope
        scope="org:admin"
        level="component"
        reason="Only organization admins can change this setting."
      >
        <Switch
          checked={on}
          onCheckedChange={(checked) => setConfirming(checked ? "on" : "off")}
          disabled={isPending}
          aria-label="Organization Gateway"
        />
      </RequireScope>
      <ConfirmDialog
        open={confirming === "on"}
        onOpenChange={onOpenChange}
        title="Turn on Organization Gateway?"
        description={
          <>
            Each person gets every MCP server they can access across all
            projects, including access inherited from broad roles.
            <br />
            <br />
            We recommend reviewing MCP assignments first and removing any that
            people no longer need.
          </>
        }
        confirmLabel="Turn on"
        pendingLabel="Turning on…"
        confirmVariant="primary"
        isPending={isPending}
        onConfirm={() => setOn(true, { onSuccess: () => setConfirming(null) })}
      />
      <ConfirmDialog
        open={confirming === "off"}
        onOpenChange={onOpenChange}
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
          setOn(false, { onSuccess: () => setConfirming(null) })
        }
      />
    </>
  );
}

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
  const organization = useOrganization();
  const { hasScope } = useRBAC();
  const canManage = hasScope("org:admin", organization.id);
  // Speakeasy Authorization Servers can make a project's server
  // organization-wide and consolidate one server into another (removing the
  // source). It is rolled out behind a flag and its actions need org:admin;
  // without both, point to an admin or support instead.
  const issuersPage =
    useFeatureFlag(FEATURE_FLAGS.userSessionIssuerDashboard).status ===
      "enabled" && canManage ? (
      <Link to={orgRoutes.userSessionIssuers.href()} className="underline">
        Speakeasy Authorization Servers
      </Link>
    ) : null;
  const askFor = canManage
    ? "Contact support"
    : "Ask an organization admin or contact support";
  switch (reason) {
    // Only organizations inserted by signup get one automatically; identity-
    // synced and existing organizations do not.
    case "no_organization_issuer":
      return (
        <Text small className="text-warning mt-3">
          Not available: it needs an organization-wide sign-in, and your
          organization doesn&apos;t have one.{" "}
          {issuersPage ? (
            <>
              Make a project&apos;s authorization server organization-wide in{" "}
              {issuersPage}, or contact support.
            </>
          ) : (
            `${askFor} to set one up.`
          )}
        </Text>
      );
    case "multiple_organization_issuers":
      return (
        <Text small className="text-warning mt-3">
          Not available: the organization has more than one organization-wide
          sign-in, and the gateway needs exactly one.{" "}
          {issuersPage ? (
            <>
              In {issuersPage}, use Consolidate into another until one is left.
            </>
          ) : (
            `${askFor} to resolve this.`
          )}
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
