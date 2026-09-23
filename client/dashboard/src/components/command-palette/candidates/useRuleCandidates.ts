import { BUILTIN_RULES_BY_CATEGORY } from "@/pages/security/detection-rules-data";
import { useRoutes } from "@/routes";
import { useRiskListCustomDetectionRules } from "@gram/client/react-query/riskListCustomDetectionRules.js";
import { useMemo } from "react";
import { useNavigate } from "react-router";
import type { LauncherCandidate } from "./types";

/**
 * Detection rules: the static built-ins plus the project's custom rules. The
 * detail page's `?rule=<id>` deep link tells them apart via the `custom.` id
 * prefix, so a single param drives both.
 *
 * Returns every rule; whether to show them on an empty query is the palette's
 * decision (they are high-cardinality and used to be search-only).
 */
export function useRuleCandidates({
  enabled,
}: {
  enabled: boolean;
}): LauncherCandidate[] {
  const routes = useRoutes();
  const navigate = useNavigate();
  const { data } = useRiskListCustomDetectionRules(undefined, undefined, {
    enabled,
  });

  return useMemo(() => {
    const builtin = Object.values(BUILTIN_RULES_BY_CATEGORY)
      .flat()
      .map((rule) => ({
        id: rule.id,
        title: rule.title,
        severity: rule.defaultSeverity as string,
      }));
    const custom = (data?.rules ?? []).map((rule) => ({
      id: rule.id,
      title: rule.title,
      severity: rule.severity as string,
    }));
    return [...builtin, ...custom].map((rule): LauncherCandidate => ({
      id: `rule:${rule.id}`,
      kind: "rule",
      title: rule.title,
      detail: `Detection rule · ${rule.severity}`,
      keywords: ["detection", "rule", rule.id],
      verbs: ["open"],
      icon: "scan-search",
      group: "Detection Rules",
      // No per-rule route; the deep link opens the rule's sheet on the
      // Guardrails page's Detection Rules tab.
      run: () => {
        void navigate(
          `${routes.policyCenter.href()}?tab=detection-rules&rule=${encodeURIComponent(rule.id)}`,
        );
      },
    }));
  }, [data, routes, navigate]);
}
