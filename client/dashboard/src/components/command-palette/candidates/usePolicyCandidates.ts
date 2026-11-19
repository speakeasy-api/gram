import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { useRoutes } from "@/routes";
import { isShadowMCPModePolicy } from "@/pages/security/policy-type";
import { useRiskListPolicies } from "@gram/client/react-query/riskListPolicies.js";
import { useMemo } from "react";
import { useNavigate } from "react-router";
import type { LauncherCandidate } from "./types";

/**
 * Risk policies (Guardrails). Admin-only; the caller gates `enabled`.
 * Prompt-based policies are hidden behind the same feature flag the
 * Guardrails page reads (pages/security/PolicyCenter.tsx), so the palette
 * never offers a policy the page would not list. The policy behind the
 * Shadow MCP page's "blocked by default" setting is left out for the same
 * reason: Guardrails does not list it.
 */
export function usePolicyCandidates({
  enabled,
}: {
  enabled: boolean;
}): LauncherCandidate[] {
  const routes = useRoutes();
  const navigate = useNavigate();
  const telemetry = useTelemetry();
  const promptPoliciesEnabled =
    telemetry.isFeatureEnabled(FEATURE_FLAGS.promptPolicies) ?? false;
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
        .filter(
          (policy) =>
            promptPoliciesEnabled || policy.policyType !== "prompt_based",
        )
        .filter((policy) => !isShadowMCPModePolicy(policy))
        .map((policy): LauncherCandidate => ({
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
    [data, promptPoliciesEnabled, routes, navigate],
  );
}
