import { useBillingState } from "./use-billing-state";
import type { ReactNode } from "react";
import { useNavigate } from "react-router";
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
  const billing = useBillingState();
  const managesPayment = billing.status === "manage";
  const paymentPath = managesPayment ? "/payment-method" : "/setup";
  const paymentLabel = managesPayment ? "Payment method" : "Billing setup";
  if (!managesPayment && billing.status !== "needs-setup")
    return <>{children}</>;
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
