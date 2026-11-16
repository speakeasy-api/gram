import type { AdminStripeSubscriptionCandidate } from "@gram/admin-client/models/components/adminstripesubscriptioncandidate";
import type { SetStripeSubscriptionRequestBody } from "@gram/admin-client/models/components/setstripesubscriptionrequestbody";
import {
  useCallback,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type JSX,
  type RefObject,
} from "react";
import { useForm, useStore } from "@tanstack/react-form";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  CircleCheckBig,
  ShieldQuestionMark,
  CircleX,
  LoaderCircle,
} from "lucide-react";

import { CopyValue } from "@/components/CopyValue";
import { ExternalLinkButton } from "@/components/ExternalLinkButton";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { useOnUnmount } from "@/hooks/useOnUnmount";
import {
  cancelOrganizationFetches,
  invalidateOrganizationBilling,
  invalidateOrganizationStats,
  invalidateOrganizations,
  writeOrganizationToCache,
} from "@/lib/adminQueries";
import { errorMessage, type AdminOrganization } from "@/lib/gramAdminApi";
import {
  getStripeSubscriptionCandidate,
  setStripeSubscription,
} from "@/lib/gramAdminClient";
import { useWriteReport } from "@/pages/organizations/writeReport";

function httpStatus(error: unknown): number | undefined {
  if (!error || typeof error !== "object") return undefined;
  if ("statusCode" in error && typeof error.statusCode === "number") {
    return error.statusCode;
  }
  if ("status" in error && typeof error.status === "number") {
    return error.status;
  }
  return undefined;
}

const STRIPE_SUBSCRIPTION_ID = /^sub_[A-Za-z0-9_]+$/;
const MAX_STRIPE_SUBSCRIPTION_ID_LENGTH = 255;

const MISSING_CUSTOMER_NOTE =
  "Set a Stripe customer ID before recording a subscription. Speakeasy checks that the subscription belongs to that customer.";

function subscriptionStatusLabel(status: string): string {
  switch (status) {
    case "active":
      return "Active";
    case "trialing":
      return "Trialing";
    case "past_due":
      return "Past due";
    default:
      return status;
  }
}

function hasValue(value: string | undefined): value is string {
  return value !== undefined && value !== "";
}

