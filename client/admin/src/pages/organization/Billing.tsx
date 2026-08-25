import { useRef, type JSX, type ReactNode } from "react";
import { useForm } from "@tanstack/react-form";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";

import { useConfirmDialog } from "@/components/ConfirmDialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  inferenceKeysQuery,
  inferenceSpendHistoryQuery,
  invalidateOrganizationBilling,
  organizationQuery,
  paygBillingSummaryQuery,
  stripeSubscriptionQuery,
} from "@/lib/adminQueries";
import {
  cancelStripeSubscription,
  errorMessage,
  GramAdminError,
  resumeStripeSubscription,
  setInferenceKeyMonthlyLimit,
  type AdminInferenceKey,
  type AdminInferenceKeyType,
  type AdminInferenceSpendMonth,
  type AdminOrganization,
  type AdminPaygBillingSummary,
  type AdminStripeSubscription,
} from "@/lib/gramAdminApi";
import { useWriteReport } from "@/pages/organizations/writeReport";

import {
  billingState,
  formatBillingDate,
  formatExactUsd,
  formatRecordedThrough,
  formatTokenCount,
} from "./billingState";

function Group({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <section className="mt-5 first:mt-0">
      <h5 className="text-muted-foreground mb-1 text-xs font-medium">
        {title}
      </h5>
      {children}
    </section>
  );
}

function Row({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <div className="grid grid-cols-[12rem_1fr] items-baseline gap-3 py-1">
      <span className="text-muted-foreground text-sm">{label}</span>
      <div className="text-sm">{children}</div>
    </div>
  );
}

function cycleLabel(start: string, end: string): string {
  const formattedStart = formatBillingDate(start);
  const formattedEnd = formatBillingDate(end);
  if (formattedStart === null || formattedEnd === null)
    return "Dates unavailable";
  return `${formattedStart} to ${formattedEnd}`;
}

function inferenceKeyPurpose(keyType: AdminInferenceKey["key_type"]): string {
  switch (keyType) {
    case "chat":
      return "Other inference";
    case "internal":
      return "Security and internal inference";
    default:
      return "Platform-managed inference";
  }
}

function formatCredits(value: number): string {
  return new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
    minimumFractionDigits: 2,
    maximumFractionDigits: 6,
  }).format(value);
}

const MIN_MONTHLY_LIMIT = 1;
const MAX_MONTHLY_LIMIT = 10_000;

function isWritableInferenceKey(
  key: AdminInferenceKey,
): key is AdminInferenceKey & { key_type: AdminInferenceKeyType } {
  return key.key_type === "chat" || key.key_type === "internal";
}

function InferenceKeyLimitEditor({
  organizationID,
  inferenceKey,
}: {
  organizationID: string;
  inferenceKey: AdminInferenceKey & { key_type: AdminInferenceKeyType };
}): JSX.Element {
  const qc = useQueryClient();
  const { announce, showFailure } = useWriteReport();
  const mutation = useMutation({
    mutationFn: (monthlyCredits: number) =>
      setInferenceKeyMonthlyLimit({
        organizationID,
        keyType: inferenceKey.key_type,
        monthlyCredits,
      }),
    onSuccess: async () => {
      showFailure(null);
      announce(`${inferenceKey.key_type} monthly limit updated.`);
      await qc.invalidateQueries({
        queryKey: inferenceKeysQuery(organizationID).queryKey,
      });
    },
    onError: (error) => {
      const message = `Could not update ${inferenceKey.key_type} monthly limit: ${errorMessage(error)}`;
      announce(message);
      showFailure(message);
    },
  });

  const inputID = `inference-key-limit-${inferenceKey.key_type}`;
  const errorID = `${inputID}-error`;

  const form = useForm({
    defaultValues: { monthlyCredits: String(inferenceKey.monthly_credits) },
    onSubmit: async ({ value }) => {
      if (inferenceKey.disabled) return;
      showFailure(null);
      try {
        await mutation.mutateAsync(Number(value.monthlyCredits));
      } catch {
        // The mutation reports the failure through the page's shared live region.
      }
    },
  });

  return (
    <Row label="Monthly limit">
      <form
        className="flex max-w-sm items-start gap-2"
        onSubmit={(event) => {
          event.preventDefault();
          void form.handleSubmit();
        }}
      >
        <form.Field
          name="monthlyCredits"
          validators={{
            onChange: ({ value }) => {
              const parsed = Number(value);
              return value.trim() === "" ||
                !Number.isInteger(parsed) ||
                parsed < MIN_MONTHLY_LIMIT ||
                parsed > MAX_MONTHLY_LIMIT
                ? "Enter a whole-dollar limit from $1 to $10,000."
                : undefined;
            },
          }}
        >
          {(field) => {
            const invalid = !field.state.meta.isValid;
            const showValidation =
              !inferenceKey.disabled && field.state.meta.isDirty && invalid;
            return (
              <div className="flex-1">
                <label className="sr-only" htmlFor={inputID}>
                  {inferenceKey.key_type} monthly limit in USD
                </label>
                <Input
                  id={inputID}
                  type="number"
                  min={MIN_MONTHLY_LIMIT}
                  max={MAX_MONTHLY_LIMIT}
                  step={1}
                  name={field.name}
                  value={field.state.value}
                  disabled={inferenceKey.disabled || mutation.isPending}
                  aria-invalid={showValidation}
                  aria-describedby={showValidation ? errorID : undefined}
                  onBlur={field.handleBlur}
                  onChange={(event) => {
                    mutation.reset();
                    field.handleChange(event.target.value);
                  }}
                />
                {showValidation && (
                  <p id={errorID} className="text-destructive mt-1 text-xs">
                    {field.state.meta.errors[0]}
                  </p>
                )}
                {mutation.isError && (
                  <p role="alert" className="text-destructive mt-1 text-xs">
                    {errorMessage(mutation.error)}
                  </p>
                )}
                {inferenceKey.disabled && (
                  <p className="text-muted-foreground mt-1 text-xs">
                    Enable this key before changing its limit.
                  </p>
                )}
              </div>
            );
          }}
        </form.Field>
        <form.Subscribe
          selector={(state) =>
            [
              state.values.monthlyCredits,
              state.canSubmit,
              state.isSubmitting,
            ] as const
          }
        >
          {([value, canSubmit, isSubmitting]) => (
            <Button
              type="submit"
              size="sm"
              disabled={
                inferenceKey.disabled ||
                !canSubmit ||
                mutation.isPending ||
                isSubmitting ||
                Number(value) === inferenceKey.monthly_credits
              }
            >
              {mutation.isPending || isSubmitting ? "Saving…" : "Save limit"}
            </Button>
          )}
        </form.Subscribe>
      </form>
    </Row>
  );
}

