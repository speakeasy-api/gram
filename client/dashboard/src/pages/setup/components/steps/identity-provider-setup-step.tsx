import { useState, type ReactNode } from "react";
import { ExternalLink } from "lucide-react";
import type { IdentityProviderClaim } from "@gram/client/models/components/identityproviderclaim.js";
import type { IdentityProviderExpectedValue } from "@gram/client/models/components/identityproviderexpectedvalue.js";
import type { IdentityProviderRepair } from "@gram/client/models/components/identityproviderrepair.js";
import type { IdentityProviderFieldOutcome } from "@gram/client/models/components/identityproviderfieldoutcome.js";
import type { IdentityProviderSetupStep } from "@gram/client/models/components/identityprovidersetupstep.js";
import type { IdentityProviderVerifyResult } from "@gram/client/models/components/identityproviderverifyresult.js";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { CopyButton } from "@/components/ui/CopyButton";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/Field";
import { Input } from "@/components/ui/Input";
import { Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { openSafeExternalUrl } from "@/lib/safe-external-url";
import { IdentityProviderCapabilities } from "@/components/identity-provider-capabilities";

type VerifyCopy = {
  title: string;
  /**
   * What the outcome means and what to do about it. Omitted where the server
   * says that better than we can, in which case its detail is the body.
   */
  body?: string;
  tone: "warning" | "info";
};

/**
 * One plain sentence per way a check can come back other than passing. The
 * server's own `detail` says which value or scope; this says what it means and
 * what to do, so the administrator is not reading an error code. Not every
 * outcome is a problem, so each says how it should read.
 */
const VERIFY_COPY: Record<string, VerifyCopy> = {
  pending_validation: {
    title: "Sign-in is configured and waiting for its first sign-in",
    tone: "info",
  },
  unreachable: {
    title: "Speakeasy could not reach your Okta organization",
    body: "Check the organization URL, and that the address answers from outside your own network.",
    tone: "warning",
  },
  refused: {
    title: "Okta refused the connection",
    body: "Okta answered, but would not accept what Speakeasy sent. The detail below is Okta's own reason.",
    tone: "warning",
  },
  mismatched_value: {
    title: "Okta did not recognize what was configured",
    body: "Check the value you brought back, and the address you pasted for Speakeasy's public keys — a trailing slash is enough to break it.",
    tone: "warning",
  },
  capability_missing: {
    title: "A scope is still missing",
    body: "Grant the missing scope on the application's Okta API Scopes tab in your console, then check again. Everything already granted keeps working.",
    tone: "warning",
  },
};

/** Correcting a stored value is an update; supplying a first one is a save. */
function saveLabel(isSubmitting: boolean, hasSavedValue: boolean): string {
  if (isSubmitting) return "Saving...";
  return hasSavedValue ? "Update" : "Save";
}

function SubStep({
  position,
  title,
  children,
}: {
  position: number;
  title?: string;
  children?: ReactNode;
}): JSX.Element {
  return (
    <li className="flex gap-3">
      <div
        aria-hidden="true"
        className="border-border text-muted-foreground flex h-6 w-6 flex-shrink-0 items-center justify-center border text-xs font-semibold"
      >
        {position}
      </div>
      <div className="min-w-0 flex-1 space-y-3">
        {title ? <Text className="leading-6">{title}</Text> : null}
        {children}
      </div>
    </li>
  );
}

function outcomeFor(
  outcomes: IdentityProviderFieldOutcome[],
  key: string,
): IdentityProviderFieldOutcome | undefined {
  return outcomes.find((outcome) => outcome.key === key);
}

function PrintedValue({
  label,
  value,
  copyable,
  secret,
}: {
  label: string;
  value: string;
  copyable: boolean;
  secret: boolean;
}): JSX.Element {
  const [revealed, setRevealed] = useState(!secret);

  return (
    <div className="space-y-1">
      <Text variant="small" muted>
        {label}
      </Text>
      <div className="border-border bg-background flex items-start gap-2 border p-2">
        <code className="text-foreground min-w-0 flex-1 font-mono text-xs break-all">
          {revealed ? value : "************"}
        </code>
        {secret ? (
          <Button
            variant="tertiary"
            size="sm"
            onClick={() => setRevealed((current) => !current)}
          >
            {revealed ? "Hide" : "Reveal"}
          </Button>
        ) : null}
        {copyable ? <CopyButton text={value} size="sm" /> : null}
      </div>
    </div>
  );
}

function ClaimTable({
  claims,
}: {
  claims: IdentityProviderClaim[];
}): JSX.Element {
  const columns: Column<IdentityProviderClaim>[] = [
    {
      key: "name",
      header: "Claim",
      width: "1.5fr",
      render: (claim) => (
        <code className="text-foreground font-mono text-xs">{claim.name}</code>
      ),
    },
    {
      key: "purpose",
      header: "What it is for",
      width: "2fr",
      render: (claim) => <Text muted>{claim.purpose}</Text>,
    },
    {
      key: "access",
      header: "",
      render: (claim) =>
        claim.carriesAccess ? (
          <Badge variant="warning" background size="sm">
            <Badge.Text>Carries access</Badge.Text>
          </Badge>
        ) : null,
    },
    {
      key: "status",
      header: "",
      render: (claim) =>
        claim.provisioned ? (
          <Badge variant="success" background size="sm">
            <Badge.Text>Provisioned</Badge.Text>
          </Badge>
        ) : null,
    },
  ];

  return (
    <div className="space-y-2">
      <Text variant="small" muted>
        What sign-in will carry about each person. This is what the application
        is configured to send, not what it has been seen sending.
      </Text>
      <Table columns={columns} data={claims} rowKey={(claim) => claim.name} />
    </div>
  );
}

function RepairBlock({
  repair,
  onConfirm,
  onFallback,
  settled,
  isPending,
}: {
  repair: IdentityProviderRepair;
  onConfirm: () => void;
  onFallback: () => void;
  /** Once a choice is made, the record of it replaces the two actions. */
  settled?: string;
  isPending: boolean;
}): JSX.Element {
  return (
    <div className="border-border bg-card space-y-4 border p-4">
      <div className="space-y-1">
        <h5 className="text-foreground text-sm font-medium">{repair.title}</h5>
      </div>
      <ol className="space-y-2">
        {repair.instructions.map((instruction, position) => (
          <li key={instruction} className="flex gap-3">
            <div
              aria-hidden="true"
              className="border-border text-muted-foreground flex h-6 w-6 flex-shrink-0 items-center justify-center border text-xs font-semibold"
            >
              {position + 1}
            </div>
            <Text className="min-w-0 flex-1">{instruction}</Text>
          </li>
        ))}
      </ol>
      {repair.deepLink ? (
        <Button
          variant="secondary"
          size="sm"
          className="gap-1.5"
          onClick={() => {
            openSafeExternalUrl(repair.deepLink!);
          }}
        >
          Open the app&apos;s Sign On tab in Okta
          <ExternalLink className="h-3.5 w-3.5" />
        </Button>
      ) : null}
      {settled ? (
        <Text variant="small" muted>
          {settled}
        </Text>
      ) : (
        <div className="space-y-2">
          <div className="flex flex-wrap items-center gap-3">
            <Button
              variant="primary"
              size="sm"
              disabled={isPending}
              onClick={onConfirm}
            >
              I have set the groups claim
            </Button>
            {repair.fallbackAvailable ? (
              <Button
                variant="tertiary"
                size="sm"
                disabled={isPending}
                onClick={onFallback}
                className="text-muted-foreground hover:text-foreground underline decoration-dotted underline-offset-4 hover:decoration-solid"
              >
                Use directory groups instead
              </Button>
            ) : null}
          </div>
          {repair.fallbackAvailable ? (
            <Text variant="small" muted>
              Access then follows group membership read from the directory
              rather than from the sign-in token.
            </Text>
          ) : null}
        </div>
      )}
    </div>
  );
}

function VerifyResultBlock({
  result,
  pendingValidationTitle,
}: {
  result: IdentityProviderVerifyResult;
  pendingValidationTitle?: string;
}): JSX.Element {
  const checkedAt = result.evidence.checkedAt.toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
  });

  if (result.outcome === "passed") {
    return (
      <div className="space-y-3">
        <Alert variant="success" alignTop>
          <div>
            <AlertTitle>Checked at {checkedAt}</AlertTitle>
            <AlertDescription>
              {result.detail ||
                "Speakeasy acquired a token and made one read for each capability."}
            </AlertDescription>
          </div>
        </Alert>
        <IdentityProviderCapabilities reads={result.evidence.reads} />
      </div>
    );
  }

  let copy = VERIFY_COPY[result.outcome];
  if (result.outcome === "pending_validation" && pendingValidationTitle) {
    copy = { title: pendingValidationTitle, tone: "info" };
  }
  // With nothing of our own to add, the server's sentence is the body rather
  // than a footnote under a sentence that says less.
  const body =
    copy?.body ??
    result.detail ??
    "Speakeasy could not confirm the connection. Check the values above and try again.";
  const quoted = copy?.body ? result.detail : undefined;

  return (
    <div className="space-y-3">
      <Alert variant={copy?.tone ?? "warning"} alignTop>
        <div>
          <AlertTitle>{copy?.title ?? "The check did not pass"}</AlertTitle>
          <AlertDescription>
            {body}
            {quoted ? (
              <span className="text-muted-foreground mt-1 block">
                Okta said: {quoted}
              </span>
            ) : null}
          </AlertDescription>
        </div>
      </Alert>
      {result.evidence.reads.length > 0 ? (
        <IdentityProviderCapabilities reads={result.evidence.reads} />
      ) : null}
    </div>
  );
}

