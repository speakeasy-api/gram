import {
  DETECTION_RULES,
  type DetectorMode,
  type RuleCategory,
} from "@/pages/security/policy-data";
import { PRESIDIO_CATEGORIES } from "@/pages/security/policy-form";
import { ruleIdToPresidioEntity } from "@/pages/security/rule-ids";

/** The sources (and, for the Presidio engine, the entity list) the onboarding
 *  wizard writes when it turns a category on. */
export function buildPolicyPayload(
  cat: RuleCategory,
  mode: DetectorMode = "presidio",
): {
  sources: string[];
  presidioEntities?: string[];
} {
  if (cat === "shadow_mcp") return { sources: ["shadow_mcp"] };
  if (cat === "secrets") return { sources: ["gitleaks"] };
  if (cat === "prompt_injection") return { sources: ["prompt_injection"] };
  // The analyzer ignores the entity list; an empty one keeps the policy
  // engine-agnostic so a flag-off falls back to scanning every entity.
  if (mode === "llm" && cat === "pii") {
    return { sources: ["presidio"], presidioEntities: [] };
  }
  if (PRESIDIO_CATEGORIES.includes(cat)) {
    return {
      sources: ["presidio"],
      presidioEntities: DETECTION_RULES[cat]
        .filter((r) => !r.hidden)
        .map((r) => ruleIdToPresidioEntity(r.id)),
    };
  }
  return { sources: [] };
}

export function categoryMatchesPolicy(
  cat: RuleCategory,
  sources: string[],
  presidioEntities?: string[],
  mode: DetectorMode = "presidio",
): boolean {
  if (cat === "shadow_mcp") return sources.includes("shadow_mcp");
  if (cat === "secrets") return sources.includes("gitleaks");
  if (cat === "prompt_injection") return sources.includes("prompt_injection");
  if (mode === "llm" && cat === "pii") return sources.includes("presidio");
  if (PRESIDIO_CATEGORIES.includes(cat)) {
    if (!sources.includes("presidio") || !presidioEntities?.length)
      return false;
    const wire = new Set(
      DETECTION_RULES[cat].map((r) => ruleIdToPresidioEntity(r.id)),
    );
    return presidioEntities.some((e) => wire.has(e));
  }
  return false;
}
