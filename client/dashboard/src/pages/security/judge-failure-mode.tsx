import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { useIsCurrentOrganization } from "@/hooks/useIsCurrentOrganization";
import { handleAPIError } from "@/lib/errors";
import { FeatureName } from "@gram/client/models/components/setproductfeaturerequestbody.js";
import { useFeaturesSetMutation } from "@gram/client/react-query/featuresSet.js";
import {
  invalidateAllProductFeatures,
  useProductFeatures,
} from "@gram/client/react-query/productFeatures.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import { SectionHeader } from "./policy-editor-shell";

export type JudgeFailureMode = {
  features: ReturnType<typeof useProductFeatures>;
  /**
   * Whether a prompt policy blocks the message when its judge times out or
   * errors. Organization-wide; undefined until the setting loads.
   */
  failClosed: boolean | undefined;
  setFailClosed: (
    failClosed: boolean,
    options?: { onSuccess?: () => void },
  ) => void;
  isPending: boolean;
};

/**
 * The organization's risk failure mode, which decides what every prompt
 * policy does when its judge can't answer. Shared by the editor's card, the
 * review summary, and the guardrail replay so all three read the same value.
 */
export function useJudgeFailureMode(): JudgeFailureMode {
  const organization = useOrganization();
  const isCurrentOrganization = useIsCurrentOrganization(organization.id);
  const queryClient = useQueryClient();
  const features = useProductFeatures(
    { organizationId: organization.id },
    undefined,
    { throwOnError: false },
  );
  const mutation = useFeaturesSetMutation({
    onSuccess: async (_, variables) => {
      if (!isCurrentOrganization()) return;
      await invalidateAllProductFeatures(queryClient);
      if (!isCurrentOrganization()) return;
      toast.success(
        variables.request.setProductFeatureRequestBody.enabled
          ? "Prompt policies now block when the judge can't answer"
          : "Prompt policies now allow when the judge can't answer",
      );
    },
    onError: (error) => {
      if (!isCurrentOrganization()) return;
      handleAPIError(error, "Failed to update the judge failure setting");
    },
  });

  const setFailClosed = (
    failClosed: boolean,
    options?: { onSuccess?: () => void },
  ) =>
    mutation.mutate(
      {
        request: {
          setProductFeatureRequestBody: {
            organizationId: organization.id,
            featureName: FeatureName.RiskEnforcementFailClosed,
            enabled: failClosed,
          },
        },
      },
      options,
    );

  return {
    features,
    failClosed: features.data?.riskEnforcementFailClosed,
    setFailClosed,
    isPending: mutation.isPending,
  };
}

/**
 * Its own card, apart from the Judge card, because it saves at once and
 * applies to every prompt policy, while the Judge card waits for Save and
 * applies to this policy only. Blocking asks first: during a judge outage it
 * stops matching traffic across the organization.
 */
export function JudgeFailureModeCard({
  mode,
}: {
  mode: JudgeFailureMode;
}): JSX.Element {
  const { features, failClosed, setFailClosed, isPending } = mode;
  const [confirming, setConfirming] = useState(false);

  return (
    <Card>
      <SectionHeader
        title="When the judge can't answer"
        description="Organization setting: applies to every prompt policy and saves right away."
      />
      <div className="flex items-start justify-between gap-4">
        <div className="space-y-1">
          <Text small>
            {failClosed === undefined
              ? "Allow or block"
              : failClosed
                ? "Block the message"
                : "Allow the message"}
          </Text>
          <Text small muted className="max-w-xl">
            If the judge times out or errors while enforcing a policy, allowing
            avoids false blocks; blocking is stricter, but an outage can
            interrupt legitimate work.
          </Text>
          {features.error && (
            <Text small className="text-warning" role="alert">
              Couldn&apos;t load this setting.{" "}
              <Button
                variant="tertiary"
                size="sm"
                disabled={features.isFetching}
                onClick={() => void features.refetch()}
              >
                {features.isFetching ? "Retrying…" : "Retry"}
              </Button>
            </Text>
          )}
        </div>
        <RequireScope
          scope="org:admin"
          level="component"
          reason="Only organization admins can change this setting."
        >
          <Switch
            checked={failClosed ?? false}
            onCheckedChange={(checked) =>
              checked ? setConfirming(true) : setFailClosed(false)
            }
            disabled={isPending || failClosed === undefined}
            aria-label="Block the message when the judge can't answer"
          />
        </RequireScope>
      </div>
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title="Block when the judge can't answer?"
        description={
          <>
            Every prompt policy in your organization will block the message
            whenever its judge times out or errors, starting now. During an
            outage that stops matching work until the judge recovers.
            <br />
            <br />
            You can switch back to allowing at any time.
          </>
        }
        confirmLabel="Block"
        pendingLabel="Saving…"
        isPending={isPending}
        onConfirm={() =>
          setFailClosed(true, { onSuccess: () => setConfirming(false) })
        }
      />
    </Card>
  );
}
