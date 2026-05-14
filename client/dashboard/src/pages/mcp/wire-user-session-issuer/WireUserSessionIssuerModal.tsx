import { Dialog } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Type } from "@/components/ui/type";
import { Toolset } from "@/lib/toolTypes";
import { Button, Stack } from "@speakeasy-api/moonshine";
import { AlertTriangle, CheckCircle2, Loader2 } from "lucide-react";

import {
  type MigrationFormState,
  type MigrationStep,
  type MigrationStepKey,
  useOAuthProxyMigration,
} from "./useOAuthProxyMigration";

// WireUserSessionIssuerModal renders the admin workflow for porting an MCP
// toolset off the legacy OAuth Proxy paradigm onto user_session_issuer +
// remote_session_issuer + remote_session_client. The step-by-step driver
// lives in useOAuthProxyMigration; this file is the presentation surface.
export function WireUserSessionIssuerModal({
  isOpen,
  onClose,
  toolset,
}: {
  isOpen: boolean;
  onClose: () => void;
  toolset: Toolset;
}) {
  return (
    <Dialog open={isOpen} onOpenChange={onClose}>
      <Dialog.Content className="max-h-[90vh] max-w-2xl">
        <Dialog.Header>
          <Dialog.Title>Wire User Session Issuer</Dialog.Title>
          <Dialog.Description>
            Port the OAuth Proxy configuration on{" "}
            <span className="font-medium">{toolset.name ?? toolset.slug}</span>{" "}
            onto a user session issuer paired with a remote session issuer and
            client.
          </Dialog.Description>
        </Dialog.Header>
        <WireUserSessionIssuerBody toolset={toolset} onClose={onClose} />
      </Dialog.Content>
    </Dialog>
  );
}

function WireUserSessionIssuerBody({
  toolset,
  onClose,
}: {
  toolset: Toolset;
  onClose: () => void;
}) {
  const migration = useOAuthProxyMigration(toolset);

  if (!migration.ready) {
    return (
      <Stack gap={4} className="py-4">
        <Callout tone="warn">
          {migration.reason === "no-proxy-provider"
            ? "This toolset doesn't have an OAuth proxy provider configured, so there is nothing to port."
            : "This toolset is on the Gram-managed OAuth paradigm, which doesn't carry an upstream client to clone."}
        </Callout>
        <Dialog.Footer>
          <Button onClick={onClose}>
            <Button.Text>Close</Button.Text>
          </Button>
        </Dialog.Footer>
      </Stack>
    );
  }

  const { steps, currentStep, isComplete, form, setForm, runCurrentStep } =
    migration;
  const runningStep = steps.find((s) => s.status === "running") ?? null;
  const errorStep = steps.find((s) => s.status === "error") ?? null;

  return (
    <Stack gap={4} className="py-4">
      <StepIndicator steps={steps} />
      {isComplete ? (
        <Callout tone="success">
          Migration complete. Use the new user session issuer to authenticate
          MCP clients going forward.
        </Callout>
      ) : currentStep ? (
        <CurrentStepBody
          step={currentStep}
          form={form}
          setForm={setForm}
          proxyProviderSlug={migration.defaults.proxyProvider.slug}
          issuerOriginGuess={migration.defaults.issuerOriginGuess}
        />
      ) : null}
      {errorStep?.error && <Callout tone="error">{errorStep.error}</Callout>}
      <Dialog.Footer>
        <Button variant="tertiary" onClick={onClose}>
          <Button.Text>{isComplete ? "Done" : "Cancel"}</Button.Text>
        </Button>
        {!isComplete && (
          <Button
            onClick={() => void runCurrentStep()}
            disabled={runningStep !== null}
          >
            <Button.Text>
              {runningStep
                ? "Working…"
                : errorStep
                  ? "Retry"
                  : currentStep?.key === "remoteSessionClient"
                    ? "Clone client"
                    : "Continue"}
            </Button.Text>
          </Button>
        )}
      </Dialog.Footer>
    </Stack>
  );
}

function StepIndicator({ steps }: { steps: MigrationStep[] }) {
  return (
    <ol className="flex items-center gap-3 text-sm">
      {steps.map((s, idx) => (
        <li key={s.key} className="flex items-center gap-2">
          <StepIcon status={s.status} ordinal={idx + 1} />
          <span
            className={
              s.status === "done"
                ? "text-muted-foreground line-through"
                : s.status === "running"
                  ? "font-medium"
                  : ""
            }
          >
            {s.label}
          </span>
          {idx < steps.length - 1 && (
            <span className="text-muted-foreground">→</span>
          )}
        </li>
      ))}
    </ol>
  );
}

