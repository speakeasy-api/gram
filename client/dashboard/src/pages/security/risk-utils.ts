import { useMemo } from "react";
import { useRiskCategories } from "@gram/client/react-query/index.js";
import { DETECTION_RULES, type RuleCategory } from "./policy-data";
import { humanizeRuleId } from "./rule-ids";

const ruleIdToTitle = new Map<string, string>();
for (const rules of Object.values(DETECTION_RULES)) {
  for (const rule of rules) {
    ruleIdToTitle.set(rule.id, rule.title);
  }
}
const RULE_ID_TO_TITLE: ReadonlyMap<string, string> = ruleIdToTitle;

// Per-rule human-readable titles aren't returned by /rpc/risk.categories
// (the API exposes only the canonical classification: source / rule_ids /
// rule_id_prefix). Keep the static title map for label display.
export function getRuleTitleFallback(ruleId: string | undefined): string {
  if (!ruleId) return "-";
  return RULE_ID_TO_TITLE.get(ruleId) ?? humanizeRuleId(ruleId);
}

export type FindingClassifier = (
  source?: string,
  ruleId?: string,
) => RuleCategory | null;

// useFindingClassifier returns a (source, rule_id) -> category lookup
// backed by the canonical Go classifier served at /rpc/risk.categories.
// React Query dedupes across the page so calling this per table row is
// cheap. Returns null while the first fetch is in flight; consumers
// should treat that as "category unknown yet" and render nothing.
export function useFindingClassifier(): FindingClassifier | null {
  const { data } = useRiskCategories(undefined, undefined, {
    staleTime: Number.POSITIVE_INFINITY,
  });
  return useMemo<FindingClassifier | null>(() => {
    const defs = data?.categories;
    if (!defs) return null;
    return (source, ruleId) => {
      for (const def of defs) {
        if (def.source && def.source === source) {
          return def.key as RuleCategory;
        }
        if (def.ruleIds.length > 0 && ruleId && def.ruleIds.includes(ruleId)) {
          return def.key as RuleCategory;
        }
        if (def.ruleIdPrefix && ruleId && ruleId.startsWith(def.ruleIdPrefix)) {
          return def.key as RuleCategory;
        }
      }
      return null;
    };
  }, [data]);
}
