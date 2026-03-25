import { useSession } from "@/contexts/Auth";
import { useGetPeriodUsage } from "@gram/client/react-query";
import { useMemo } from "react";

export type ProductTier =
  | "base"
  | "base_PAID"
  | "__deprecated__pro"
  | "enterprise"
  | "enterprise_free_trial";

export const useProductTier = () => {
  const session = useSession();
  const { data: periodUsage } = useGetPeriodUsage(undefined, undefined, {
    throwOnError: false,
  });

  const productTier = useMemo(() => {
    if (session.isFreeTrial) {
      return "enterprise_free_trial";
    }
    if (session.rawGramAccountType === "enterprise") {
      return "enterprise";
    }
    if (session.rawGramAccountType === "pro") {
      return "__deprecated__pro";
    }

    if (periodUsage?.hasActiveSubscription) {
      return "base_PAID";
    }
    return "base";
  }, [periodUsage, session.rawGramAccountType, session.isFreeTrial]);

  return productTier;
};
