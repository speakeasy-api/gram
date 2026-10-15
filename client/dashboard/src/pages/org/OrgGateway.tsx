import { SettingsPage } from "@/components/page-templates";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { CopyButton } from "@/components/ui/CopyButton";
import { Text } from "@/components/ui/Text";
import {
  PageTabsList,
  PageTabsTrigger,
  Tabs,
  TabsContent,
} from "@/components/ui/Tabs";
import { useOrganization } from "@/contexts/Auth";
import { useIsCurrentOrganization } from "@/hooks/useIsCurrentOrganization";
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
import { Link, useSearchParams } from "react-router";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { MCPAssignments } from "../access/MCPAssignments";
import { GatewayOnboarding } from "./GatewayOnboarding";

export default function OrgGateway(): JSX.Element {
  const organization = useOrganization();
  return (
    <SettingsPage
      scope={["org:read", "org:admin"]}
      title="Org Gateway"
      description="One connection for every MCP server each person is allowed to use, across all projects."
    >
      <GatewayContent key={organization.id} />
    </SettingsPage>
  );
}

function GatewayContent(): JSX.Element {
  const organization = useOrganization();
  const isCurrentOrganization = useIsCurrentOrganization(organization.id);
  const queryClient = useQueryClient();
  const [params, setParams] = useSearchParams();
  const tab = params.get("tab") === "assignments" ? "assignments" : "overview";
  const changeTab = (value: string) =>
    setParams((previous) => {
      const next = new URLSearchParams(previous);
      next.set("tab", value);
      return next;
    });
  const [reviewing, setReviewing] = useState(false);
  const [reviewReady, setReviewReady] = useState(false);
  const [acknowledged, setAcknowledged] = useState(false);
  useEffect(() => {
    if (!reviewReady) setAcknowledged(false);
  }, [reviewReady]);
  const features = useProductFeatures(
    { organizationId: organization.id },
    undefined,
    { throwOnError: false },
  );
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
      setReviewing(false);
      setAcknowledged(false);
      changeTab("overview");
      toast.success(
        variables.request.setProductFeatureRequestBody.enabled
          ? "Org Gateway disabled"
          : "Org Gateway enabled",
      );
    },
    onError: (error) => {
      if (isCurrentOrganization())
        handleAPIError(error, "Couldn’t update Org Gateway");
    },
  });
  const on = features.data?.organizationGatewayDisabled === false;
  const setEnabled = (enabled: boolean) =>
    mutation.mutate({
      request: {
        setProductFeatureRequestBody: {
          organizationId: organization.id,
          featureName: FeatureName.OrganizationGatewayDisabled,
          enabled: !enabled,
        },
      },
    });
  const url = gateway.data?.available ? gateway.data.url : undefined;
  if (features.isLoading) return <Text muted>Loading Org Gateway…</Text>;
  if (features.error || !features.data)
    return (
      <div role="alert" className="space-y-3">
        <Text>Couldn’t load the gateway setting.</Text>
        <Button variant="secondary" onClick={() => void features.refetch()}>
          Retry
        </Button>
      </div>
    );

  return (
    <Tabs value={tab} onValueChange={changeTab}>
      <div className="border-border -mx-8 border-b px-8">
        <PageTabsList>
          <PageTabsTrigger value="overview">Overview</PageTabsTrigger>
          <PageTabsTrigger value="assignments">MCP Assignments</PageTabsTrigger>
        </PageTabsList>
      </div>
      <TabsContent value="overview" className="mt-6 space-y-8">
        <section className="border-border space-y-4 border-b pb-6">
          <div className="flex flex-wrap items-center justify-between gap-4">
            <Text variant="subheading">
              {on ? "Org Gateway is enabled" : "Org Gateway is disabled"}
            </Text>
            <RequireScope
              scope="org:admin"
              level="component"
              reason="Only organization admins can change this setting."
            >
              <Button
                variant={on ? "secondary" : "primary"}
                disabled={mutation.isPending}
                onClick={() => {
                  if (on) setEnabled(false);
                  else {
                    setReviewing(true);
                    setAcknowledged(false);
                    changeTab("assignments");
                  }
                }}
              >
                {mutation.isPending
                  ? "Saving…"
                  : on
                    ? "Disable Org Gateway"
                    : "Review access and enable"}
              </Button>
            </RequireScope>
          </div>
          <Text muted>
            Members connect their AI client once and sign in with their
            organization account. The gateway follows their existing access as
            assignments change. It does not grant new permissions, but makes all
            assigned servers available through one connection—including access
            inherited from broad roles.
          </Text>
          <Text small muted>
            Access is specific to the signed-in person. Removing an assignment
            changes what they can use. Disabling the gateway stops gateway
            requests for everyone, including connected clients; individual
            server connections keep their own settings.
          </Text>
          {gateway.error ? (
            <div role="alert">
              <Text small>Couldn’t check gateway availability.</Text>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => void gateway.refetch()}
              >
                Check again
              </Button>
            </div>
          ) : (
            gateway.data && (
              <GatewayNotServed reason={gateway.data.unavailableReason} />
            )
          )}
          {url && (
            <div className="border-border flex min-w-0 items-center gap-3 border p-3">
              <Text className="min-w-0 flex-1 truncate font-mono text-sm">
                {url}
              </Text>
              <CopyButton text={url} tooltip="Copy gateway URL" />
            </div>
          )}
        </section>
        <GatewayOnboarding url={url ?? undefined} />
      </TabsContent>
      <TabsContent value="assignments" className="mt-6 space-y-6">
        {reviewing && !on && (
          <div className="border-border space-y-2 border-b pb-5">
            <Text variant="subheading">Review access before enabling</Text>
            <Text small muted>
              Remove access people no longer need, then sign off below to enable
              the gateway.
            </Text>
          </div>
        )}
        <MCPAssignments onReady={setReviewReady} />
        {reviewing && !on && (
          <RequireScope scope="org:admin" level="section">
            <div className="border-border space-y-4 border-t pt-5">
              <label className="flex items-start gap-3 text-sm">
                <Checkbox
                  checked={acknowledged && reviewReady}
                  disabled={!reviewReady || mutation.isPending}
                  onCheckedChange={(value) => setAcknowledged(value === true)}
                />
                I have reviewed MCP assignments across the organization and
                approve making each person’s assigned servers available through
                Org Gateway.
              </label>
              <div className="flex gap-3">
                <Button
                  disabled={!reviewReady || !acknowledged || mutation.isPending}
                  onClick={() => setEnabled(true)}
                >
                  {mutation.isPending ? "Enabling…" : "Enable Org Gateway"}
                </Button>
                <Button
                  variant="secondary"
                  disabled={mutation.isPending}
                  onClick={() => {
                    setReviewing(false);
                    setAcknowledged(false);
                    changeTab("overview");
                  }}
                >
                  Cancel
                </Button>
              </div>
            </div>
          </RequireScope>
        )}
      </TabsContent>
    </Tabs>
  );
}
/**
 * Explains why an enabled gateway is unavailable and links to its setup.
 */
function GatewayNotServed({
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
    // The disabled state is already shown above; a disabled organization
    // never reaches this page.
    case "turned_off":
    case "organization_disabled":
    case undefined:
      return null;
  }
}
