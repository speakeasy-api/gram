import { Dialog } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Type } from "@/components/ui/type";
import { Toolset } from "@/lib/toolTypes";
import type { RemoteSessionIssuer } from "@gram/client/models/components";
import { Button, Stack } from "@speakeasy-api/moonshine";
import { AlertTriangle, CheckCircle2, Loader2 } from "lucide-react";

import {
  type ClientStrategy,
  type MigrationFormState,
  type MigrationParadigm,
  type MigrationStep,
  type MigrationStepKey,
  REMOTE_LOGIN_CALLBACK_URL,
  useOAuthProxyMigration,
} from "./useOAuthProxyMigration";

// Refresh-token lifetime options. Stored on the wire as hours so the
// user_session_issuer payload stays a flat integer, but presented to the
// admin in human units. The DEFAULT_SESSION_DURATION_HOURS in defaults.ts
// should match one of these values so the chooser doesn't open blank.
const REFRESH_TOKEN_DURATION_OPTIONS: ReadonlyArray<{
  label: string;
  hours: number;
}> = [
  { label: "1 week", hours: 24 * 7 },
  { label: "2 weeks", hours: 24 * 14 },
  { label: "1 month", hours: 24 * 30 },
];

// WireUserSessionIssuerModal renders the admin workflow for porting an MCP
// toolset off the legacy OAuth Proxy paradigm onto the user-session resource
// chain. The shape of the chain depends on which proxy paradigm the toolset
// is running today (see useOAuthProxyMigration). The driver lives in that
// hook; this file is the presentation surface.
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
      <Dialog.Content className="flex max-h-[85vh] max-w-2xl flex-col gap-0 p-0">
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
      <>
        <Dialog.Header className="shrink-0 border-b px-6 py-4">
          <Dialog.Title>Wire User Session Issuer</Dialog.Title>
        </Dialog.Header>
        <div className="flex-1 overflow-y-auto px-6 py-4">
          <Callout tone="warn">
            This toolset doesn't have an OAuth proxy provider configured, so
            there is nothing to port.
          </Callout>
        </div>
        <Dialog.Footer className="shrink-0 border-t px-6 py-3">
          <Button onClick={onClose}>
            <Button.Text>Close</Button.Text>
          </Button>
        </Dialog.Footer>
      </>
    );
  }

  const {
    paradigm,
    steps,
    currentStep,
    isComplete,
    form,
    setForm,
    runCurrentStep,
    remoteSessionIssuer,
  } = migration;
  const runningStep = steps.find((s) => s.status === "running") ?? null;
  const errorStep = steps.find((s) => s.status === "error") ?? null;

  const advanceDisabled =
    runningStep !== null ||
    (currentStep?.key === "remoteSessionClient" &&
      !canAdvanceClientStep(form, remoteSessionIssuer));

  // Active step index for the indicator. When the whole flow is complete we
  // pin the indicator to the final step so it reads as "all done", not as
  // "off the end".
  const activeIndex = isComplete
    ? steps.length - 1
    : Math.max(
        0,
        steps.findIndex((s) => s.key === currentStep?.key),
      );
  const activeStep = isComplete ? steps[steps.length - 1] : currentStep;

  return (
    <>
      <Dialog.Header className="shrink-0 border-b px-6 py-4">
        <Dialog.Title>Wire User Session Issuer</Dialog.Title>
        <Dialog.Description>
          Port the OAuth Proxy configuration on{" "}
          <span className="font-medium">{toolset.name ?? toolset.slug}</span>{" "}
          onto the user-session resource chain.
        </Dialog.Description>
        <div className="pt-3">
          <StepIndicator steps={steps} activeIndex={activeIndex} />
        </div>
      </Dialog.Header>

      <div className="flex-1 overflow-y-auto px-6 py-5">
        <Stack gap={4}>
          {activeIndex === 0 && <ParadigmSummary paradigm={paradigm} />}
          {activeStep && (
            <StepHeading
              step={activeStep}
              index={activeIndex}
              total={steps.length}
            />
          )}
          {isComplete ? (
            <Callout tone="success">
              Migration complete. Use the new user session issuer to
              authenticate MCP clients going forward.
            </Callout>
          ) : currentStep ? (
            <CurrentStepBody
              step={currentStep}
              form={form}
              setForm={setForm}
              proxyProviderSlug={migration.defaults.proxyProvider.slug}
              issuerOriginGuess={migration.defaults.issuerOriginGuess}
              remoteSessionIssuer={remoteSessionIssuer}
            />
          ) : null}
          {errorStep?.error && (
            <Callout tone="error">{errorStep.error}</Callout>
          )}
        </Stack>
      </div>

      <Dialog.Footer className="shrink-0 border-t px-6 py-3">
        <Button variant="tertiary" onClick={onClose}>
          <Button.Text>{isComplete ? "Done" : "Cancel"}</Button.Text>
        </Button>
        {!isComplete && (
          <Button
            onClick={() => void runCurrentStep()}
            disabled={advanceDisabled}
          >
            <Button.Text>
              {primaryActionLabel(currentStep, runningStep, errorStep, form)}
            </Button.Text>
          </Button>
        )}
      </Dialog.Footer>
    </>
  );
}

