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
import { Text } from "@/components/ui/Text";
import { openSafeExternalUrl } from "@/lib/safe-external-url";
import { getServerURL } from "@/lib/utils";
import { StepSection } from "../step-section";
import { errorMessage, isUnavailable } from "./identity-provider-errors";
import { IdentityProviderSetupStepPanel } from "./identity-provider-setup-step";

/** The step this section drives. */
const SIGN_IN_STEP_KEY = "sign_in";

/**
 * What the administrator carries from Okta into the setup portal. The secret is
 * on the Okta screen the deep link opens and goes straight into the portal, so
 * Speakeasy has no field for it and never sees it.
 */
const PORTAL_NOTE =
  "Open the app in Okta, copy its client secret from that screen, and paste it into the setup portal along with the values above. The secret goes from Okta to the portal directly — Speakeasy never receives it and has nowhere to put it.";

function settledGroupsChoice(
  connection: IdentityProviderConnection,
): string | undefined {
  if (connection.groupsSource === "directory") {
    return "Group membership is read from the directory rather than the sign-in token. Step 3 does that work.";
  }
  if (connection.groupsClaimConfirmed) {
    return "Okta is set to send group membership in the sign-in token.";
  }
  return undefined;
}

// Step two of the guided journey: Speakeasy creates the sign-in application in
// Okta, the administrator finishes the provider connection in the setup portal
// because that API is not open to us here, and the check reads both back.
export function OktaSignOnSection({
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

  // The sign-on step only exists once the connection itself has passed.
  const active = connection?.status === "active";
  const setup = useIdentityProviderSetup(undefined, undefined, {
    enabled: active,
    throwOnError: false,
  });
  const step = setup.data?.steps.find((s) => s.key === SIGN_IN_STEP_KEY);

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
          submitSetupStepRequestBody: {
            stepKey: SIGN_IN_STEP_KEY,
            values,
          },
        },
      },
      {
        onSuccess: (result) => {
          setFieldOutcomes(result.fieldOutcomes);
          void refresh();
        },
      },
    );
  };

  const signInState = connection?.signInState ?? "not_started";
  const applicationExists = signInState !== "not_started";
  // Speakeasy creating the application is a submit that carries nothing: the
  // administrator is agreeing to it, not supplying anything.
  const createsApplication =
    step?.where === "our_page" && step.expectedValues.length === 0;

  let body: JSX.Element | null = null;
  if (setup.isPending) {
    body = (
      <Skeleton>
        <div className="h-[200px] w-full" />
      </Skeleton>
    );
  } else if (step) {
    body = (
      <div className="space-y-4">
        {applicationExists ? (
          <Text variant="small" muted>
            The sign-in application was created in Okta.
          </Text>
        ) : null}
        <IdentityProviderSetupStepPanel
          step={step}
          fieldOutcomes={fieldOutcomes}
          isSubmitting={submit.isPending}
          submitError={
            submit.error
              ? errorMessage(submit.error, "Could not save that")
              : undefined
          }
          onSubmit={submitValues}
          submitLabel={
            createsApplication ? "Create the sign-in application" : undefined
          }
          allowSubmitWithoutValues={createsApplication && !applicationExists}
          verifyLabel="Check the connection"
          isVerifying={verify.isPending}
          onVerify={() => {
            verify.mutate(
              {
                request: {
                  verifySetupStepRequestBody: { stepKey: SIGN_IN_STEP_KEY },
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
            step.portalIntent && applicationExists
              ? {
                  note: PORTAL_NOTE,
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
          repair={
            applicationExists && connection
              ? {
                  isPending: submit.isPending,
                  settled: settledGroupsChoice(connection),
                  valueKeys: ["groups_claim_confirmed", "groups_source"],
                  onConfirm: () =>
                    submitValues([
                      { key: "groups_claim_confirmed", value: "true" },
                    ]),
                  onFallback: () =>
                    submitValues([
                      { key: "groups_source", value: "directory" },
                    ]),
                }
              : undefined
          }
        />
      </div>
    );
  }

  return (
    <StepSection
      index={index}
      slug="single-sign-on"
      title="Single sign-on"
      description={
        active
          ? "Speakeasy creates the sign-in application in Okta and reads it back to prove it."
          : "Speakeasy creates the sign-in application in Okta and proves it with a real sign-in."
      }
      complete={signInState === "passed"}
      locked={!active}
      badge={active ? undefined : "Waiting"}
    >
      {body}
    </StepSection>
  );
}
