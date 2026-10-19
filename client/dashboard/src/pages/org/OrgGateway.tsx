import { SettingsPage } from "@/components/page-templates";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { useIsCurrentOrganization } from "@/hooks/useIsCurrentOrganization";
import { handleAPIError } from "@/lib/errors";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
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
import { Network } from "lucide-react";
import { Link } from "react-router";
import { toast } from "sonner";

export default function OrgGateway(): JSX.Element {
  const organization = useOrganization();
  const isCurrentOrganization = useIsCurrentOrganization(organization.id);
  return (
    <SettingsPage
      scope={["org:read", "org:admin"]}
      title="Gateway"
      description="One MCP URL that gives each member every MCP server in the organization they can use, across all projects."
    >
      <GatewaySetting
        key={organization.id}
        organizationId={organization.id}
        isCurrentOrganization={isCurrentOrganization}
      />
    </SettingsPage>
  );
}

function GatewaySetting({
  organizationId,
  isCurrentOrganization,
}: {
  organizationId: string;
  isCurrentOrganization: () => boolean;
}): JSX.Element | null {
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
      // The stored feature turns the gateway off, so enabled means "off".
      toast.success(
        variables.request.setProductFeatureRequestBody.enabled
          ? "Gateway turned off"
          : "Gateway turned on",
      );
    },
    onError: (error) => {
      if (!isCurrentOrganization()) return;
      handleAPIError(error, "Failed to update the gateway");
    },
  });

  if (features.isLoading) return null;

  if (features.error || !features.data) {
    return (
      <section
        className="border-border bg-card border p-5"
        role="alert"
        aria-live="polite"
      >
        <Text className="text-destructive" small>
          Couldn&apos;t load the gateway setting.
        </Text>
        <Button
          variant="secondary"
          size="sm"
          className="mt-3"
          disabled={features.isFetching}
          onClick={() => void features.refetch()}
        >
          {features.isFetching ? "Retrying…" : "Retry"}
        </Button>
      </section>
    );
  }

  const on = !features.data.organizationGatewayDisabled;

  return (
    <section className="border-border bg-card border p-5">
      <div className="flex items-start justify-between gap-3">
        <div className="flex items-start gap-3">
          <div className="border-border flex size-9 shrink-0 items-center justify-center border">
            <Network className="text-muted-foreground size-4" />
          </div>
          <div>
            <Text variant="subheading">Organization gateway</Text>
            <Text small muted className="mt-1 max-w-3xl">
              Members add one URL to their AI client and sign in with their
              organization account. Turning it off stops it working for everyone
              straight away, including clients already connected.
            </Text>
            {on && gateway.data && !gateway.data.available && (
              <GatewayNotServed reason={gateway.data.unavailableReason} />
            )}
            {on && gateway.error && (
              <Text small className="text-warning mt-3" role="alert">
                Couldn&apos;t check whether the gateway is being served.{" "}
                <button
                  type="button"
                  className="underline"
                  disabled={gateway.isFetching}
                  onClick={() => void gateway.refetch()}
                >
                  {gateway.isFetching ? "Checking…" : "Check again"}
                </button>
              </Text>
            )}
          </div>
        </div>
        <RequireScope
          scope="org:admin"
          level="component"
          reason="Only organization admins can change this setting."
        >
          <Switch
            checked={on}
            onCheckedChange={(checked) =>
              mutation.mutate({
                request: {
                  setProductFeatureRequestBody: {
                    organizationId,
                    featureName: FeatureName.OrganizationGatewayDisabled,
                    enabled: !checked,
                  },
                },
              })
            }
            disabled={mutation.isPending}
            aria-label="Organization gateway"
          />
        </RequireScope>
      </div>
    </section>
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
  // With the sign-ins page rolled out, send people where they can fix it.
  const signInsPage =
    useFeatureFlag(FEATURE_FLAGS.userSessionIssuerDashboard).status ===
    "enabled";
  switch (reason) {
    case "no_organization_issuer":
      if (signInsPage) {
        return (
          <Text small className="text-warning mt-3">
            Not available yet: it needs an organization-wide sign-in.{" "}
            <Link
              to={`${orgRoutes.userSessionIssuers.href()}?create=1`}
              className="underline"
            >
              Create one
            </Link>
            .
          </Text>
        );
      }
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
      if (signInsPage) {
        return (
          <Text small className="text-warning mt-3">
            Not available: the organization has more than one organization-wide
            sign-in. Keep one on the{" "}
            <Link
              to={orgRoutes.userSessionIssuers.href()}
              className="underline"
            >
              Sign-ins
            </Link>{" "}
            page with Consolidate into another.
          </Text>
        );
      }
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