function StepIcon({
  status,
  ordinal,
}: {
  status: MigrationStep["status"];
  ordinal: number;
}) {
  if (status === "done")
    return <CheckCircle2 className="h-4 w-4 text-green-600" />;
  if (status === "running") return <Loader2 className="h-4 w-4 animate-spin" />;
  if (status === "error")
    return <AlertTriangle className="text-destructive h-4 w-4" />;
  return (
    <span className="border-muted-foreground text-muted-foreground flex h-4 w-4 items-center justify-center rounded-full border text-[10px]">
      {ordinal}
    </span>
  );
}

function CurrentStepBody({
  step,
  form,
  setForm,
  proxyProviderSlug,
  issuerOriginGuess,
}: {
  step: MigrationStep;
  form: MigrationFormState;
  setForm: (patch: Partial<MigrationFormState>) => void;
  proxyProviderSlug: string;
  issuerOriginGuess: string | null;
}) {
  switch (step.key satisfies MigrationStepKey) {
    case "userSessionIssuer":
      return (
        <Stack gap={3}>
          <FieldLabel label="User session issuer slug">
            <Input
              value={form.userSessionIssuerSlug}
              onChange={(value) => setForm({ userSessionIssuerSlug: value })}
            />
          </FieldLabel>
          <FieldLabel label="Session duration (hours)">
            <Input
              type="number"
              min={1}
              value={String(form.sessionDurationHours)}
              onChange={(value) =>
                setForm({ sessionDurationHours: Number(value) || 0 })
              }
            />
          </FieldLabel>
          <Type small className="text-muted-foreground">
            authn_challenge_mode is fixed to{" "}
            <code className="font-mono">interactive</code> — the chain mode is
            retained only for legacy proxies.
          </Type>
        </Stack>
      );
    case "remoteSessionIssuer":
      return (
        <Stack gap={3}>
          <FieldLabel label="Remote session issuer slug">
            <Input
              value={form.remoteSessionIssuerSlug}
              onChange={(value) => setForm({ remoteSessionIssuerSlug: value })}
            />
          </FieldLabel>
          <FieldLabel label="Issuer URL">
            <Input
              value={form.issuerUrl}
              placeholder={issuerOriginGuess ?? "https://idp.example.com"}
              onChange={(value) => setForm({ issuerUrl: value })}
            />
          </FieldLabel>
          <Type small className="text-muted-foreground">
            Gram hits this URL's RFC 8414 well-known document to prefill the
            authorization, token, registration, and JWKS endpoints. If the
            upstream does not publish one, Gram falls back to the endpoints
            already stored on the OAuth proxy provider.
          </Type>
        </Stack>
      );
    case "remoteSessionClient":
      return (
        <Stack gap={3}>
          <Callout tone="warn">
            Confirm that the upstream IdP has redirect URIs registered for the
            new user-session flow. Gram is going to reuse the client_id stored
            on the <code className="font-mono">{proxyProviderSlug}</code> proxy
            provider, so any redirect URIs already registered with that
            client_id keep working — but if you previously registered URIs tied
            to the OAuth proxy callback, double-check they cover the
            user-session callback too.
          </Callout>
          <Type small className="text-muted-foreground">
            Clicking Clone reads the upstream client_id / client_secret out of
            the OAuth proxy provider and persists them on a new
            remote_session_client. The secret never leaves the server.
          </Type>
        </Stack>
      );
  }
}

function FieldLabel({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <Stack gap={1}>
      <Type small className="font-medium">
        {label}
      </Type>
      {children}
    </Stack>
  );
}

function Callout({
  tone,
  children,
}: {
  tone: "warn" | "error" | "success";
  children: React.ReactNode;
}) {
  const toneClasses =
    tone === "error"
      ? "border-destructive/40 bg-destructive/5 text-destructive"
      : tone === "success"
        ? "border-green-500/40 bg-green-50 text-green-900 dark:bg-green-950 dark:text-green-200"
        : "border-amber-500/40 bg-amber-50 text-amber-900 dark:bg-amber-950 dark:text-amber-200";
  return (
    <div
      className={`rounded-md border px-3 py-2 text-sm ${toneClasses}`}
      role={tone === "error" ? "alert" : undefined}
    >
      {children}
    </div>
  );
}