export function SetStripeSubscription({
  org,
  focusFallbackRef,
}: {
  org: AdminOrganization;
  focusFallbackRef?: RefObject<HTMLElement | null>;
}): JSX.Element {
  const activatesPayg = org.account_type !== "payg";
  const qc = useQueryClient();
  const { announce, showFailure } = useWriteReport();
  const inputID = useId();
  const messageID = useId();
  const [open, setOpen] = useState(false);
  const context = JSON.stringify([
    org.id,
    org.name,
    org.stripe_customer_id,
    org.stripe_subscription_id,
    org.account_type,
    org.whitelisted,
    org.disabled_at,
    org.trial_tier,
    org.trial_state,
    org.trial_converted_at,
    org.trial_demoted_at,
    org.trial_ends_at,
  ]);
  // A generation, not just an ID comparison, rejects A → B → A responses.
  const generation = useRef(0);
  const [candidate, setCandidate] =
    useState<AdminStripeSubscriptionCandidate | null>(null);
  const [validationError, setValidationError] = useState<string | null>(null);
  const [validating, setValidating] = useState(false);
  const invalidateValidation = useCallback(() => {
    generation.current += 1;
    setCandidate(null);
    setValidationError(null);
    setValidating(false);
  }, []);
  // Clear validation before a changed organization context can be acted on.
  useLayoutEffect(() => {
    invalidateValidation();
  }, [context, invalidateValidation]);
  const mounted = useRef(true);
  const assignmentSucceeded = useRef(false);
  useOnUnmount(() => {
    mounted.current = false;
  });

  const focusAssignmentTarget = (): boolean => {
    const target = focusFallbackRef?.current;
    if (
      !mounted.current ||
      !assignmentSucceeded.current ||
      !target?.isConnected
    ) {
      return false;
    }
    target.focus();
    return true;
  };

  const mutation = useMutation({
    mutationFn: setStripeSubscription,
    onMutate: () => cancelOrganizationFetches(qc),
    onSuccess: (updated) => {
      assignmentSucceeded.current = true;
      writeOrganizationToCache(qc, updated);
      void invalidateOrganizationBilling(qc, updated.id);
      void invalidateOrganizations(qc);
      setTimeout(() => {
        focusAssignmentTarget();
      });
    },
    onError: (error) => {
      invalidateOrganizationStats(qc);
      const status = httpStatus(error);
      if (
        status === undefined ||
        status === 404 ||
        status === 409 ||
        status >= 500
      ) {
        void invalidateOrganizations(qc);
      }
    },
  });

  const form = useForm({
    defaultValues: { subscriptionID: "" },
    onSubmit: async ({ value }) => {
      const request: SetStripeSubscriptionRequestBody = {
        organizationId: org.id,
        stripeSubscriptionId: value.subscriptionID.trim(),
      };
      const reviewedOrganization = { id: org.id, name: org.name };
      if (!candidate) return;
      showFailure(null);
      try {
        const updated = await mutation.mutateAsync(request);
        if (!mounted.current) return;
        const text = activatesPayg
          ? `Activated PAYG and linked Stripe subscription ID ${updated.stripe_subscription_id ?? request.stripeSubscriptionId} for ${updated.name}.`
          : `Set Stripe subscription ID ${updated.stripe_subscription_id ?? request.stripeSubscriptionId} for ${updated.name}.`;
        announce(text);
        showFailure(null);
        invalidateValidation();
        setOpen(false);
        form.reset();
      } catch (error) {
        if (!mounted.current) return;
        invalidateValidation();
        const text = `Could not set Stripe subscription ID for ${reviewedOrganization.name}: ${errorMessage(error)}`;
        announce(text);
        showFailure(text);
      }
    },
  });
  const busy = useStore(form.store, (state) => state.isSubmitting);
  const validate = async () => {
    invalidateValidation();
    mutation.reset();
    showFailure(null);
    const subscriptionID = form.getFieldValue("subscriptionID").trim();
    const formatError = !subscriptionID
      ? "Enter a Stripe subscription ID."
      : subscriptionID.length > MAX_STRIPE_SUBSCRIPTION_ID_LENGTH
        ? `Stripe subscription IDs must be ${MAX_STRIPE_SUBSCRIPTION_ID_LENGTH} characters or fewer.`
        : !STRIPE_SUBSCRIPTION_ID.test(subscriptionID)
          ? "Enter a Stripe subscription ID beginning with sub_ and containing only letters, numbers, or underscores."
          : null;
    if (formatError) {
      setValidationError(formatError);
      return;
    }
    const attempt = generation.current;
    setValidating(true);
    try {
      const preview = await getStripeSubscriptionCandidate({
        organizationId: org.id,
        stripeSubscriptionId: subscriptionID,
      });
      if (!mounted.current || generation.current !== attempt) return;
      setCandidate(preview);
    } catch (error) {
      if (!mounted.current || generation.current !== attempt) return;
      const message = errorMessage(error);
      setValidationError(message);
      if (httpStatus(error) === 409) void invalidateOrganizations(qc);
      const text = `Could not verify Stripe subscription ID for ${org.name}: ${message}`;
      announce(text);
      showFailure(text);
    } finally {
      if (mounted.current && generation.current === attempt)
        setValidating(false);
    }
  };

  if (hasValue(org.stripe_subscription_id)) {
    return (
      <span className="flex min-w-0 items-center gap-1">
        <CopyValue
          label="Stripe subscription ID"
          value={org.stripe_subscription_id}
          className="text-sm"
        />
        {org.stripe_subscription_dashboard_url ? (
          <ExternalLinkButton
            href={org.stripe_subscription_dashboard_url}
            label="Open subscription in Stripe"
          />
        ) : null}
      </span>
    );
  }

  const unconvertedTrial =
    org.trial_tier !== undefined && !hasValue(org.trial_converted_at);
  const unconvertedEnterpriseTrial =
    org.trial_tier === "enterprise" && unconvertedTrial;
  const repairNote =
    org.account_type === "payg" && unconvertedTrial
      ? "This PAYG organization has an unconverted trial. Repair its trial lifecycle before linking a Stripe subscription."
      : unconvertedEnterpriseTrial &&
          ((org.account_type === "enterprise" && !org.whitelisted) ||
            (org.account_type === "free" && org.whitelisted))
        ? "This unconverted enterprise trial has incompatible organization access. Review and repair its trial lifecycle and book-a-demo gate before linking a Stripe subscription."
        : null;
  if (repairNote) {
    return (
      <div className="grid gap-1">
        <span className="text-muted-foreground text-sm">-</span>
        <p className="text-muted-foreground max-w-md text-sm">{repairNote}</p>
      </div>
    );
  }
  if (
    org.account_type !== "payg" &&
    org.account_type !== "free" &&
    !(org.account_type === "enterprise" && unconvertedEnterpriseTrial)
  ) {
    return <span className="text-muted-foreground text-sm">-</span>;
  }

  if (!hasValue(org.stripe_customer_id)) {
    return (
      <div className="grid gap-1">
        <span className="text-muted-foreground text-sm">-</span>
        <p className="text-muted-foreground max-w-md text-sm">
          {MISSING_CUSTOMER_NOTE}
        </p>
      </div>
    );
  }

  const error =
    validationError ?? (mutation.error ? errorMessage(mutation.error) : null);
  return (
    <div className="flex flex-wrap items-center gap-2">
      <span className="text-muted-foreground text-sm">-</span>
      <Dialog
        open={open}
        onOpenChange={(next) => {
          if (!next && busy) return;
          invalidateValidation();
          if (next) {
            form.reset();
            mutation.reset();
          }
          setOpen(next);
        }}
      >
        <DialogTrigger asChild>
          <Button variant="outline" size="sm">
            Set subscription ID
          </Button>
        </DialogTrigger>
        <DialogContent
          showCloseButton={!busy}
          onCloseAutoFocus={(event) => {
            if (focusAssignmentTarget()) event.preventDefault();
          }}
        >
          <form
            noValidate
            onSubmit={(event) => {
              event.preventDefault();
              void form.handleSubmit();
            }}
          >
            <DialogHeader>
              <DialogTitle>Set Stripe subscription ID</DialogTitle>
              <DialogDescription>
                Link an existing subscription to {org.name}.
              </DialogDescription>
            </DialogHeader>

            <form.Field name="subscriptionID">
              {(field) => {
                const message = error;
                return (
                  <>
                    <div className="my-4 grid gap-2">
                      <label htmlFor={inputID} className="text-sm font-medium">
                        Stripe subscription ID
                      </label>
                      <div className="flex items-start gap-2">
                        <Input
                          id={inputID}
                          name={field.name}
                          value={field.state.value}
                          required
                          autoComplete="off"
                          placeholder="sub_..."
                          disabled={busy}
                          aria-invalid={Boolean(message)}
                          aria-describedby={
                            message || candidate ? messageID : undefined
                          }
                          onBlur={field.handleBlur}
                          onChange={(event) => {
                            invalidateValidation();
                            field.handleChange(event.target.value);
                            if (mutation.error) mutation.reset();
                          }}
                        />
                        <Button
                          type="button"
                          variant="outline"
                          disabled={
                            busy || validating || !field.state.value.trim()
                          }
                          onClick={() => void validate()}
                          className={
                            validationError
                              ? "border-destructive text-destructive hover:bg-destructive/10 hover:text-destructive dark:border-destructive dark:hover:bg-destructive/10"
                              : candidate
                                ? "border-success-border text-success-foreground hover:bg-success hover:text-success-foreground dark:border-success-border dark:hover:bg-success"
                                : undefined
                          }
                          aria-busy={validating}
                        >
                          Validate
                          {validating ? (
                            <LoaderCircle
                              data-slot="validation-spinner"
                              aria-hidden="true"
                              className="animate-spin motion-reduce:animate-none"
                            />
                          ) : validationError ? (
                            <CircleX
                              data-slot="validation-error"
                              aria-hidden="true"
                            />
                          ) : candidate ? (
                            <CircleCheckBig
                              data-slot="validation-success"
                              aria-hidden="true"
                            />
                          ) : (
                            <ShieldQuestionMark
                              data-slot="validation-idle"
                              aria-hidden="true"
                            />
                          )}
                        </Button>
                      </div>
                    </div>
                    {candidate && (
                      <div
                        id={messageID}
                        role="status"
                        className="grid gap-1 text-sm"
                      >
                        <p className="font-medium">
                          Valid subscription ·{" "}
                          {subscriptionStatusLabel(candidate.status)}
                        </p>
                        <p className="text-muted-foreground">
                          Matches this organization’s Stripe customer.
                        </p>
                      </div>
                    )}
                    {message && (
                      <p
                        id={messageID}
                        role="alert"
                        className="text-destructive text-sm"
                      >
                        {message}
                      </p>
                    )}
                  </>
                );
              }}
            </form.Field>

            {candidate && (
              <div className="mt-4 grid gap-2 text-sm">
                <p>
                  {activatesPayg
                    ? `Applying changes will switch ${org.name} from ${org.account_type === "free" ? "Free" : "Enterprise"} to PAYG.`
                    : "Applying changes will link this subscription. Account type and access stay unchanged."}
                </p>
                {activatesPayg && unconvertedTrial && (
                  <p className="text-muted-foreground">
                    The current trial will be marked converted.
                  </p>
                )}
                {org.disabled_at && (
                  <p className="text-muted-foreground">
                    This organization will remain disabled.
                  </p>
                )}
              </div>
            )}

            <DialogFooter className="mt-4">
              <Button
                type="button"
                variant="ghost"
                size="sm"
                disabled={busy}
                onClick={() => {
                  invalidateValidation();
                  setOpen(false);
                }}
              >
                Cancel
              </Button>
              <Button type="submit" size="sm" disabled={busy || !candidate}>
                {mutation.isPending ? "Setting…" : "Apply changes"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}
