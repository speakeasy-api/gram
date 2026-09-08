import { useId, useRef, useState, type JSX } from "react";
import { useForm, useStore } from "@tanstack/react-form";
import { useMutation, useQueryClient } from "@tanstack/react-query";

import { useConfirmDialog } from "@/components/ConfirmDialog";
import { CopyValue } from "@/components/CopyValue";
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
import {
  errorMessage,
  getStripeCustomer,
  GramAdminError,
  setStripeCustomer,
  type AdminStripeCustomer,
  type AdminOrganization,
  type SetStripeCustomerRequest,
} from "@/lib/gramAdminApi";
import { useWriteReport } from "@/pages/organizations/writeReport";

const STRIPE_CUSTOMER_ID = /^cus_[A-Za-z0-9_]+$/;
const MAX_STRIPE_CUSTOMER_ID_LENGTH = 255;

function ConfirmationDetail({
  label,
  value,
}: {
  label: string;
  value: string;
}): JSX.Element {
  return (
    <div className="grid gap-1 sm:grid-cols-[8rem_minmax(0,1fr)]">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="break-all font-mono">{value}</dd>
    </div>
  );
}

export function SetStripeCustomer({
  org,
}: {
  org: AdminOrganization;
}): JSX.Element {
  const qc = useQueryClient();
  const [confirm, confirmDialog] = useConfirmDialog();
  const { announce, showFailure } = useWriteReport();
  const inputID = useId();
  const messageID = useId();
  const [open, setOpen] = useState(false);
  const mounted = useRef(true);
  useOnUnmount(() => {
    mounted.current = false;
  });

  const mutation = useMutation({
    mutationFn: setStripeCustomer,
    onMutate: () => cancelOrganizationFetches(qc),
    onSuccess: (updated) => {
      writeOrganizationToCache(qc, updated);
      void invalidateOrganizationBilling(qc, updated.id);
      void invalidateOrganizations(qc);
    },
    onError: (error) => {
      invalidateOrganizationStats(qc);
      if (
        !(error instanceof GramAdminError) ||
        error.status === 404 ||
        error.status === 409 ||
        error.status >= 500
      ) {
        void invalidateOrganizations(qc);
      }
    },
  });

  const form = useForm({
    defaultValues: { customerID: "" },
    onSubmit: async ({ value }) => {
      const request: SetStripeCustomerRequest = {
        organization_id: org.id,
        stripe_customer_id: value.customerID.trim(),
      };
      const reviewedOrganization = { id: org.id, name: org.name };
      showFailure(null);
      let preview: AdminStripeCustomer;
      try {
        preview = await getStripeCustomer(
          request.organization_id,
          request.stripe_customer_id,
        );
        if (!mounted.current) return;
      } catch (error) {
        if (!mounted.current) return;
        const message = errorMessage(error);
        if (error instanceof GramAdminError && error.status === 409) {
          void invalidateOrganizations(qc);
        }
        const text = `Could not verify Stripe customer ID for ${reviewedOrganization.name}: ${message}`;
        form.setErrorMap({ onSubmit: { form: message, fields: {} } });
        announce(text);
        showFailure(text);
        return;
      }

      const confirmed = await confirm({
        title: `Set Stripe customer for ${reviewedOrganization.name}?`,
        description:
          "Verify this live Stripe customer belongs to the target organization before saving.",
        details: (
          <dl className="bg-muted/20 grid gap-2 border p-3 text-sm">
            <ConfirmationDetail
              label="Organization"
              value={`${reviewedOrganization.name} (${reviewedOrganization.id})`}
            />
            <ConfirmationDetail
              label="Requested ID"
              value={request.stripe_customer_id}
            />
            <ConfirmationDetail label="Stripe returned ID" value={preview.id} />
            {preview.name && (
              <ConfirmationDetail label="Name" value={preview.name} />
            )}
            {preview.email && (
              <ConfirmationDetail label="Email" value={preview.email} />
            )}
            {preview.description && (
              <ConfirmationDetail
                label="Description"
                value={preview.description}
              />
            )}
            <ConfirmationDetail
              label="Mode"
              value={preview.livemode ? "Live" : "Test"}
            />
          </dl>
        ),
        confirmLabel: "Set customer ID",
      });
      if (!mounted.current) return;
      if (!confirmed) return;

      showFailure(null);
      try {
        const updated = await mutation.mutateAsync(request);
        if (!mounted.current) return;
        const text = `Set Stripe customer ID ${updated.stripe_customer_id ?? request.stripe_customer_id} for ${updated.name}.`;
        announce(text);
        showFailure(null);
        setOpen(false);
        form.reset();
      } catch (error) {
        if (!mounted.current) return;
        const text = `Could not set Stripe customer ID for ${reviewedOrganization.name}: ${errorMessage(error)}`;
        announce(text);
        showFailure(text);
      }
    },
  });
  const busy = useStore(form.store, (state) => state.isSubmitting);
  const lookupError = useStore(form.store, (state) => {
    const error = state.errorMap.onSubmit;
    return typeof error === "string" ? error : null;
  });
  if (org.stripe_customer_id !== undefined && org.stripe_customer_id !== null) {
    return org.stripe_customer_id ? (
      <CopyValue
        label="Stripe customer ID"
        value={org.stripe_customer_id}
        className="text-sm"
      />
    ) : (
      <span className="text-muted-foreground text-sm">-</span>
    );
  }

  // A subscription without a customer is inconsistent but still not an empty
  // billing identity. The server applies the same two-field guard.
  if (
    org.stripe_subscription_id !== undefined &&
    org.stripe_subscription_id !== null
  ) {
    return <span className="text-muted-foreground text-sm">-</span>;
  }

  const error =
    lookupError ?? (mutation.error ? errorMessage(mutation.error) : null);
  return (
    <div className="flex flex-wrap items-center gap-2">
      <span className="text-muted-foreground text-sm">-</span>
      <Dialog
        open={open}
        onOpenChange={(next) => {
          if (!next && busy) return;
          if (next) {
            form.reset();
            mutation.reset();
          }
          setOpen(next);
        }}
      >
        <DialogTrigger asChild>
          <Button variant="outline" size="sm">
            Set customer ID
          </Button>
        </DialogTrigger>
        <DialogContent showCloseButton={!busy}>
          <form
            noValidate
            onSubmit={(event) => {
              event.preventDefault();
              form.setErrorMap({ onSubmit: undefined });
              void form.handleSubmit();
            }}
          >
            <DialogHeader>
              <DialogTitle>Set Stripe customer ID</DialogTitle>
              <DialogDescription>
                This can only set an organization&apos;s initial Stripe customer
                ID. It cannot replace a customer or set a subscription. Review
                the live Stripe customer details and verify the customer belongs
                to this organization before saving.
              </DialogDescription>
            </DialogHeader>

            <form.Field
              name="customerID"
              validators={{
                onChange: ({ value }) => {
                  const trimmed = value.trim();
                  if (!trimmed) return "Enter a Stripe customer ID.";
                  if (trimmed.length > MAX_STRIPE_CUSTOMER_ID_LENGTH) {
                    return `Stripe customer IDs must be ${MAX_STRIPE_CUSTOMER_ID_LENGTH} characters or fewer.`;
                  }
                  if (!STRIPE_CUSTOMER_ID.test(trimmed)) {
                    return "Enter a Stripe customer ID beginning with cus_ and containing only letters, numbers, or underscores.";
                  }
                  return undefined;
                },
              }}
            >
              {(field) => {
                const message = field.state.meta.errors[0] ?? error;
                return (
                  <>
                    <div className="my-4 grid gap-2">
                      <label htmlFor={inputID} className="text-sm font-medium">
                        Stripe customer ID
                      </label>
                      <Input
                        id={inputID}
                        name={field.name}
                        value={field.state.value}
                        required
                        autoComplete="off"
                        placeholder="cus_..."
                        disabled={busy}
                        aria-invalid={Boolean(message)}
                        aria-describedby={message ? messageID : undefined}
                        onBlur={field.handleBlur}
                        onChange={(event) => {
                          field.handleChange(event.target.value);
                          form.setErrorMap({ onSubmit: undefined });
                          if (mutation.error) mutation.reset();
                        }}
                      />
                    </div>
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

            <DialogFooter className="mt-4">
              <Button
                type="button"
                variant="ghost"
                size="sm"
                disabled={busy}
                onClick={() => setOpen(false)}
              >
                Cancel
              </Button>
              <Button type="submit" size="sm" disabled={busy}>
                {mutation.isPending ? "Setting…" : "Review and set"}
              </Button>
            </DialogFooter>
          </form>
          {confirmDialog}
        </DialogContent>
      </Dialog>
    </div>
  );
}