function InferenceKeys({
  organizationID,
  keys,
}: {
  organizationID: string;
  keys: AdminInferenceKey[];
}): JSX.Element {
  return (
    <Group title="Platform-managed OpenRouter keys">
      {keys.length === 0 && (
        <p className="text-muted-foreground text-sm">
          No platform-managed keys have been materialized.
        </p>
      )}
      {keys.map((key) => (
        <div
          key={key.key_type}
          className="border-border mt-3 border-t pt-2 first:mt-0 first:border-0 first:pt-0"
        >
          <Row label="Key type">{key.key_type}</Row>
          <Row label="Purpose">{inferenceKeyPurpose(key.key_type)}</Row>
          <Row label="Current monthly usage">
            {formatCredits(key.credits_used)}
            {key.monthly_credits === 0
              ? " (unlimited limit)"
              : ` of ${formatCredits(key.monthly_credits)}`}
          </Row>
          <Row label="Configured monthly credit limit">
            {key.monthly_credits === 0
              ? "Unlimited"
              : formatCredits(key.monthly_credits)}
          </Row>
          <Row label="State">{key.disabled ? "Disabled" : "Enabled"}</Row>
          {isWritableInferenceKey(key) && (
            <InferenceKeyLimitEditor
              key={`${key.key_type}:${key.monthly_credits}`}
              organizationID={organizationID}
              inferenceKey={key}
            />
          )}
        </div>
      ))}
    </Group>
  );
}

function hasSufficientHistory(months: AdminInferenceSpendMonth[]): boolean {
  const previous = months.at(-2);
  const latest = months.at(-1);
  return previous !== undefined && previous.period_end === latest?.period_start;
}

function InferenceSpendHistory({
  months,
}: {
  months: AdminInferenceSpendMonth[];
}): JSX.Element {
  const showGraph = hasSufficientHistory(months);
  const amounts = months.map((month) => Number.parseFloat(month.spend_usd));
  const maximum = Math.max(0, ...amounts);

  return (
    <Group title="Monthly inference spend">
      {months.length === 0 ? (
        <p className="text-muted-foreground text-sm">
          No complete monthly inference spend has been recorded yet.
        </p>
      ) : showGraph ? (
        <figure
          aria-label="Monthly inference spend graph"
          className="space-y-2"
        >
          <figcaption className="sr-only">
            Monthly inference spend by completed UTC calendar month
          </figcaption>
          {months.map((month, index) => {
            const amount = amounts[index] ?? 0;
            const width = maximum === 0 ? 0 : (amount / maximum) * 100;
            return (
              <div
                key={month.period_start}
                className="grid grid-cols-[5rem_minmax(8rem,1fr)_7rem] items-center gap-3"
              >
                <span className="text-muted-foreground text-xs">
                  {formatBillingDate(month.period_start) ?? month.period_start}
                </span>
                <div
                  aria-hidden="true"
                  className="bg-muted h-3 overflow-hidden rounded-sm"
                >
                  <div
                    className="bg-primary h-full rounded-sm"
                    style={{ width: `${width}%` }}
                  />
                </div>
                <span className="text-right text-sm tabular-nums">
                  {formatExactUsd(month.spend_usd) ?? "—"}
                </span>
              </div>
            );
          })}
        </figure>
      ) : (
        months.map((month) => (
          <Row
            key={month.period_start}
            label={formatBillingDate(month.period_start) ?? month.period_start}
          >
            {formatExactUsd(month.spend_usd) ?? "—"}
          </Row>
        ))
      )}
      {months.length > 0 ? (
        <p className="text-muted-foreground mt-2 text-xs">
          Complete UTC calendar months only.
        </p>
      ) : null}
    </Group>
  );
}

