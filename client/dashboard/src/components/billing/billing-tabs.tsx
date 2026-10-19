import { usePaymentMethodVisibility } from "@/components/billing/use-payment-method-visibility";
import type { ReactNode } from "react";
import { useLocation, useNavigate } from "react-router";
import { useStripeSubscription } from "@/components/billing/use-stripe-subscription";
import { isNotFoundError } from "@/lib/route-errors";
import { useSession } from "@/contexts/Auth";
import {
  PageTabsList,
  PageTabsTrigger,
  Tabs,
  TabsContent,
} from "@/components/ui/Tabs";

export function BillingTabs({
  value,
  children,
}: {
  value: "overview" | "payment-method";
  children: ReactNode;
}): JSX.Element {
  const { organization } = useSession();
  const navigate = useNavigate();
  const showPaymentMethod = usePaymentMethodVisibility();
  const location = useLocation();
  const subscription = useStripeSubscription({
    enabled: showPaymentMethod && value === "overview",
  });
  // Detail routes own their headings; overview follows the shared subscription read.
  // A confirmed 404 outranks stale subscription data, just as on the billing page.
  const managesPayment =
    value === "overview"
      ? !!subscription.data && !isNotFoundError(subscription.error)
      : location.pathname.endsWith("/payment-method");
  const paymentPath = managesPayment ? "/payment-method" : "/setup";
  const paymentLabel = managesPayment ? "Payment method" : "Billing setup";
  if (!showPaymentMethod) return <>{children}</>;
  return (
    <Tabs
      value={value}
      onValueChange={(next) => {
        if (next === value) return;
        void navigate(
          `/${organization.slug}/billing${next === "overview" ? "" : paymentPath}`,
        );
      }}
    >
      <div className="border-border -mx-8 border-b px-8">
        <PageTabsList aria-label="Billing navigation">
          <PageTabsTrigger value="overview">Overview</PageTabsTrigger>
          <PageTabsTrigger value="payment-method">
            {paymentLabel}
          </PageTabsTrigger>
        </PageTabsList>
      </div>
      <TabsContent value={value} className="mt-6">
        {children}
      </TabsContent>
    </Tabs>
  );
}
