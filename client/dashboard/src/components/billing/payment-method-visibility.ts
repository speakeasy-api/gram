import type { ProductTier } from "@/hooks/useProductTier";
import type { TrialLifecycle } from "@/lib/trial-status";

/** Match the Payment section: eligible active trials, otherwise PAYG only. */
export function showPaymentMethods(
  productTier: ProductTier,
  trialLifecycle: TrialLifecycle,
  canStartCheckout: boolean,
): boolean {
  return trialLifecycle === "active"
    ? canStartCheckout
    : productTier === "payg";
}
