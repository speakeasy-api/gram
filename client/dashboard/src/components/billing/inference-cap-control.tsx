import { InferenceCapMeter } from "@/components/billing/inference-cap-meter";
import {
  inferenceCapFieldId,
  inferenceCapLabel,
} from "@/components/billing/inference-caps";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import type { InferenceSpendCap } from "@gram/client/models/components/inferencespendcap.js";
import { invalidateAllGetInferenceSpendCaps } from "@gram/client/react-query/getInferenceSpendCaps.js";
import { useSetSpendCapMutation } from "@gram/client/react-query/setSpendCap.js";
import { useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { Link } from "react-router";

/** The bounds the API accepts for a monthly inference cap, in whole USD. */
const MIN_CAP_USD = 1;
const MAX_CAP_USD = 10_000;

// The in-app booking gate, which prefills the form from the session — the same
// path the trial card sends people to. Not the marketing site's /talk-to-us.
const SALES_PATH = "/talk-to-us";

function formatUsd(amount: number): string {
  return `$${amount.toLocaleString("en-US")}`;
}

const MIN_LABEL = formatUsd(MIN_CAP_USD);
const MAX_LABEL = formatUsd(MAX_CAP_USD);
const RANGE_MESSAGE = `Enter a whole dollar amount between ${MIN_LABEL} and ${MAX_LABEL}.`;

const MEMBER_NOTE = "Only organization admins can change this cap.";

// A key can be materialized with no cap on it, and 0 is how the API says so
// rather than an amount anyone set. Shown as an amount it would be the one
// number the endpoint refuses, so the field is left empty instead.
const NO_CAP_PLACEHOLDER = "No cap set";

// Once a cap is set, an empty editable field is an amount on its way to being
// replaced rather than a key with no cap, so the hint there is what to type.
const AMOUNT_PLACEHOLDER = `${MIN_LABEL}–${MAX_LABEL}`;

/** The field text for a cap amount. An uncapped key has an empty field. */
function capAmountText(monthlyCredits: number): string {
  return monthlyCredits > 0 ? String(monthlyCredits) : "";
}

// A disabled key is refused at the endpoint, so the field would only invite a
// request that is going to come back a conflict.
const DISABLED_NOTE =
  "This inference is turned off for this organization, so its cap can't be changed.";

/**
 * One organization's cap on one Gram-managed inference key: what it has spent
 * this month, what the ceiling is, and — for an admin who can move it — the
 * field that does.
 *
 * Every control owns its own draft, its own save and its own feedback. The caps
 * are independent limits on unrelated work, so an admin editing one must not
 * have the other's save reported back at them, or their amount replaced by it.
 */
export function InferenceCapControl({
  cap,
  locked,
}: {
  cap: InferenceSpendCap;
  /**
   * Whether the cap is enforced but can't be changed from here, which during a
   * trial it can't: the keys are live on the trial's own defaults, and what the
   * trial withholds is the ability to move them.
   *
   * The amount and its meter still show; the field and the save are inert, and
   * the mutation is never mounted.
   */
  locked: boolean;
}): JSX.Element {
  if (cap.disabled) return <CapReadOnly cap={cap} note={DISABLED_NOTE} />;

  // Nothing writable here, so the mutation is never mounted — the lock holds
  // whatever an admin's scopes are.
  if (locked) return <LockedCapField cap={cap} />;

  return (
    <RequireScope
      scope="org:admin"
      level="section"
      fallback={<CapReadOnly cap={cap} note={MEMBER_NOTE} />}
    >
      <CapForm cap={cap} />
    </RequireScope>
  );
}

/**
 * The cap as it is being enforced, in the field that would change it.
 *
 * The amount is the one upstream is holding — a trial runs on defaults — so it
 * is shown in the control rather than replaced by a note about it.
 */
function LockedCapField({ cap }: { cap: InferenceSpendCap }): JSX.Element {
  const label = inferenceCapLabel(cap.keyType);
  const fieldId = inferenceCapFieldId(cap.keyType);

  return (
    <Stack gap={4} className="max-w-md">
      <Stack gap={2}>
        <Label htmlFor={fieldId}>{label}</Label>
        <InferenceCapMeter cap={cap} />
        <Input
          id={fieldId}
          type="number"
          value={capAmountText(cap.monthlyCredits)}
          placeholder={NO_CAP_PLACEHOLDER}
          disabled
          readOnly
        />
      </Stack>
      <Button type="button" disabled>
        {`SAVE ${label.toUpperCase()}`}
      </Button>
    </Stack>
  );
}

// A member sees the cap they are spending under but gets no control — the
// endpoint is admin-only, so a disabled field would only invite a request that
// is going to be refused. The same shape reports a key this organization can't
// change at all.
function CapReadOnly({
  cap,
  note,
}: {
  cap: InferenceSpendCap;
  note: string;
}): JSX.Element {
  return (
    <Stack gap={2} className="max-w-md">
      <Text className="text-eyebrow">{inferenceCapLabel(cap.keyType)}</Text>
      <InferenceCapMeter cap={cap} />
      <Text muted small>
        {note}
      </Text>
    </Stack>
  );
}

/** Whether `value` is a cap the API will accept, and why it isn't. */
function capError(value: string): string | null {
  const trimmed = value.trim();
  const amount = Number(trimmed);
  const valid =
    trimmed !== "" &&
    Number.isInteger(amount) &&
    amount >= MIN_CAP_USD &&
    amount <= MAX_CAP_USD;

  return valid ? null : RANGE_MESSAGE;
}

// The field seeds from the loaded cap and re-seeds only while it is pristine: a
// cap changed elsewhere (another admin, the save's own invalidation) has to
// reach an untouched field, but a background refetch landing mid-edit must not
// overwrite what this admin typed.
function CapForm({ cap }: { cap: InferenceSpendCap }): JSX.Element {
  const queryClient = useQueryClient();
  const label = inferenceCapLabel(cap.keyType);
  const fieldId = inferenceCapFieldId(cap.keyType);
  const capAmount = capAmountText(cap.monthlyCredits);
  const [amount, setAmount] = useState(capAmount);
  // The text the field was last seeded from. Comparing against it — rather
  // than against a dirty flag — means an admin who edits back to the seeded
  // amount is pristine again, and it survives a save because the invalidated
  // query comes back with the amount now in the field.
  const [seeded, setSeeded] = useState(capAmount);
  // Whether this control has been asked to save. An amount is only refused out
  // loud once there is somebody to refuse.
  const [submitted, setSubmitted] = useState(false);

  // Adjusting state during render rather than in an effect: React re-runs this
  // component before committing, so the field never paints the stale cap.
  if (seeded !== capAmount) {
    setSeeded(capAmount);
    if (amount === seeded) setAmount(capAmount);
  }

  const mutation = useSetSpendCapMutation({
    onSuccess: () => {
      // Every meter and field on the page reads this list, and the whole key has
      // to be refreshed rather than the exact one this control subscribes to.
      void invalidateAllGetInferenceSpendCaps(queryClient);
    },
    onError: () => {
      // A lifecycle transition can reject the write after this control loaded.
      // Refresh both controls so a newly locked or disabled key is not left
      // looking editable after that authoritative rejection.
      void invalidateAllGetInferenceSpendCaps(queryClient);
    },
  });

  const validationError = capError(amount);
  // An amount is only wrong once somebody has put it there. A key with no cap
  // opens on an empty field, and a field emptied on the way to a new amount is
  // mid-edit — neither is a mistake, and announcing one greets an admin with a
  // correction for something they haven't done. So the message waits for an
  // amount to actually be in the field, or for a save that has to be refused.
  const showError =
    validationError !== null && (submitted || amount.trim() !== "");

  const handleChange = (value: string) => {
    setAmount(value);
    // The refusal belonged to the amount that was saved, not to whatever is
    // being typed in its place. Left standing, it makes a field cleared on the
    // way to a new amount read as a mistake — the one state an untouched field
    // is deliberately quiet about. The next save sets this again, so an empty
    // field submitted unchanged is still refused out loud.
    setSubmitted(false);
    // "Saved."/failure text left beside a field that has since been edited
    // reads as feedback about the value now in the field.
    if (mutation.isSuccess || mutation.isError) mutation.reset();
  };

  // An out-of-range cap is rejected here, where the amount can be corrected,
  // instead of coming back as a transient-looking API failure the admin is
  // invited to retry.
  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setSubmitted(true);
    if (validationError !== null) return;

    // The key type is what makes this control's save its own: the endpoint
    // defaults to the invoiced key when it is left off, so every save names the
    // key it belongs to rather than relying on that default.
    mutation.mutate({
      request: {
        setSpendCapRequestBody: {
          keyType: cap.keyType,
          monthlyCredits: Number(amount.trim()),
        },
      },
    });
  };

  return (
    <form onSubmit={handleSubmit}>
      <Stack gap={4} className="max-w-md">
        <Stack gap={2}>
          <Label htmlFor={fieldId}>{label}</Label>
          <InferenceCapMeter cap={cap} />
          <Input
            id={fieldId}
            type="number"
            inputMode="numeric"
            min={MIN_CAP_USD}
            max={MAX_CAP_USD}
            step={1}
            value={amount}
            placeholder={
              capAmount === "" ? NO_CAP_PLACEHOLDER : AMOUNT_PLACEHOLDER
            }
            onChange={handleChange}
            error={showError}
          />
          {!showError ? (
            <Text muted small>
              {cap.monthlyCredits > 0
                ? "This inference stops once the month's spend reaches the cap. Raise or lower it at any time."
                : "No monthly cap is currently set. Enter an amount to limit this inference."}
            </Text>
          ) : (
            <Text small destructive role="alert">
              {validationError}
            </Text>
          )}
          {/* The maximum is the ceiling the endpoint enforces, so anything
              above it is a conversation rather than a form. This sits outside
              the validation branch on purpose: it is the way forward for the
              admin who just had a larger amount rejected. */}
          <Text muted small>
            Need a cap above {MAX_LABEL}?{" "}
            <Link to={SALES_PATH} className="underline underline-offset-2">
              Talk to us
            </Link>
            .
          </Text>
        </Stack>
        <Stack direction="horizontal" align="center" gap={3}>
          <Button type="submit" disabled={mutation.isPending}>
            {mutation.isPending ? "SAVING..." : `SAVE ${label.toUpperCase()}`}
          </Button>
          {/* Every message names its own cap: both controls can be speaking at
              once, and an unattributed "Saved." beside two fields belongs to
              neither. */}
          {mutation.isSuccess && (
            <Text muted small role="status">
              Saved the {label.toLowerCase()}.
            </Text>
          )}
          {mutation.isError && (
            <Text small destructive role="alert">
              Couldn't save the {label.toLowerCase()}. Try again.
            </Text>
          )}
        </Stack>
      </Stack>
    </form>
  );
}