function BillingSummary({
  summary,
}: {
  summary: AdminPaygBillingSummary;
}): JSX.Element {
  const recordedThrough = formatRecordedThrough(summary.recorded_through);
  return (
    <Group title="Current billing cycle">
      <Row label="Period">
        {cycleLabel(summary.period_start, summary.period_end)}
      </Row>
      <Row label="Tokens under management">
        {formatTokenCount(summary.tum_tokens) ?? "—"}
      </Row>
      <Row label="TUM unit price">
        {formatExactUsd(summary.tum_unit_price_usd) ?? "—"} per token
      </Row>
      <Row label="TUM cost">{formatExactUsd(summary.tum_cost_usd) ?? "—"}</Row>
      <Row label="Inference spend">
        {formatExactUsd(summary.other_inference_spend_usd) ?? "—"}
      </Row>
      {recordedThrough && (
        <Row label="Spend recorded through">{recordedThrough}</Row>
      )}
      <Row label="Estimated total">
        <strong>{formatExactUsd(summary.estimated_total_usd) ?? "—"}</strong>
      </Row>
      <p className="text-muted-foreground mt-2 text-xs">
        This is an estimate, not a bill. Inference spend includes completed UTC
        days only, and the invoice can finalize up to 72 hours after the cycle
        ends.
      </p>
    </Group>
  );
}

function paymentLabel(subscription: AdminStripeSubscription): string {
  if (subscription.payment_failed) return "Payment failed";
  if (subscription.status === "past_due") return "Past due";
  if (
    [
      "canceled",
      "unpaid",
      "incomplete",
      "incomplete_expired",
      "paused",
    ].includes(subscription.status)
  ) {
    return "Not collecting";
  }
  return "No payment failure reported";
}

function SubscriptionDetails({
  subscription,
}: {
  subscription: AdminStripeSubscription;
}): JSX.Element {
  const state = billingState(subscription);
  return (
    <Group title="Subscription and payment">
      {state.paymentFailed && (
        <p role="alert" className="text-destructive mb-2 text-sm font-medium">
          The latest invoice has an unpaid balance.
        </p>
      )}
      <Row label="Subscription status">
        {subscription.status.replaceAll("_", " ")}
      </Row>
      <Row label="Payment state">{paymentLabel(subscription)}</Row>
      <Row label="Current period">
        {cycleLabel(
          subscription.current_period_start,
          subscription.current_period_end,
        )}
      </Row>
      {subscription.trial_end && (
        <Row label="Trial ends">
          {formatBillingDate(subscription.trial_end) ?? "—"}
        </Row>
      )}
      {state.kind === "ending" && (
        <Row label="Scheduled cancellation">
          {formatBillingDate(state.date) ?? "At the end of the current period"}
        </Row>
      )}
      {subscription.canceled_at && (
        <Row label="Cancellation requested">
          {formatBillingDate(subscription.canceled_at) ?? "—"}
        </Row>
      )}
    </Group>
  );
}

export function BillingRoute(): JSX.Element | null {
  const { idOrSlug } = useParams({ from: "/organizations/$idOrSlug" });
  const { data } = useQuery(organizationQuery(idOrSlug));
  if (!data) return null;
  return <Billing key={data.id} org={data} />;
}

