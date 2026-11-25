import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { useOrgRoutes } from "@/routes";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { StepContainer } from "../step-container";

export function PrivateNetworkSetupStep({
  onComplete,
}: {
  onComplete: () => void;
}): JSX.Element {
  const organization = useOrganization();
  const routes = useOrgRoutes();
  const features = useProductFeatures(
    { organizationId: organization.id },
    undefined,
    { throwOnError: false },
  );
  const entitled = features.data?.networkIngressEnabled === true;
  const unavailable = features.isError || !features.data;

  return (
    <StepContainer
      title="Set up private network access"
      description="Connect Tailscale to serve hosted MCP endpoints privately."
      onContinue={onComplete}
      canContinue={!features.isPending && !unavailable && entitled}
    >
      <div className="space-y-4">
        <Text>
          {features.isPending
            ? "Checking private network access…"
            : unavailable
              ? "Private network access could not be checked. Open organization settings to retry."
              : entitled
                ? "Private network access is enabled for this organization. Connect your tailnet in organization settings, then confirm the connection is online."
                : "Private network access is not enabled for this organization. Contact support to enable it or discuss an upgrade."}
        </Text>
        <Text>
          Enter Tailscale OAuth credentials only in the organization settings
          dashboard. Never paste credentials into an agent conversation. Return
          here and mark this task done after verifying the connection.
        </Text>
        <div className="flex flex-wrap gap-2">
          <Button asChild>
            <a href={routes.domains.href()}>Open private network settings</a>
          </Button>
          {!features.isPending && !unavailable && !entitled && (
            <Button asChild variant="secondary">
              <a href="mailto:support@speakeasy.com?subject=Enable%20Tailscale%20private%20access">
                Contact support
              </a>
            </Button>
          )}
        </div>
      </div>
    </StepContainer>
  );
}
