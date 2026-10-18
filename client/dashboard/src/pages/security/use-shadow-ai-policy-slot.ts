import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { useRiskListPolicies } from "@gram/client/react-query/riskListPolicies.js";
import { useRoutes } from "@/routes";
import { findBlockingShadowMCPPolicy } from "./policy-type";

/** The state of this project's single Shadow AI slot. The server allows one
 *  enabled blocking Shadow MCP policy per project, so every entry point that
 *  would create one has to know whether the slot is taken; a pending or
 *  failed read is not proof that it is free. */
export interface ShadowAIPolicySlot {
  pending: boolean;
  failed: boolean;
  existing: RiskPolicy | undefined;
  /** The existing policy's detail route, when there is one. */
  href: string | undefined;
}

export function useShadowAIPolicySlot(): ShadowAIPolicySlot {
  const routes = useRoutes();
  const policiesQuery = useRiskListPolicies(undefined, undefined, {
    throwOnError: false,
  });
  const existing = findBlockingShadowMCPPolicy(
    policiesQuery.data?.policies ?? [],
  );
  return {
    pending: !policiesQuery.data && !policiesQuery.isError,
    // A failed refetch over a cached list still answers the question.
    failed: policiesQuery.isError && !policiesQuery.data,
    existing,
    href: existing ? routes.policyCenter.detail.href(existing.id) : undefined,
  };
}
