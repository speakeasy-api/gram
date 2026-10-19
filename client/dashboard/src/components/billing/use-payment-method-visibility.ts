import { usePaygCheckoutAccess } from "@/components/billing/payg-checkout-access";
import { showPaymentMethods } from "@/components/billing/payment-method-visibility";
import { useProductTier } from "@/hooks/useProductTier";

export function usePaymentMethodVisibility(): boolean {
  const productTier = useProductTier();
  const { eligible, trialLifecycle } = usePaygCheckoutAccess("active-trial");
  return showPaymentMethods(productTier, trialLifecycle, eligible);
}
