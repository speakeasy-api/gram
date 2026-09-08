import { useId, useRef, useState, type FormEvent, type JSX } from "react";
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
  invalidateOrganizationActivity,
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
  const [customerID, setCustomerID] = useState("");
  const [clientError, setClientError] = useState<string | null>(null);
  const [lookupError, setLookupError] = useState<string | null>(null);
  const [confirming, setConfirming] = useState(false);
  const mounted = useRef(true);
  useOnUnmount(() => {
    mounted.current = false;
  });

  const mutation = useMutation({
    mutationFn: setStripeCustomer,
    onMutate: () => cancelOrganizationFetches(qc),
    onSuccess: (updated) => {
      writeOrganizationToCache(qc, updated);
      invalidateOrganizationActivity(qc, updated.id);
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

  const trimmedCustomerID = customerID.trim();
  const busy = confirming || mutation.isPending;

  const submit = async (event: FormEvent<HTMLFormElement>): Promise<void> => {
    event.preventDefault();
    if (busy) return;

    let invalid: string | null = null;
    if (trimmedCustomerID.length === 0) {
      invalid = "Enter a Stripe customer ID.";
    } else if (trimmedCustomerID.length > MAX_STRIPE_CUSTOMER_ID_LENGTH) {
      invalid = `Stripe customer IDs must be ${MAX_STRIPE_CUSTOMER_ID_LENGTH} characters or fewer.`;
    } else if (!STRIPE_CUSTOMER_ID.test(trimmedCustomerID)) {
      invalid =
        "Enter a Stripe customer ID beginning with cus_ and containing only letters, numbers, or underscores.";
    }
    setClientError(invalid);
    if (invalid) return;

    const request: SetStripeCustomerRequest = {
      organization_id: org.id,
      stripe_customer_id: trimmedCustomerID,
    };
    const reviewedOrganization = { id: org.id, name: org.name };
    setConfirming(true);
    setLookupError(null);
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
      setLookupError(message);
      setConfirming(false);
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
    setConfirming(false);
    if (!mounted.current) return;
    if (!confirmed) return;

    showFailure(null);
    mutation.mutate(request, {
      onSuccess: (updated) => {
        const text = `Set Stripe customer ID ${updated.stripe_customer_id ?? request.stripe_customer_id} for ${updated.name}.`;
        announce(text);
        showFailure(null);
        setOpen(false);
        setCustomerID("");
        setClientError(null);
        setLookupError(null);
      },
      onError: (error) => {
        const text = `Could not set Stripe customer ID for ${reviewedOrganization.name}: ${errorMessage(error)}`;
        announce(text);
        showFailure(text);
      },
    });
  };

  const error =
    clientError ??
    lookupError ??
    (mutation.error ? errorMessage(mutation.error) : null);
  return (
    <>
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-muted-foreground text-sm">-</span>
        <Dialog
          open={open}
          onOpenChange={(next) => {
            if (!next && busy) return;
            if (next) {
              setCustomerID("");
              setClientError(null);
              mutation.reset();
              setLookupError(null);
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
            <form onSubmit={(event) => void submit(event)}>
              <DialogHeader>
                <DialogTitle>Set Stripe customer ID</DialogTitle>
                <DialogDescription>
                  This can only set an organization&apos;s initial Stripe
                  customer ID. It cannot replace a customer or set a
                  subscription. Review the live Stripe customer details and
                  verify the customer belongs to this organization before
                  saving.
                </DialogDescription>
              </DialogHeader>

              <div className="my-4 grid gap-2">
                <label htmlFor={inputID} className="text-sm font-medium">
                  Stripe customer ID
                </label>
                <Input
                  id={inputID}
                  value={customerID}
                  required
                  autoComplete="off"
                  placeholder="cus_..."
                  disabled={busy}
                  aria-invalid={Boolean(error)}
                  aria-describedby={error ? messageID : undefined}
                  onChange={(event) => {
                    setCustomerID(event.target.value);
                    setClientError(null);
                    if (mutation.error) mutation.reset();
                    setLookupError(null);
                  }}
                />
              </div>

              {error && (
                <p
                  id={messageID}
                  role="alert"
                  className="text-destructive text-sm"
                >
                  {error}
                </p>
              )}

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
          </DialogContent>
        </Dialog>
      </div>
      {confirmDialog}
    </>
  );
}