interface IdentityProviderSetupStepPanelProps {
  step: IdentityProviderSetupStep;
  /** Outcomes from the last save, keyed to the expected values above. */
  fieldOutcomes: IdentityProviderFieldOutcome[];
  onSubmit: (values: { key: string; value: string }[]) => void;
  isSubmitting: boolean;
  submitError?: string;
  onVerify: () => void;
  isVerifying: boolean;
  /** The last check, whether from this session or a previous one. */
  verifyResult?: IdentityProviderVerifyResult;
  /** Shown in place of a result when verification cannot run at all. */
  verifyUnavailable?: string;
  /** Overrides the primary action's label, e.g. "Create the sign-in application". */
  submitLabel?: string;
  /** Overrides the check's label, e.g. "Check the connection". */
  verifyLabel?: string;
  /** Overrides the external-link action, e.g. "Open the Provisioning tab in Okta". */
  deepLinkLabel?: string;
  /** Gives pending validation context specific to this setup step. */
  pendingValidationTitle?: string;
  /**
   * A step whose primary action carries no values still needs one — creating
   * the sign-in application is a submit with nothing in it.
   */
  allowSubmitWithoutValues?: boolean;
  /**
   * An action the step keeps offering after its sequence is done, for work the
   * server redoes safely and that the administrator has reason to repeat —
   * assigning groups added in Okta since the last submit. It sits below the
   * numbered work rather than in it: it is not a step on the way anywhere, and
   * numbering it would say the step is unfinished every time it is offered.
   */
  secondaryAction?: {
    label: string;
    /** When to reach for it, in the caller's own words. */
    note?: string;
    onClick: () => void;
    isPending: boolean;
  };
  /**
   * The round trip through the setup portal, for the part of this step our own
   * API cannot do. `note` says what the administrator carries there.
   */
  portal?: { onConnect: () => void; isPending: boolean; note: string };
  /** Wired to `step.repair`; the caller owns what the choices mean. */
  repair?: {
    onConfirm: () => void;
    onFallback: () => void;
    settled?: string;
    isPending: boolean;
    /**
     * Expected values the repair actions write. The step lists them because
     * they are what it accepts, but they are answered by choosing one of the
     * two actions, not by typing — so they get no field of their own.
     */
    valueKeys: string[];
  };
}

