import { useState } from "react";
import { ExternalLink } from "lucide-react";
import type { IdentityProviderExpectedValue } from "@gram/client/models/components/identityproviderexpectedvalue.js";
import type { IdentityProviderFieldOutcome } from "@gram/client/models/components/identityproviderfieldoutcome.js";
import type { IdentityProviderSetupStep } from "@gram/client/models/components/identityprovidersetupstep.js";
import type { IdentityProviderVerifyResult } from "@gram/client/models/components/identityproviderverifyresult.js";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { CopyButton } from "@/components/ui/CopyButton";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/Field";
import { Input } from "@/components/ui/Input";
import { Text } from "@/components/ui/Text";
import { openSafeExternalUrl } from "@/lib/safe-external-url";
import { IdentityProviderCapabilities } from "./identity-provider-capabilities";

/**
 * One plain sentence per way a check can come back short. The server's own
 * `detail` says which value or scope; this says what it means and what to do,
 * so the administrator is not reading an error code.
 */
const VERIFY_COPY: Record<string, { title: string; body: string }> = {
  unreachable: {
    title: "Speakeasy could not reach your Okta organization",
    body: "Check the organization URL, and that the address answers from outside your own network.",
  },
  refused: {
    title: "Okta refused the request",
    body: "The application may not have its administrator roles assigned yet, or the identifier may belong to a different application.",
  },
  mismatched_value: {
    title: "Okta did not recognize what was configured",
    body: "Check the value you brought back, and the address you pasted for Speakeasy's public keys — a trailing slash is enough to break it.",
  },
  capability_missing: {
    title: "A scope is still missing",
    body: "Grant the missing scope on the application's Okta API Scopes tab in your console, then check again. Everything already granted keeps working.",
  },
};

/** Correcting a stored value is an update; supplying a first one is a save. */
function saveLabel(isSubmitting: boolean, hasSavedValue: boolean): string {
  if (isSubmitting) return "Saving...";
  return hasSavedValue ? "Update" : "Save";
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
}: {
  label: string;
  value: string;
  copyable: boolean;
}): JSX.Element {
  return (
    <div className="space-y-1">
      <Text variant="small" muted>
        {label}
      </Text>
      <div className="border-border bg-background flex items-start gap-2 border p-2">
        <code className="text-foreground min-w-0 flex-1 font-mono text-xs break-all">
          {value}
        </code>
        {copyable ? <CopyButton text={value} size="sm" /> : null}
      </div>
    </div>
  );
}

function VerifyResultBlock({
  result,
}: {
  result: IdentityProviderVerifyResult;
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
            <AlertTitle>Connected to Okta · checked {checkedAt}</AlertTitle>
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

  const copy = VERIFY_COPY[result.outcome];

  return (
    <div className="space-y-3">
      <Alert variant="warning" alignTop>
        <div>
          <AlertTitle>{copy?.title ?? "The check did not pass"}</AlertTitle>
          <AlertDescription>
            {copy?.body ??
              "Speakeasy could not confirm the connection. Check the values above and try again."}
            {result.detail ? (
              <span className="text-muted-foreground mt-1 block">
                Okta said: {result.detail}
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
}: IdentityProviderSetupStepPanelProps): JSX.Element {
  const [values, setValues] = useState<Record<string, string>>({});
  // What is on screen: what has been typed here, else what was submitted
  // before. A secret never arrives back from the server, so its field is
  // always empty and saving it again means retyping it.
  const valueFor = (expected: IdentityProviderExpectedValue) =>
    values[expected.key] ?? expected.currentValue ?? "";
  const complete = step.expectedValues.every((expected) =>
    valueFor(expected).trim(),
  );
  // A check that came back short leaves the step failed, and the server takes
  // another one from there, so the button has to survive its own bad news.
  const canVerify =
    step.state === "awaiting_verification" || step.state === "failed";
  // Something is already stored, so this is a correction rather than a first
  // answer. Secrets are excluded: theirs is always a fresh value.
  const hasSavedValue = step.expectedValues.some(
    (expected) => !expected.secret && expected.currentValue,
  );

  return (
    <div className="border-border bg-card space-y-5 border p-5">
      <div className="space-y-1">
        <h4 className="text-foreground text-sm leading-5 font-semibold">
          {step.title}
        </h4>
        {step.instructions.map((instruction) => (
          <Text key={instruction} variant="small" muted>
            {instruction}
          </Text>
        ))}
      </div>

      {step.deepLink ? (
        <Button
          variant="secondary"
          size="sm"
          className="gap-1.5"
          onClick={() => {
            openSafeExternalUrl(step.deepLink!);
          }}
        >
          Open the right screen in Okta
          <ExternalLink className="h-3.5 w-3.5" />
        </Button>
      ) : null}

      {step.printedValues.length > 0 ? (
        <div className="space-y-3">
          {step.printedValues.map((printed) => (
            <PrintedValue
              key={printed.label}
              label={printed.label}
              value={printed.value}
              copyable={printed.copyable}
            />
          ))}
        </div>
      ) : null}

      {step.expectedValues.length > 0 ? (
        <div className="space-y-4">
          {step.expectedValues.map((expected) => {
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
            <Button
              variant="primary"
              size="sm"
              disabled={!complete || isSubmitting}
              onClick={() =>
                onSubmit(
                  step.expectedValues.map((expected) => ({
                    key: expected.key,
                    value: valueFor(expected).trim(),
                  })),
                )
              }
            >
              {saveLabel(isSubmitting, hasSavedValue)}
            </Button>
            {canVerify ? (
              <Button
                variant="secondary"
                size="sm"
                disabled={isVerifying}
                onClick={onVerify}
              >
                {isVerifying ? "Checking..." : "Verify connection"}
              </Button>
            ) : null}
          </div>
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
        <VerifyResultBlock result={verifyResult} />
      ) : null}
    </div>
  );
}