export function Billing({ org }: { org: AdminOrganization }): JSX.Element {
  const qc = useQueryClient();
  const [confirm, confirmDialog] = useConfirmDialog();
  const { announce, showFailure } = useWriteReport();
  const control = useRef<HTMLButtonElement>(null);

  const inferenceKeysResult = useQuery(inferenceKeysQuery(org.id));
  const inferenceSpendHistoryResult = useQuery(
    inferenceSpendHistoryQuery(org.id),
  );
  const subscriptionQuery = useQuery(stripeSubscriptionQuery(org.id));
  const subscription = subscriptionQuery.data;
  const state = subscription ? billingState(subscription) : null;
  const showCurrentBillingCycle =
    subscription?.status === "active" || subscription?.status === "past_due";
  const summaryQuery = useQuery({
    ...paygBillingSummaryQuery(org.id),
    enabled: showCurrentBillingCycle,
  });

  const mutation = useMutation({
    mutationFn: (cancel: boolean) =>
      cancel
        ? cancelStripeSubscription(org.id)
        : resumeStripeSubscription(org.id),
    onSuccess: (updated) => {
      qc.setQueryData(stripeSubscriptionQuery(org.id).queryKey, updated);
      void invalidateOrganizationBilling(qc, org.id);
    },
  });

  const changeCancellation = async (cancel: boolean): Promise<void> => {
    const confirmed = await confirm({
      title: `${cancel ? "Cancel" : "Resume"} pay as you go for ${org.name}?`,
      description: cancel
        ? "Billing and service continue until the current period ends."
        : "The subscription will continue past the current period.",
      confirmLabel: cancel ? "Cancel pay as you go" : "Resume pay as you go",
      destructive: cancel,
    });
    if (!confirmed) {
      control.current?.focus();
      return;
    }

    showFailure(null);
    mutation.mutate(cancel, {
      onSuccess: () =>
        announce(
          `${org.name} pay as you go ${cancel ? "will end after this period" : "will continue"}.`,
        ),
      onError: (error) => {
        const text = `Could not update billing for ${org.name}: ${errorMessage(error)}`;
        announce(text);
        showFailure(text);
      },
      onSettled: () => {
        setTimeout(function restoreControlFocus() {
          const target = control.current;
          if (!target?.isConnected) return;
          if (target.disabled) {
            setTimeout(restoreControlFocus);
            return;
          }
          target.focus();
        });
      },
    });
  };

  const missingSubscription =
    subscriptionQuery.error instanceof GramAdminError &&
    subscriptionQuery.error.status === 404;

  return (
    <div className="border-border bg-muted/10 rounded-md border p-4">
      {subscriptionQuery.isPending && (
        <p className="text-muted-foreground text-sm">Loading billing…</p>
      )}
      {missingSubscription && (
        <p className="text-muted-foreground text-sm">
          This organization has no Stripe subscription.
        </p>
      )}
      {subscriptionQuery.isError && !missingSubscription && (
        <p role="alert" className="text-destructive text-sm">
          Could not load subscription: {errorMessage(subscriptionQuery.error)}
        </p>
      )}
      {subscription && <SubscriptionDetails subscription={subscription} />}

      {inferenceKeysResult.data && (
        <InferenceKeys
          organizationID={org.id}
          keys={inferenceKeysResult.data}
        />
      )}
      {inferenceKeysResult.isPending && (
        <p className="text-muted-foreground mt-5 text-sm">
          Loading OpenRouter keys…
        </p>
      )}
      {inferenceKeysResult.isError && (
        <p role="alert" className="text-destructive mt-5 text-sm">
          Could not load OpenRouter keys:{" "}
          {errorMessage(inferenceKeysResult.error)}
        </p>
      )}

      {showCurrentBillingCycle && summaryQuery.data && (
        <BillingSummary summary={summaryQuery.data} />
      )}
      {showCurrentBillingCycle &&
        summaryQuery.isPending &&
        summaryQuery.fetchStatus !== "idle" && (
          <p className="text-muted-foreground mt-5 text-sm">
            Loading current billing cycle…
          </p>
        )}
      {showCurrentBillingCycle && summaryQuery.isError && (
        <p role="alert" className="text-destructive mt-5 text-sm">
          Could not load current billing cycle:{" "}
          {errorMessage(summaryQuery.error)}
        </p>
      )}

      {inferenceSpendHistoryResult.data ? (
        <InferenceSpendHistory months={inferenceSpendHistoryResult.data} />
      ) : null}
      {inferenceSpendHistoryResult.isPending ? (
        <p className="text-muted-foreground mt-5 text-sm">
          Loading inference spend history…
        </p>
      ) : null}
      {inferenceSpendHistoryResult.isError ? (
        <p role="alert" className="text-destructive mt-5 text-sm">
          Could not load inference spend history:{" "}
          {errorMessage(inferenceSpendHistoryResult.error)}
        </p>
      ) : null}

      {state &&
        (state.kind === "active" ||
          state.kind === "trialing" ||
          state.kind === "ending") && (
          <Group title="Controls">
            <Button
              ref={control}
              size="sm"
              variant={state.kind === "ending" ? "default" : "destructive"}
              disabled={mutation.isPending}
              onClick={() => void changeCancellation(state.kind !== "ending")}
            >
              {state.kind === "ending"
                ? "Resume pay as you go"
                : "Cancel pay as you go"}
            </Button>
          </Group>
        )}
      {confirmDialog}
    </div>
  );
}
