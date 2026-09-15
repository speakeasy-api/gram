import { useState, type ReactNode } from "react";
import { ArrowLeft } from "lucide-react";
import type { IdentityProviderConnection } from "@gram/client/models/components/identityproviderconnection.js";
import type { IdentityProviderFieldOutcome } from "@gram/client/models/components/identityproviderfieldoutcome.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
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
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/Field";
import { Input } from "@/components/ui/Input";
import { Skeleton } from "@/components/ui/Skeleton";
import { IdentityProviderSetupStepPanel } from "./identity-provider-setup-step";
import { OktaConnectionSummary } from "./okta-connection-summary";

/**
 * API scopes the administrator grants the Okta app. Read access covers the
 * directory and the application inventory; the single write scope is what
 * lets Speakeasy create the sign-in application in the next sub-step.
 */
const OKTA_API_SCOPES = [
  "okta.groups.read",
  "okta.users.read",
  "okta.apps.read",
  "okta.apps.manage",
];

/** The step this slice drives. Phase 2 adds its own alongside it. */
const CONNECT_STEP_KEY = "connect";

function errorMessage(error: unknown, fallback: string): string {
  if (error instanceof Error && error.message) return error.message;
  return fallback;
}

/**
 * Verification is stubbed until the backend's check lands. Matched on the base
 * error class, not ServiceError: the SDK only decodes a typed body for 4XX,
 * 500 and 502, so a 503 arrives as whichever GramError it fell back to.
 */
function isUnavailable(error: unknown): boolean {
  return error instanceof GramError && error.statusCode === 503;
}

/**
 * A control or value named exactly as the Okta Admin Console spells it. The
 * administrator is reading our instructions with their console open, so these
 * mirror that screen rather than anything in our own model.
 */
function ConsoleLabel({ children }: { children: ReactNode }): JSX.Element {
  return <span className="text-foreground font-mono text-xs">{children}</span>;
}

function ConsoleStep({
  index,
  children,
}: {
  index: number;
  children: ReactNode;
}): JSX.Element {
  return (
    <li className="flex gap-3">
      <div
        aria-hidden="true"
        className="border-border text-muted-foreground flex h-6 w-6 flex-shrink-0 items-center justify-center border text-xs font-semibold"
      >
        {index}
      </div>
      <div className="min-w-0 flex-1 space-y-1">{children}</div>
    </li>
  );
}

// What the administrator does in their own console, ahead of the exchange the
// server describes. Deliberately not driven by the step data: this is the cost
// of the step stated before they start, where the step's own instructions are
// terse reminders sitting next to the values they apply to.
function ConsoleCeremony(): JSX.Element {
  return (
    <>
      <Alert variant="warning" alignTop>
        <div>
          <AlertTitle>This step needs an Okta Super Administrator</AlertTitle>
          <AlertDescription>
            Granting API scopes to a service app is a Super Administrator action
            in Okta. If that is not you, the four console steps below have to go
            to someone who holds that role — everything after this step is back
            in Speakeasy.
          </AlertDescription>
        </div>
      </Alert>

      <div className="border-border bg-card border p-5">
        <h4 className="text-foreground text-sm leading-5 font-semibold">
          What you will do in the Okta Admin Console
        </h4>
        <p className="text-muted-foreground mt-1 text-sm">
          Four steps, once. Nothing secret comes back to Speakeasy.
        </p>

        <ol className="mt-5 space-y-5">
          <ConsoleStep index={1}>
            <p className="text-foreground text-sm">
              Create an <ConsoleLabel>API Services</ConsoleLabel> app
              integration.
            </p>
            <p className="text-muted-foreground text-sm">
              Applications › Create App Integration › API Services.
            </p>
          </ConsoleStep>

          <ConsoleStep index={2}>
            <p className="text-foreground text-sm">
              Set client authentication to{" "}
              <ConsoleLabel>Public key / Private key</ConsoleLabel>, choose{" "}
              <ConsoleLabel>Use a URL</ConsoleLabel>, and paste the address
              Speakeasy publishes its public keys at.
            </p>
            <p className="text-muted-foreground text-sm">
              Okta reads the key from that address, so rotating it later never
              means opening Okta again.
            </p>
          </ConsoleStep>

          <ConsoleStep index={3}>
            <p className="text-foreground text-sm">
              Grant these API scopes on the app&apos;s Okta API Scopes tab.
            </p>
            <div className="flex flex-wrap gap-1.5 pt-1">
              {OKTA_API_SCOPES.map((scope) => (
                <Badge
                  key={scope}
                  variant="neutral"
                  size="sm"
                  className="tracking-normal normal-case"
                >
                  <Badge.Text>{scope}</Badge.Text>
                </Badge>
              ))}
            </div>
            <p className="text-muted-foreground text-sm">
              One write scope in the whole flow.{" "}
              <ConsoleLabel>okta.apps.manage</ConsoleLabel> is what lets
              Speakeasy create the sign-in application for you in the next step;
              leave it out and you configure that application by hand instead.
            </p>
          </ConsoleStep>

          <ConsoleStep index={4}>
            <p className="text-foreground text-sm">
              Assign the app the{" "}
              <ConsoleLabel>Read-only Administrator</ConsoleLabel> and{" "}
              <ConsoleLabel>Application Administrator</ConsoleLabel> roles.
            </p>
            <p className="text-muted-foreground text-sm">
              Okta requires an admin role on an API Services app before the
              scopes take effect.
            </p>
          </ConsoleStep>
        </ol>
      </div>

      <Alert variant="info" alignTop>
        <div>
          <AlertTitle>There is no secret to hand over</AlertTitle>
          <AlertDescription>
            Okta authenticates this kind of app with a public key rather than a
            shared secret, so nothing sensitive is pasted in either direction.
            Speakeasy&apos;s private key stays in Speakeasy and is never
            displayed, exported, or returned by anything.
          </AlertDescription>
        </div>
      </Alert>
    </>
  );
}

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
  /** Returns the card to the provider grid. Only while nothing is connected. */
  onChangeProvider: () => void;
  connection: IdentityProviderConnection | undefined;
  isLoadingConnection: boolean;
}

// The first guided sub-step: the ceremony in the administrator's console, then
// the exchange the server describes — what Speakeasy prints, where to put it,
// and what comes back — and the connection itself once it has proved out.
export function OktaConnectSection({
  onChangeProvider,
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
      ) : (
        <Button
          variant="tertiary"
          size="sm"
          onClick={onChangeProvider}
          className="text-muted-foreground hover:text-foreground -ml-2 gap-1.5"
        >
          <ArrowLeft className="h-4 w-4" />
          Choose a different provider
        </Button>
      )}

      {/* Once the connection is proved, the ceremony is history: the summary
          says what it can do instead of repeating how to set it up. */}
      {active ? null : <ConsoleCeremony />}

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

      {active ? <OktaConnectionSummary connection={connection} /> : null}

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
