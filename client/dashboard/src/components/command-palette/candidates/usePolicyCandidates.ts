import { useRoutes } from "@/routes";
import { useRiskListPolicies } from "@gram/client/react-query/riskListPolicies.js";
import { useMemo } from "react";
import { useNavigate } from "react-router";
import type { LauncherCandidate } from "./types";

/** Risk policies (Guardrails). Admin-only; the caller gates `enabled`. */
export function usePolicyCandidates({
  enabled,
}: {
  enabled: boolean;
}): LauncherCandidate[] {
  const routes = useRoutes();
  const navigate = useNavigate();
  const { data } = useRiskListPolicies(undefined, undefined, { enabled });

  return useMemo(
    () =>
      (data?.policies ?? []).map((policy): LauncherCandidate => ({
        id: `policy:${policy.id}`,
        kind: "policy",
        title: policy.name,
        detail: `Guardrail · ${policy.action}`,
        keywords: ["risk", "policy", "guardrail", policy.id],
        verbs: ["open"],
        icon: "shield-check",
        group: "Guardrails",
        // No per-policy route; the deep link opens the policy's sheet by id.
        run: () => {
          void navigate(
            `${routes.policyCenter.href()}?policy=${encodeURIComponent(policy.id)}`,
          );
        },
      })),
    [data, routes, navigate],
  );
}
