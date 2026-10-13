import { lazy, Suspense } from "react";
import type { PaygCheckoutSession } from "@/components/billing/use-start-payg-checkout";
import { Dialog } from "@/components/ui/Dialog";
import { Spinner } from "@/components/ui/Spinner";

const PaygEmbeddedCheckoutDialog = lazy(() =>
  import("./payg-embedded-checkout-dialog").then((mod) => ({
    default: mod.PaygEmbeddedCheckoutDialog,
  })),
);

export function PaygCheckoutMount({
  session,
  onClose,
}: {
  session: PaygCheckoutSession | null;
  onClose: () => void;
}): JSX.Element | null {
  if (!session) return null;

  return (
    <Suspense fallback={<CheckoutLoading onClose={onClose} />}>
      <PaygEmbeddedCheckoutDialog
        clientSecret={session.clientSecret}
        publishableKey={session.publishableKey}
        onClose={onClose}
      />
    </Suspense>
  );
}

function CheckoutLoading({ onClose }: { onClose: () => void }): JSX.Element {
  return (
    <Dialog
      open
      modal={false}
      onOpenChange={(next) => {
        if (!next) onClose();
      }}
    >
      <Dialog.Content className="max-w-2xl">
        <Dialog.Header>
          <Dialog.Title>Add a payment method</Dialog.Title>
        </Dialog.Header>
        <p className="flex items-center text-sm" role="status">
          <Spinner />
          Loading checkout
        </p>
      </Dialog.Content>
    </Dialog>
  );
}
