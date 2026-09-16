import { useState } from "react";
import type { IdentityProviderConnection } from "@gram/client/models/components/identityproviderconnection.js";
import type { IdentityProviderFieldOutcome } from "@gram/client/models/components/identityproviderfieldoutcome.js";
import { useGenerateWorkOSAdminPortalLinkMutation } from "@gram/client/react-query/generateWorkOSAdminPortalLink.js";
import { invalidateAllIdentityProvider } from "@gram/client/react-query/identityProvider.js";
import {
  invalidateAllIdentityProviderSetup,
  useIdentityProviderSetup,
} from "@gram/client/react-query/identityProviderSetup.js";
import { useSubmitIdentityProviderSetupStepMutation } from "@gram/client/react-query/submitIdentityProviderSetupStep.js";
import { useVerifyIdentityProviderSetupStepMutation } from "@gram/client/react-query/verifyIdentityProviderSetupStep.js";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Skeleton } from "@/components/ui/Skeleton";
import { openSafeExternalUrl } from "@/lib/safe-external-url";
import { getServerURL } from "@/lib/utils";
import { StepSection } from "../step-section";
import { errorMessage, isUnavailable } from "./identity-provider-errors";
import { IdentityProviderSetupStepPanel } from "./identity-provider-setup-step";

const DIRECTORY_STEP_KEY = "directory";

export function OktaDirectorySection({
  index,
  connection,
}: {
  index: number;
  connection: IdentityProviderConnection | undefined;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [fieldOutcomes, setFieldOutcomes] = useState<
    IdentityProviderFieldOutcome[]
  >([]);
  const active = connection?.status === "active";
  const setup = useIdentityProviderSetup(undefined, undefined, {
    enabled: active,
    throwOnError: false,
  });
  const step = setup.data?.steps.find((candidate) => {
    return candidate.key === DIRECTORY_STEP_KEY;
  });
  const submit = useSubmitIdentityProviderSetupStepMutation();
  const verify = useVerifyIdentityProviderSetupStepMutation();
  const portalLink = useGenerateWorkOSAdminPortalLinkMutation({
    onError: (error) => {
      toast.error(errorMessage(error, "Failed to open the setup portal"));
    },
  });

  const refresh = async () => {
    await Promise.all([
      invalidateAllIdentityProvider(queryClient),
      invalidateAllIdentityProviderSetup(queryClient),
    ]);
  };

  const submitValues = (values: { key: string; value: string }[]) => {
    submit.mutate(
      {
        request: {
          submitSetupStepRequestBody: { stepKey: DIRECTORY_STEP_KEY, values },
        },
      },
      {
        onSuccess: (result) => {
          setFieldOutcomes(result.fieldOutcomes);
          // The printed values and the evidence rows are both read back from
          // the step, so re-assigning groups repaints them rather than leaving
          // the counts from before on screen.
          void refresh();
        },
      },
    );
  };

  const directoryState = connection?.directoryState ?? "not_started";
  const applicationExists = directoryState !== "not_started";
  const guided = step && !step.portalIntent;
  let body: JSX.Element | null = null;

  if (setup.isPending) {
    body = (
      <Skeleton>
        <div className="h-[220px] w-full" />
      </Skeleton>
    );
  } else if (step) {
    body = (
      <IdentityProviderSetupStepPanel
        step={step}
        fieldOutcomes={fieldOutcomes}
        isSubmitting={submit.isPending}
        submitError={
          submit.error
            ? errorMessage(submit.error, "Could not set up directory sync")
            : undefined
        }
        onSubmit={submitValues}
        submitLabel="Set up directory sync"
        allowSubmitWithoutValues={Boolean(guided && !applicationExists)}
        verifyLabel="Check the directory"
        deepLinkLabel="Open the Provisioning tab in Okta"
        isVerifying={verify.isPending}
        onVerify={() => {
          verify.mutate(
            {
              request: {
                verifySetupStepRequestBody: { stepKey: DIRECTORY_STEP_KEY },
              },
            },
            { onSuccess: () => void refresh() },
          );
        }}
        verifyResult={verify.data ?? step.lastOutcome}
        verifyUnavailable={
          isUnavailable(verify.error)
            ? "Speakeasy cannot run this check yet. Come back and check once it is available."
            : undefined
        }
        portal={
          step.portalIntent
            ? {
                note: "Open the directory setup portal, finish the connection, then return here.",
                isPending: portalLink.isPending,
                onConnect: () => {
                  portalLink.mutate(
                    {
                      request: {
                        generateWorkOSAdminPortalLinkRequestBody: {
                          intent: step.portalIntent!,
                          successUrl: `${getServerURL()}/v1/setup/callback?intent=${step.portalIntent!}`,
                          returnUrl: window.location.href,
                        },
                      },
                    },
                    {
                      onSuccess: (data) => {
                        openSafeExternalUrl(data.url);
                      },
                    },
                  );
                },
              }
            : undefined
        }
      />
    );
  }

  return (
    <StepSection
      index={index}
      slug="directory-sync"
      title="Directory sync"
      description="Keep users, groups, and roles in step with Okta automatically."
      complete={directoryState === "passed"}
      locked={!active}
      badge={active ? undefined : "Waiting"}
    >
      {body}
    </StepSection>
  );
}
