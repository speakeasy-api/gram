import { useSession } from "@/contexts/Auth";
import { useMemo } from "react";

export type ProductTier =
  | "base"
  | "base_PAID"
  | "__deprecated__pro"
  | "enterprise";

export const useProductTier = () => {
  const session = useSession();

  const productTier = useMemo(() => {
    if (session.rawGramAccountType === "enterprise") {
      return "enterprise";
    }
    if (session.rawGramAccountType === "pro") {
      return "__deprecated__pro";
    }

    if (session.hasActiveSubscription) {
      return "base_PAID";
    }
    return "base";
  }, [session.hasActiveSubscription, session.rawGramAccountType]);

  return productTier;
};