/**
 * A setup step rendered from what the server says it is: what to do, what to
 * take into the other console, where to do it, and what to bring back. No Okta
 * strings live here, so the same panel renders the sign-on step next.
 */
export function IdentityProviderSetupStepPanel({
  step,
  fieldOutcomes,
  onSubmit,
  isSubmitting,
  submitError,
  onVerify,
  isVerifying,
  verifyResult,
  verifyUnavailable,
  submitLabel,
  verifyLabel,
  deepLinkLabel,
  pendingValidationTitle,
  allowSubmitWithoutValues = false,
  secondaryAction,
  portal,
  repair,
}: IdentityProviderSetupStepPanelProps): JSX.Element {
  const [values, setValues] = useState<Record<string, string>>({});
  const askedValues = step.expectedValues.filter(
    (expected) => !repair?.valueKeys.includes(expected.key),
  );
  // What is on screen: what has been typed here, else what was submitted
  // before. A secret never arrives back from the server, so its field is
  // always empty and saving it again means retyping it.
  const valueFor = (expected: IdentityProviderExpectedValue) =>
    values[expected.key] ?? expected.currentValue ?? "";
  const complete = askedValues.every((expected) => valueFor(expected).trim());
  // A check that came back short leaves the step failed, and the server takes
  // another one from there, so the button has to survive its own bad news.
  const canVerify =
    step.state === "awaiting_verification" || step.state === "failed";
  const canSubmit = askedValues.length > 0 || allowSubmitWithoutValues;
  // Something is already stored, so this is a correction rather than a first
  // answer. Secrets are excluded: theirs is always a fresh value.
  const hasSavedValue = askedValues.some(
    (expected) => !expected.secret && expected.currentValue,
  );
  const checkLabel = isVerifying
    ? "Checking..."
    : (verifyLabel ?? "Verify connection");

  // Each part of the step is numbered, because the work is a sequence and
  // reads as one. The outer journey is the rail's job, not this panel's.
  const parts: ReactNode[] = [];
  for (const instruction of step.instructions) {
    parts.push(
      <SubStep
        key={`instruction-${instruction}`}
        position={parts.length + 1}
        title={instruction}
      />,
    );
  }
  if (step.printedValues.length > 0 || step.deepLink) {
    parts.push(
      <SubStep
        key="printed"
        position={parts.length + 1}
        title={
          step.printedValues.length > 0
            ? "Copy these into Okta."
            : "Open Okta to make the change."
        }
      >
        {step.printedValues.map((printed) => (
          <PrintedValue
            key={printed.label}
            label={printed.label}
            value={printed.value}
            copyable={printed.copyable}
            secret={printed.secret}
          />
        ))}
        {step.deepLink ? (
          <Button
            variant="secondary"
            size="sm"
            className="gap-1.5"
            onClick={() => {
              openSafeExternalUrl(step.deepLink!);
            }}
          >
            {deepLinkLabel ??
              (portal ? "Open the app in Okta" : "Connect in Okta")}
            <ExternalLink className="h-3.5 w-3.5" />
          </Button>
        ) : null}
      </SubStep>,
    );
  }
  if (portal) {
    parts.push(
      <SubStep key="portal" position={parts.length + 1} title={portal.note}>
        <Button
          variant="secondary"
          size="sm"
          disabled={portal.isPending}
          onClick={portal.onConnect}
        >
          {portal.isPending ? "Opening..." : "Connect"}
        </Button>
      </SubStep>,
    );
  }
  if (canSubmit || canVerify) {
    parts.push(
      <SubStep
        key="bring-back"
        position={parts.length + 1}
        title={
          askedValues.length > 0
            ? "Bring the value back here."
            : "Then run the check."
        }
      >
        {askedValues.map((expected) => {
          const outcome = outcomeFor(fieldOutcomes, expected.key);
          const rejected = outcome?.outcome === "rejected";
          return (
            <Field key={expected.key}>
              <FieldLabel htmlFor={`setup-${expected.key}`}>
                {expected.label}
              </FieldLabel>
              <Input
                id={`setup-${expected.key}`}
                type={expected.secret ? "password" : "text"}
                reveal={expected.secret}
                value={valueFor(expected)}
                onChange={(value) =>
                  setValues((previous) => ({
                    ...previous,
                    [expected.key]: value,
                  }))
                }
                error={rejected}
                disabled={isSubmitting}
              />
              {rejected ? <FieldError>{outcome.detail}</FieldError> : null}
              {outcome?.outcome === "accepted" ? (
                <FieldDescription>{outcome.detail}</FieldDescription>
              ) : null}
            </Field>
          );
        })}
        {submitError ? <FieldError>{submitError}</FieldError> : null}
        <div className="flex flex-wrap items-center gap-3">
          {canSubmit ? (
            <Button
              variant="primary"
              size="sm"
              disabled={!complete || isSubmitting}
              onClick={() =>
                onSubmit(
                  askedValues.map((expected) => ({
                    key: expected.key,
                    value: valueFor(expected).trim(),
                  })),
                )
              }
            >
              {submitLabel ?? saveLabel(isSubmitting, hasSavedValue)}
            </Button>
          ) : null}
          {canVerify ? (
            <Button
              variant="secondary"
              size="sm"
              disabled={isVerifying}
              onClick={onVerify}
            >
              {checkLabel}
            </Button>
          ) : null}
        </div>
      </SubStep>,
    );
  }

  return (
    <div className="space-y-5">
      {step.claims && step.claims.length > 0 ? (
        <ClaimTable claims={step.claims} />
      ) : null}

      <ol className="space-y-5">{parts}</ol>

      {secondaryAction ? (
        <div className="flex flex-wrap items-center gap-3">
          <Button
            variant="secondary"
            size="sm"
            disabled={secondaryAction.isPending}
            onClick={secondaryAction.onClick}
          >
            {secondaryAction.label}
          </Button>
          {secondaryAction.note ? (
            <Text variant="small" muted>
              {secondaryAction.note}
            </Text>
          ) : null}
        </div>
      ) : null}

      {verifyUnavailable ? (
        <Alert variant="info" alignTop>
          <div>
            <AlertTitle>Verification is not available yet</AlertTitle>
            <AlertDescription>{verifyUnavailable}</AlertDescription>
          </div>
        </Alert>
      ) : null}

      {!verifyUnavailable && verifyResult ? (
        <VerifyResultBlock
          result={verifyResult}
          pendingValidationTitle={pendingValidationTitle}
        />
      ) : null}

      {step.repair && repair ? (
        <RepairBlock
          repair={step.repair}
          onConfirm={repair.onConfirm}
          onFallback={repair.onFallback}
          settled={repair.settled}
          isPending={repair.isPending}
        />
      ) : null}
    </div>
  );
}
