import { FeatureToggleRow } from "@/components/feature-toggle-row";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { handleAPIError } from "@/lib/errors";
import { FeatureName } from "@gram/client/models/components/setproductfeaturerequestbody.js";
import { useFeaturesSetMutation } from "@gram/client/react-query/featuresSet";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { Stack } from "@speakeasy-api/moonshine";
import { LogIn, Unplug } from "lucide-react";
import { useState } from "react";

// Hook plugin behavior. The data-capture settings that used to live here
// (enable logs, tool I/O, session capture, skill content, OTel forwarding)
// moved to Data → Configuration; what remains controls HOW hooks behave on
// user machines, not what data is captured.
export default function OrgLogs(): JSX.Element {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope={["org:read", "org:admin"]} level="page">
          <OrgLogsInner />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}

function OrgLogsInner() {
  const { data: featuresData } = useProductFeatures();
  const [hooksBrowserLoginEnabled, setHooksBrowserLoginEnabled] = useState<
    boolean | null
  >(null);
  const [hooksFailOpenEnabled, setHooksFailOpenEnabled] = useState<
    boolean | null
  >(null);

  const effectiveHooksBrowserLoginEnabled =
    hooksBrowserLoginEnabled ?? featuresData?.hooksBrowserLoginEnabled ?? false;
  const effectiveHooksFailOpenEnabled =
    hooksFailOpenEnabled ?? featuresData?.hooksFailOpenEnabled ?? false;

  const { mutate: setFeature, status: mutationStatus } = useFeaturesSetMutation(
    {
      onSuccess: (_, variables) => {
        const { featureName, enabled } =
          variables.request.setProductFeatureRequestBody;
        if (featureName === FeatureName.HooksBrowserLogin) {
          setHooksBrowserLoginEnabled(enabled);
        } else if (featureName === FeatureName.HooksFailOpen) {
          setHooksFailOpenEnabled(enabled);
        }
      },
      onError: (error) => {
        // On error the optimistic state above never runs, so the switch
        // reverts to the server value.
        handleAPIError(error, "Failed to update setting");
      },
    },
  );

  const isMutating = mutationStatus === "pending";

  const setNamedFeature = (featureName: FeatureName, enabled: boolean) => {
    setFeature({
      request: { setProductFeatureRequestBody: { featureName, enabled } },
    });
  };

  return (
    <>
      <Heading variant="h4" className="mb-2">
        Hook Behavior
      </Heading>
      <Type muted small className="mb-6">
        Operational settings for hook plugins running on your users&apos;
        machines. Data capture settings live under Data → Configuration.
      </Type>
      <div className="border-border bg-card rounded-lg border p-4">
        <Stack gap={4}>
          <FeatureToggleRow
            icon={Unplug}
            title="Fail Open During Outages"
            description="Let tool calls proceed while Speakeasy is unreachable, instead of blocking them (the default). Blocking policies go unenforced during the outage; events are still recorded and scanned after recovery. Invalid credentials always block."
            checked={effectiveHooksFailOpenEnabled}
            onCheckedChange={(enabled) =>
              setNamedFeature(FeatureName.HooksFailOpen, enabled)
            }
            disabled={isMutating}
            ready={!!featuresData}
            ariaLabel="Fail open during outages"
          />

          <div className="border-border border-t" />

          <FeatureToggleRow
            icon={LogIn}
            title="Hook Browser Sign-In"
            description="Let hook plugins sign users in through the browser to record events under their own identity. When off, plugins use the organization key or explicitly configured credentials."
            checked={effectiveHooksBrowserLoginEnabled}
            onCheckedChange={(enabled) =>
              setNamedFeature(FeatureName.HooksBrowserLogin, enabled)
            }
            disabled={isMutating}
            ready={!!featuresData}
            ariaLabel="Enable hook browser sign-in"
          />
        </Stack>
      </div>
    </>
  );
}
