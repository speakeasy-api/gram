import { useSession } from "@/contexts/Auth";
import { useGetPeriodUsage } from "@gram/client/react-query";
import { useMemo } from "react";

export type ProductTier =
  | "base"
  | "base_PAID"
  | "__deprecated__pro"
  | "enterprise";

export const useProductTier = () => {
  const session = useSession();
  const { data: periodUsage } = useGetPeriodUsage();

  const productTier = useMemo(() => {
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
  }, [periodUsage]);

  return productTier;
};
