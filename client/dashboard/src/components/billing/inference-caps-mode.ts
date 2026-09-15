import { useSession } from "@/contexts/Auth";
import { type ProductTier, useProductTier } from "@/hooks/useProductTier";
import { useTrialNow } from "@/hooks/useTrialNow";
import {
  getTrialLifecycleFromDates,
  type TrialLifecycle,
} from "@/lib/trial-status";

export type InferenceCapsMode = "payg" | "product-trial" | "hidden";

function inferenceCapsMode(
  productTier: ProductTier,
  trialLifecycle: TrialLifecycle,
): InferenceCapsMode {
  if (productTier !== "payg" && productTier !== "enterprise") return "hidden";
  if (trialLifecycle === "active") return "product-trial";
  return productTier === "payg" ? "payg" : "hidden";
}

/** One shared lifecycle gate for the cap controls and reached-cap banners. */
export function useInferenceCapsMode(): InferenceCapsMode {
  const productTier = useProductTier();
  const { trial } = useSession();
  const now = useTrialNow(trial);

  return inferenceCapsMode(productTier, getTrialLifecycleFromDates(trial, now));
}
