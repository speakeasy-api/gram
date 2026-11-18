import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import { isShadowMCPModePolicy } from "@/pages/security/policy-type";
import { useRiskListPolicies } from "@gram/client/react-query/riskListPolicies.js";
import { useMemo } from "react";
import { useNavigate } from "react-router";
import type { LauncherCandidate } from "./types";

/**
 * Risk policies (Guardrails). Admin-only; the caller gates `enabled`.
 * The policy behind the Shadow MCP page's "blocked by default" setting is
 * excluded because Guardrails does not list it.
 */
export function usePolicyCandidates({
  enabled,
}: {
  enabled: boolean;
}): LauncherCandidate[] {
  const routes = useRoutes();
  const navigate = useNavigate();
  const gramProject = useProjectSlugForRequests();
  // Keyed by project: the SDK folds gramProject into the query key, so
  // omitting it would share one cache entry across projects. Never throws:
  // a failing source degrades to no candidates rather than blanking the
  // palette.
  const { data } = useRiskListPolicies({ gramProject }, undefined, {
    enabled,
    throwOnError: false,
  });

  return useMemo(
    () =>
      (data?.policies ?? [])
        .filter((policy) => !isShadowMCPModePolicy(policy))
        .map((policy): LauncherCandidate => ({
          id: `policy:${policy.id}`,
          kind: "policy",
          title: policy.name,
          detail: `Guardrail · ${policy.action}`,
          keywords:
            policy.policyType === "prompt_based"
              ? [
                  "risk",
                  "policy",
                  "guardrail",
                  "prompt",
                  "prompt-based",
                  policy.id,
                ]
              : ["risk", "policy", "guardrail", policy.id],
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
