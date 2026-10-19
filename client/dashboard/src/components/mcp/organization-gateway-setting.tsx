import { RequireScope } from "@/components/require-scope";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import { handleAPIError } from "@/lib/errors";
import { useOrgRoutes } from "@/routes";
import type { OrganizationGateway } from "@gram/client/models/components/organizationgateway.js";
import { FeatureName } from "@gram/client/models/components/setproductfeaturerequestbody.js";
import { useFeaturesSetMutation } from "@gram/client/react-query/featuresSet.js";
import {
  invalidateAllOrganizationGateway,
  useOrganizationGateway,
} from "@gram/client/react-query/organizationGateway.js";
import {
  invalidateAllProductFeatures,
  useProductFeatures,
} from "@gram/client/react-query/productFeatures.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router";
import { toast } from "sonner";

/**
 * The Team Gateway's on/off state and the switch that changes it. The stored
 * product feature turns the gateway off, so "on" is its inverse. Shared by the
 * Team Gateway page and the organization's Gateway settings page so both read
 * and write the same thing the same way.
 */
export function useOrganizationGatewaySetting({
  organizationId,
  isCurrentOrganization,
}: {
  organizationId: string;
  isCurrentOrganization: () => boolean;
}) {
  const queryClient = useQueryClient();
  const features = useProductFeatures({ organizationId }, undefined, {
    throwOnError: false,
  });
  const gateway = useOrganizationGateway(undefined, undefined, {
    throwOnError: false,
  });
  const mutation = useFeaturesSetMutation({
    onSuccess: async (_, variables) => {
      if (!isCurrentOrganization()) return;
      await Promise.all([
        invalidateAllProductFeatures(queryClient),
        invalidateAllOrganizationGateway(queryClient),
      ]);
      if (!isCurrentOrganization()) return;
      toast.success(
        variables.request.setProductFeatureRequestBody.enabled
          ? "Team Gateway turned off"
          : "Team Gateway turned on",
      );
    },
    onError: (error) => {
      if (!isCurrentOrganization()) return;
      handleAPIError(error, "Failed to update the Team Gateway");
    },
  });

  const setOn = (on: boolean, options?: { onSuccess?: () => void }) =>
    mutation.mutate(
      {
        request: {
          setProductFeatureRequestBody: {
            organizationId,
            featureName: FeatureName.OrganizationGatewayDisabled,
            enabled: !on,
          },
        },
      },
      options,
    );

  return {
    features,
    gateway,
    on: features.data ? !features.data.organizationGatewayDisabled : undefined,
    setOn,
    isPending: mutation.isPending,
  };
}

/**
 * The on/off switch. Turning on is immediate; turning off asks first, because
 * it cuts off every connected client at once. Members see it disabled with the
 * admin-only reason.
 */
export function OrganizationGatewaySwitch({
  on,
  setOn,
  isPending,
}: Pick<
  ReturnType<typeof useOrganizationGatewaySetting>,
  "setOn" | "isPending"
> & { on: boolean }): JSX.Element {
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
          aria-label="Team Gateway"
        />
      </RequireScope>
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title="Turn off Team Gateway?"
        description={
          <>
            This removes your team&apos;s access to their servers through the
            Team Gateway. Clients connected to it stop working right away.
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
    case "no_organization_issuer":
      return (
        <Text small className="text-warning mt-3">
          Not available yet: it needs an organization-wide sign-in. Set one up
          in{" "}
          <Link to={orgRoutes.identity.href()} className="underline">
            IDP and SSO
          </Link>
          .
        </Text>
      );
    case "multiple_organization_issuers":
      return (
        <Text small className="text-warning mt-3">
          Not available: the organization has more than one organization-wide
          sign-in. Keep one in{" "}
          <Link to={orgRoutes.identity.href()} className="underline">
            IDP and SSO
          </Link>
          .
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
}
