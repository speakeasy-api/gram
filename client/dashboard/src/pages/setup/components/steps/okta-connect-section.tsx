import { useState } from "react";
import type { IdentityProviderConnection } from "@gram/client/models/components/identityproviderconnection.js";
import type { IdentityProviderFieldOutcome } from "@gram/client/models/components/identityproviderfieldoutcome.js";
import { useCreateIdentityProviderMutation } from "@gram/client/react-query/createIdentityProvider.js";
import { useDeleteIdentityProviderMutation } from "@gram/client/react-query/deleteIdentityProvider.js";
import { invalidateAllIdentityProvider } from "@gram/client/react-query/identityProvider.js";
import {
  invalidateAllIdentityProviderSetup,
  useIdentityProviderSetup,
} from "@gram/client/react-query/identityProviderSetup.js";
import { useSubmitIdentityProviderSetupStepMutation } from "@gram/client/react-query/submitIdentityProviderSetupStep.js";
import { useVerifyIdentityProviderSetupStepMutation } from "@gram/client/react-query/verifyIdentityProviderSetupStep.js";
import { useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/Button";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/Field";
import { Input } from "@/components/ui/Input";
import { Skeleton } from "@/components/ui/Skeleton";
import { errorMessage, isUnavailable } from "./identity-provider-errors";
import { IdentityProviderSetupStepPanel } from "./identity-provider-setup-step";
import { OktaConnectionSummary } from "./okta-connection-summary";

/** The step this slice drives. Phase 2 adds its own alongside it. */
const CONNECT_STEP_KEY = "connect";

// Asking for the tenant first is what lets every later instruction be a link
// into the administrator's own console rather than a description of it.
function TenantUrlForm({
  onCreate,
  isPending,
  error,
}: {
  onCreate: (tenantUrl: string) => void;
  isPending: boolean;
  error?: string;
}): JSX.Element {
  const [tenantUrl, setTenantUrl] = useState("");

  return (
    <div className="border-border bg-card space-y-4 border p-5">
      <Field>
        <FieldLabel htmlFor="okta-tenant-url">Okta organization URL</FieldLabel>
        <Input
          id="okta-tenant-url"
          value={tenantUrl}
          onChange={setTenantUrl}
          placeholder="https://your-org.okta.com"
          error={!!error}
          disabled={isPending}
          onEnter={() => {
            if (tenantUrl.trim()) onCreate(tenantUrl.trim());
          }}
        />
        {error ? <FieldError>{error}</FieldError> : null}
        <FieldDescription>
          Found in the top right of your Okta Admin Console. This is what turns
          every instruction below into a link straight into your own console.
        </FieldDescription>
      </Field>
      <Button
        variant="primary"
        size="sm"
        disabled={!tenantUrl.trim() || isPending}
        onClick={() => onCreate(tenantUrl.trim())}
      >
        {isPending ? "Connecting..." : "Connect Okta"}
      </Button>
    </div>
  );
}

// Removing is the only way back once a connection exists, so it stays in the
// same place the provider escape hatch was — de-emphasized, and asking once.
function RemoveConnection({
  onRemove,
  isPending,
}: {
  onRemove: () => void;
  isPending: boolean;
}): JSX.Element {
  const [confirming, setConfirming] = useState(false);

  if (!confirming) {
    return (
      <Button
        variant="tertiary"
        size="sm"
        onClick={() => setConfirming(true)}
        className="text-muted-foreground hover:text-foreground -ml-2 underline decoration-dotted underline-offset-4 hover:decoration-solid"
      >
        Remove connection
      </Button>
    );
  }

  return (
    <div className="flex flex-wrap items-center gap-3">
      <p className="text-muted-foreground text-sm">
        Remove this connection? Speakeasy stops reading your tenant. The
        application in Okta is yours to delete.
      </p>
      <Button
        variant="destructive-primary"
        size="sm"
        onClick={onRemove}
        disabled={isPending}
      >
        {isPending ? "Removing..." : "Remove"}
      </Button>
      <Button
        variant="tertiary"
        size="sm"
        onClick={() => setConfirming(false)}
        disabled={isPending}
      >
        Keep it
      </Button>
    </div>
  );
}

interface OktaConnectSectionProps {
  connection: IdentityProviderConnection | undefined;
  isLoadingConnection: boolean;
}

// The first guided sub-step: the ceremony in the administrator's console, then
// the exchange the server describes — what Speakeasy prints, where to put it,
// and what comes back — and the connection itself once it has proved out.
export function OktaConnectSection({
  connection,
  isLoadingConnection,
}: OktaConnectSectionProps): JSX.Element {
  const queryClient = useQueryClient();
  const [fieldOutcomes, setFieldOutcomes] = useState<
    IdentityProviderFieldOutcome[]
  >([]);

  // describeSetup 404s with no connection, so it only runs once there is one.
  const setup = useIdentityProviderSetup(undefined, undefined, {
    enabled: !!connection,
    throwOnError: false,
  });

  const refresh = async () => {
    await Promise.all([
      invalidateAllIdentityProvider(queryClient),
      invalidateAllIdentityProviderSetup(queryClient),
    ]);
  };

  const create = useCreateIdentityProviderMutation();
  const submit = useSubmitIdentityProviderSetupStepMutation();
  const verify = useVerifyIdentityProviderSetupStepMutation();
  const remove = useDeleteIdentityProviderMutation();

  const active = connection?.status === "active";
  const step = setup.data?.steps.find((s) => s.key === CONNECT_STEP_KEY);

  if (isLoadingConnection) {
    return (
      <Skeleton>
        <div className="h-[220px] w-full" />
      </Skeleton>
    );
  }

  return (
    <div className="space-y-6">
      {connection ? (
        <RemoveConnection
          isPending={remove.isPending}
          onRemove={() => {
            remove.mutate(
              { request: { id: connection.id } },
              { onSuccess: () => void refresh() },
            );
          }}
        />
      ) : null}

      {!connection ? (
        <TenantUrlForm
          isPending={create.isPending}
          error={
            create.error
              ? errorMessage(create.error, "Could not connect to Okta")
              : undefined
          }
          onCreate={(tenantUrl) => {
            create.mutate(
              { request: { createRequestBody2: { kind: "okta", tenantUrl } } },
              { onSuccess: () => void refresh() },
            );
          }}
        />
      ) : null}

      {active ? (
        <OktaConnectionSummary
          connection={connection}
          isChecking={verify.isPending}
          onRecheck={() => {
            verify.mutate(
              {
                request: {
                  verifySetupStepRequestBody: { stepKey: CONNECT_STEP_KEY },
                },
              },
              // The capabilities table and the checked-at line are both read
              // back from the connection, so the result has to land before
              // either can change.
              { onSuccess: () => void refresh() },
            );
          }}
        />
      ) : null}

      {connection && !active && step ? (
        <IdentityProviderSetupStepPanel
          step={step}
          fieldOutcomes={fieldOutcomes}
          isSubmitting={submit.isPending}
          submitError={
            submit.error
              ? errorMessage(submit.error, "Could not save that value")
              : undefined
          }
          onSubmit={(values) => {
            submit.mutate(
              {
                request: {
                  submitSetupStepRequestBody: {
                    stepKey: step.key,
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
          }}
          isVerifying={verify.isPending}
          onVerify={() => {
            verify.mutate(
              {
                request: { verifySetupStepRequestBody: { stepKey: step.key } },
              },
              { onSuccess: () => void refresh() },
            );
          }}
          verifyResult={verify.data ?? step.lastOutcome}
          verifyUnavailable={
            isUnavailable(verify.error)
              ? "Speakeasy cannot run the check against Okta yet. Your values are saved — come back and verify once it is available."
              : undefined
          }
        />
      ) : null}

      {connection && !active && setup.isPending ? (
        <Skeleton>
          <div className="h-[160px] w-full" />
        </Skeleton>
      ) : null}
    </div>
  );
}