function StepHeading({
  step,
  index,
  total,
}: {
  step: MigrationStep;
  index: number;
  total: number;
}) {
  return (
    <Stack gap={1}>
      <Type
        small
        className="text-muted-foreground text-[11px] tracking-wide uppercase"
      >
        Step {index + 1} of {total}
      </Type>
      <Type className="text-lg font-medium">{step.resourceLabel}</Type>
      <Type small className="text-muted-foreground">
        {step.description}
      </Type>
    </Stack>
  );
}

// canAdvanceClientStep gates the primary button on the client step so we can
// keep the per-strategy preconditions in one place: a strategy must be picked,
// clone needs the callback-confirmation checkbox, manual needs at least a
// client_id, register needs the issuer to advertise a registration endpoint.
function canAdvanceClientStep(
  form: MigrationFormState,
  issuer: RemoteSessionIssuer | null,
): boolean {
  switch (form.clientStrategy) {
    case "clone":
      return form.cloneCallbackConfirmed;
    case "register":
      return !!issuer?.registrationEndpoint;
    case "manual":
      return form.manualClientId.trim().length > 0;
    case null:
      return false;
  }
}

function primaryActionLabel(
  current: MigrationStep | null,
  running: MigrationStep | null,
  error: MigrationStep | null,
  form: MigrationFormState,
): string {
  if (running) return "Working…";
  if (error) return "Retry";
  if (current?.key === "remoteSessionClient") {
    switch (form.clientStrategy) {
      case "clone":
        return "Clone client";
      case "register":
        return "Register client";
      case "manual":
        return "Save client";
      case null:
        return "Pick a strategy";
    }
  }
  if (current?.key === "remoteSessionIssuer") return "Create remote issuer";
  if (current?.key === "userSessionIssuer") return "Create user session issuer";
  return "Continue";
}

function ParadigmSummary({ paradigm }: { paradigm: MigrationParadigm }) {
  if (paradigm === "gram") {
    return (
      <Callout tone="info">
        This toolset is on the <strong>Gram-managed</strong> OAuth paradigm.
        Gram is itself the upstream identity provider, so the migration produces
        a single resource: a user session issuer. No remote session issuer or
        client is created — there is no external authorization server to be a
        client of.
      </Callout>
    );
  }
  return (
    <Callout tone="info">
      This toolset is on the <strong>Custom</strong> OAuth Proxy paradigm. The
      migration produces three resources: a user session issuer, a remote
      session issuer (the upstream IdP identity), and a remote session client.
      You pick how the client is provisioned on the third step.
    </Callout>
  );
}

