import { NotSetUpState } from "@/components/not-set-up-state";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { useOrganization } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { useIsCurrentOrganization } from "@/hooks/useIsCurrentOrganization";
import { DEMO_ORG_SLUG, demoProjectPageHref } from "@/lib/demo";
import { useOrgRoutes } from "@/routes";
import { FeatureName } from "@gram/client/models/components/setproductfeaturerequestbody.js";
import { useFeaturesSetMutation } from "@gram/client/react-query/featuresSet.js";
import { useState } from "react";
import { useLocation } from "react-router";

interface EnableLoggingOverlayProps {
  onEnabled: () => void;
  screenshotSrc?: string;
  screenshotAlt?: string;
  className?: string;
}

/**
 * Shared overlay component shown when logs are not enabled for the organization.
 * Displays a centered card with enable button and handles the mutation state.
 */
export function EnableLoggingOverlay({
  onEnabled,
  screenshotSrc,
  screenshotAlt = "Feature preview",
  className,
}: EnableLoggingOverlayProps): JSX.Element {
  const organization = useOrganization();
  const { orgSlug, projectSlug } = useSlugs();
  const location = useLocation();
  const orgRoutes = useOrgRoutes();
  const isCurrentOrganization = useIsCurrentOrganization(organization.id);
  const demoHref =
    orgSlug === DEMO_ORG_SLUG
      ? undefined
      : demoProjectPageHref(location.pathname, projectSlug);

  return (
    <EnableLoggingOverlayInner
      key={organization.id}
      organizationId={organization.id}
      isCurrentOrganization={isCurrentOrganization}
      setupHref={orgRoutes.logs.href()}
      demoHref={demoHref}
      screenshotSrc={screenshotSrc}
      screenshotAlt={screenshotAlt}
      className={className}
      onEnabled={onEnabled}
    />
  );
}

function EnableLoggingOverlayInner({
  organizationId,
  isCurrentOrganization,
  setupHref,
  demoHref,
  screenshotSrc,
  screenshotAlt,
  className,
  onEnabled,
}: EnableLoggingOverlayProps & {
  organizationId: string;
  isCurrentOrganization: () => boolean;
  setupHref: string;
  demoHref?: string;
}): JSX.Element {
  const [mutationError, setMutationError] = useState<string | null>(null);
  const { mutate: setLogsFeature, status: mutationStatus } =
    useFeaturesSetMutation({
      onSuccess: () => {
        if (!isCurrentOrganization()) return;
        setMutationError(null);
        onEnabled();
      },
      onError: (err) => {
        if (!isCurrentOrganization()) return;
        const message =
          err instanceof Error ? err.message : "Failed to enable logging";
        setMutationError(message);
      },
    });

  const isMutating = mutationStatus === "pending";

  const handleEnable = () => {
    setMutationError(null);
    setLogsFeature({
      request: {
        setProductFeatureRequestBody: {
          organizationId,
          featureName: FeatureName.Logs,
          enabled: true,
        },
      },
    });
  };

  const enableAction = (
    <div className="flex flex-col items-center gap-2">
      <RequireScope scope="org:admin" level="component">
        <Button onClick={handleEnable} disabled={isMutating}>
          <Button.LeftIcon>
            <Icon name="activity" className="size-4" />
          </Button.LeftIcon>
          <Button.Text>
            {isMutating ? "Enabling..." : "Enable Observability"}
          </Button.Text>
        </Button>
      </RequireScope>
      {mutationError && (
        <span className="text-destructive text-sm">{mutationError}</span>
      )}
    </div>
  );

  return (
    <NotSetUpState
      heading="Observability is not set up yet"
      description="Enable observability to start collecting tool calls, agent sessions, and system metrics for this dashboard. Once observability is enabled, an empty view means no activity has been recorded yet."
      action={enableAction}
      screenshot={
        screenshotSrc ? (
          <img
            src={screenshotSrc}
            alt={screenshotAlt}
            className="block h-auto w-full"
          />
        ) : undefined
      }
      setupHref={setupHref}
      setupLabel="Configure Observability"
      demoHref={demoHref}
      className={className}
    />
  );
}