// StepIndicator is the wizard-style header dot row: small circles for each
// step connected by thin lines, with the active step highlighted and earlier
// steps ticked. Read-only — clicking a dot does nothing on purpose, because
// the underlying step state is server-derived and can't be navigated
// backward without rolling back resources.
function StepIndicator({
  steps,
  activeIndex,
}: {
  steps: MigrationStep[];
  activeIndex: number;
}) {
  return (
    <ol className="flex items-center gap-2">
      {steps.map((s, idx) => {
        const isActive = idx === activeIndex;
        const isPast = s.status === "done";
        return (
          <li key={s.key} className="flex items-center gap-2">
            <StepDot
              status={s.status}
              ordinal={idx + 1}
              emphasized={isActive}
            />
            <Type
              small
              className={
                isActive
                  ? "font-medium"
                  : isPast
                    ? "text-muted-foreground"
                    : "text-muted-foreground/70"
              }
            >
              {s.resourceLabel}
            </Type>
            {idx < steps.length - 1 && (
              <span
                className={`mx-1 h-px w-6 ${
                  isPast ? "bg-foreground/40" : "bg-border"
                }`}
              />
            )}
          </li>
        );
      })}
    </ol>
  );
}

function StepDot({
  status,
  ordinal,
  emphasized,
}: {
  status: MigrationStep["status"];
  ordinal: number;
  emphasized: boolean;
}) {
  if (status === "done")
    return <CheckCircle2 className="h-5 w-5 text-green-600" />;
  if (status === "running") return <Loader2 className="h-5 w-5 animate-spin" />;
  if (status === "error")
    return <AlertTriangle className="text-destructive h-5 w-5" />;
  return (
    <span
      className={`flex h-5 w-5 items-center justify-center rounded-full border text-xs ${
        emphasized
          ? "border-foreground text-foreground"
          : "border-muted-foreground text-muted-foreground"
      }`}
    >
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
  remoteSessionIssuer,
}: {
  step: MigrationStep;
  form: MigrationFormState;
  setForm: (patch: Partial<MigrationFormState>) => void;
  proxyProviderSlug: string;
  issuerOriginGuess: string | null;
  remoteSessionIssuer: RemoteSessionIssuer | null;
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
          <FieldLabel label="Refresh Token Duration">
            <Select
              value={String(form.sessionDurationHours)}
              onValueChange={(value) =>
                setForm({ sessionDurationHours: Number(value) })
              }
            >
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {REFRESH_TOKEN_DURATION_OPTIONS.map((opt) => (
                  <SelectItem key={opt.hours} value={String(opt.hours)}>
                    {opt.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
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
        <ClientStepBody
          form={form}
          setForm={setForm}
          proxyProviderSlug={proxyProviderSlug}
          remoteSessionIssuer={remoteSessionIssuer}
        />
      );
  }
}

function ClientStepBody({
  form,
  setForm,
  proxyProviderSlug,
  remoteSessionIssuer,
}: {
  form: MigrationFormState;
  setForm: (patch: Partial<MigrationFormState>) => void;
  proxyProviderSlug: string;
  remoteSessionIssuer: RemoteSessionIssuer | null;
}) {
  if (form.clientStrategy === null) {
    return (
      <ClientStrategyChooser
        onPick={(strategy) =>
          setForm({ clientStrategy: strategy, cloneCallbackConfirmed: false })
        }
        canRegister={!!remoteSessionIssuer?.registrationEndpoint}
      />
    );
  }

  const back = () =>
    setForm({ clientStrategy: null, cloneCallbackConfirmed: false });

  switch (form.clientStrategy) {
    case "clone":
      return (
        <ClonePane
          form={form}
          setForm={setForm}
          proxyProviderSlug={proxyProviderSlug}
          remoteSessionIssuer={remoteSessionIssuer}
          onBack={back}
        />
      );
    case "register":
      return (
        <RegisterPane
          form={form}
          setForm={setForm}
          remoteSessionIssuer={remoteSessionIssuer}
          onBack={back}
        />
      );
    case "manual":
      return <ManualPane form={form} setForm={setForm} onBack={back} />;
  }
}

const STRATEGY_OPTIONS: ReadonlyArray<{
  key: ClientStrategy;
  title: string;
  blurb: string;
}> = [
  {
    key: "clone",
    title: "Clone",
    blurb:
      "Reuse the client_id and client_secret already stored on the OAuth proxy provider. Best when the upstream IdP already has a registered client for this MCP and you want to keep it working without re-registering anything.",
  },
  {
    key: "register",
    title: "Register",
    blurb:
      "Mint a fresh client via RFC 7591 Dynamic Client Registration against the issuer's registration_endpoint. Best when the upstream IdP supports DCR and you want Gram to manage credentials end-to-end.",
  },
  {
    key: "manual",
    title: "Manual",
    blurb:
      "Paste a client_id and client_secret you registered out-of-band with the upstream IdP. Best when DCR is unavailable and you don't have a proxy provider to clone from.",
  },
];

function ClientStrategyChooser({
  onPick,
  canRegister,
}: {
  onPick: (s: ClientStrategy) => void;
  canRegister: boolean;
}) {
  return (
    <Stack gap={2}>
      {STRATEGY_OPTIONS.map((opt) => {
        const disabled = opt.key === "register" && !canRegister;
        return (
          <button
            key={opt.key}
            type="button"
            disabled={disabled}
            onClick={() => onPick(opt.key)}
            className={`border-border w-full rounded-md border p-3 text-left transition-colors ${
              disabled
                ? "cursor-not-allowed opacity-50"
                : "hover:bg-muted/60 hover:border-foreground/30"
            }`}
          >
            <Stack gap={1}>
              <div className="flex items-center justify-between">
                <Type className="font-medium">{opt.title}</Type>
                {disabled && (
                  <Type
                    small
                    className="text-muted-foreground text-[10px] tracking-wide uppercase"
                  >
                    No registration endpoint
                  </Type>
                )}
              </div>
              <Type small className="text-muted-foreground">
                {opt.blurb}
              </Type>
            </Stack>
          </button>
        );
      })}
    </Stack>
  );
}

function ClonePane({
  form,
  setForm,
  proxyProviderSlug,
  remoteSessionIssuer,
  onBack,
}: {
  form: MigrationFormState;
  setForm: (patch: Partial<MigrationFormState>) => void;
  proxyProviderSlug: string;
  remoteSessionIssuer: RemoteSessionIssuer | null;
  onBack: () => void;
}) {
  return (
    <Stack gap={3}>
      <StrategyHeader title="Clone" onBack={onBack} />
      <Callout tone="warn">
        Before cloning, update the upstream IdP's registered redirect URIs to
        include Gram's user-session callback. The existing client_id stays the
        same, so any redirect URIs you have registered already keep working —
        but the user-session flow lands on a different callback than the OAuth
        proxy did, so the new URL has to be added.
      </Callout>
      <Stack gap={2}>
        <FieldReadOnly
          label="Authorization server"
          value={remoteSessionIssuer?.issuer ?? "—"}
          hint="Go here to manage the client's redirect URIs."
        />
        <FieldReadOnly
          label="Callback URL to register"
          value={REMOTE_LOGIN_CALLBACK_URL}
          hint="Add this to the client's redirect URI list on the authorization server."
          mono
        />
      </Stack>
      <Type small className="text-muted-foreground">
        Cloning reads the client_id / client_secret out of the{" "}
        <code className="font-mono">{proxyProviderSlug}</code> proxy provider
        and persists them on a new remote_session_client server-side. The secret
        never leaves the server.
      </Type>
      <label className="flex items-start gap-2 text-sm">
        <input
          type="checkbox"
          className="mt-0.5"
          checked={form.cloneCallbackConfirmed}
          onChange={(e) =>
            setForm({ cloneCallbackConfirmed: e.target.checked })
          }
        />
        <span>
          I've registered{" "}
          <code className="font-mono">{REMOTE_LOGIN_CALLBACK_URL}</code> as a
          redirect URI on the upstream authorization server.
        </span>
      </label>
    </Stack>
  );
}

function RegisterPane({
  form,
  setForm,
  remoteSessionIssuer,
  onBack,
}: {
  form: MigrationFormState;
  setForm: (patch: Partial<MigrationFormState>) => void;
  remoteSessionIssuer: RemoteSessionIssuer | null;
  onBack: () => void;
}) {
  return (
    <Stack gap={3}>
      <StrategyHeader title="Register (DCR)" onBack={onBack} />
      <Type small className="text-muted-foreground">
        Gram sends an RFC 7591 Dynamic Client Registration request to the
        issuer's registration_endpoint. The issuer mints a new client_id and
        client_secret; Gram persists both as a remote_session_client. Use this
        when the upstream IdP supports DCR.
      </Type>
      <FieldReadOnly
        label="Registration endpoint"
        value={remoteSessionIssuer?.registrationEndpoint ?? "—"}
        mono
      />
      <FieldLabel label="Client name (sent on registration)">
        <Input
          value={form.manualClientName}
          onChange={(value) => setForm({ manualClientName: value })}
        />
      </FieldLabel>
    </Stack>
  );
}

function ManualPane({
  form,
  setForm,
  onBack,
}: {
  form: MigrationFormState;
  setForm: (patch: Partial<MigrationFormState>) => void;
  onBack: () => void;
}) {
  return (
    <Stack gap={3}>
      <StrategyHeader title="Manual" onBack={onBack} />
      <Type small className="text-muted-foreground">
        Paste a client_id and client_secret you registered with the upstream
        authorization server. Gram encrypts the secret before persisting it.
      </Type>
      <FieldLabel label="Client ID">
        <Input
          value={form.manualClientId}
          onChange={(value) => setForm({ manualClientId: value })}
          placeholder="abc123…"
        />
      </FieldLabel>
      <FieldLabel label="Client secret (optional)">
        <Input
          type="password"
          value={form.manualClientSecret}
          onChange={(value) => setForm({ manualClientSecret: value })}
          placeholder="Leave blank for public clients"
        />
      </FieldLabel>
      <FieldLabel label="Client name">
        <Input
          value={form.manualClientName}
          onChange={(value) => setForm({ manualClientName: value })}
        />
      </FieldLabel>
    </Stack>
  );
}

function StrategyHeader({
  title,
  onBack,
}: {
  title: string;
  onBack: () => void;
}) {
  return (
    <div className="flex items-center justify-between">
      <Type className="font-medium">{title}</Type>
      <button
        type="button"
        onClick={onBack}
        className="text-muted-foreground hover:text-foreground text-xs underline"
      >
        Pick a different strategy
      </button>
    </div>
  );
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

function FieldReadOnly({
  label,
  value,
  hint,
  mono,
}: {
  label: string;
  value: string;
  hint?: string;
  mono?: boolean;
}) {
  return (
    <Stack gap={1}>
      <Type small className="font-medium">
        {label}
      </Type>
      <div
        className={`bg-muted/40 border-border rounded-md border px-2 py-1.5 text-sm break-all ${
          mono ? "font-mono" : ""
        }`}
      >
        {value}
      </div>
      {hint && (
        <Type small className="text-muted-foreground">
          {hint}
        </Type>
      )}
    </Stack>
  );
}

function Callout({
  tone,
  children,
}: {
  tone: "warn" | "error" | "success" | "info";
  children: React.ReactNode;
}) {
  const toneClasses =
    tone === "error"
      ? "border-destructive/40 bg-destructive/5 text-destructive"
      : tone === "success"
        ? "border-green-500/40 bg-green-50 text-green-900 dark:bg-green-950 dark:text-green-200"
        : tone === "info"
          ? "border-border bg-muted/30 text-foreground"
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
