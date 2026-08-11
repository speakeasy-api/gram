import celExamples from "./cel-examples.json";
import {
  DETECTION_RULES,
  POLICY_MESSAGE_TYPE_META,
  type PolicyAction,
  type PolicyMessageType,
  type RuleCategory,
} from "./policy-data";
import { ruleIdToPresidioEntity } from "./rule-ids";

/** Presidio-backed categories */
export const PRESIDIO_CATEGORIES: RuleCategory[] = [
  "financial",
  "pii",
  "government_ids",
  "healthcare",
  "off_policy",
];

/** Categories that are currently available */
export const AVAILABLE_CATEGORIES: Set<RuleCategory> = new Set([
  "secrets",
  ...PRESIDIO_CATEGORIES,
  "shadow_mcp",
  "destructive_tool",
  "cli_destructive",
  "account_identity",
  "prompt_injection",
  "custom",
]);

/** All rule categories in display order. off_policy renders via the
 * PRESIDIO_CATEGORIES spread — don't also list it explicitly. */
export const ALL_CATEGORIES: RuleCategory[] = [
  "secrets",
  ...PRESIDIO_CATEGORIES,
  "shadow_mcp",
  "destructive_tool",
  "cli_destructive",
  "account_identity",
  "prompt_injection",
];

/** Categories whose source the server rejects with action=block; the form
 * must force flag when any of these are selected. Mirrors validateSourceAction
 * in server/internal/risk/impl.go. */
export const FLAG_ONLY_CATEGORIES: Set<RuleCategory> = new Set([
  "destructive_tool",
  "cli_destructive",
  "account_identity",
]);

/** Built-in detectors that run at the category level and have no individual
 *  sub-rules in DETECTION_RULES (their rule list is intentionally empty).
 *  Selecting one of these is enough to enable the policy on its own. */
export const CATEGORY_LEVEL_DETECTORS: Set<RuleCategory> = new Set([
  "prompt_injection",
  "shadow_mcp",
  "destructive_tool",
  "cli_destructive",
]);

export const ALL_POLICY_MESSAGE_TYPES = Object.keys(
  POLICY_MESSAGE_TYPE_META,
) as Array<PolicyMessageType>;

export type CategoriesPayload = {
  sources: string[];
  presidioEntities: string[];
  promptInjectionRules: string[];
  disabledRules: string[];
};

/** Derive selected categories from a policy's sources + presidioEntities.
 *
 * DETECTION_RULES.id is the canonical `pii.<snake_case>` form; the wire format
 * stored on the policy is the UPPER_SNAKE entity name Presidio speaks. We
 * translate at this boundary so callers never see the wire format. */
export function policyToCategories(
  sources: string[],
  presidioEntities?: string[],
): Set<RuleCategory> {
  const cats = new Set<RuleCategory>();
  if (sources.includes("gitleaks")) cats.add("secrets");
  if (sources.includes("shadow_mcp")) cats.add("shadow_mcp");
  if (sources.includes("destructive_tool")) cats.add("destructive_tool");
  if (sources.includes("cli_destructive")) cats.add("cli_destructive");
  if (sources.includes("account_identity")) cats.add("account_identity");
  if (sources.includes("prompt_injection")) cats.add("prompt_injection");
  for (const cat of PRESIDIO_CATEGORIES) {
    const wireEntities = DETECTION_RULES[cat].map((r) =>
      ruleIdToPresidioEntity(r.id),
    );
    if (wireEntities.some((id) => presidioEntities?.includes(id))) {
      cats.add(cat);
    }
  }
  return cats;
}

/** Derive sources, presidioEntities, promptInjectionRules, and disabledRules
 * from selected categories + per-rule disable set.
 *
 * - `sources` selects which scanners run (category-level).
 * - `presidioEntities` (UPPER_SNAKE) narrows the Presidio query to only the
 *   entities the user has enabled across selected presidio-backed categories.
 *   Rules in `disabledRules` are omitted from this list so the scanner is
 *   never even asked about them.
 * - `disabledRules` (canonical ids like `secret.aws_access_token`) is the
 *   per-rule allowlist applied post-scan for sources without entity-level
 *   query support (gitleaks). It also serves as a redundancy net for
 *   presidio in case of API drift.
 * - `promptInjectionRules` stays empty for backward compatibility — whether
 *   the L1 LLM judge runs on top of the L0 heuristics is chosen per-org via a
 *   feature flag, not by the policy author. */
export function categoriesToPayload(
  cats: Set<RuleCategory>,
  disabledRules: Set<string>,
  pinnedHidden: Set<string> = new Set(),
): CategoriesPayload {
  const sources: string[] = [];
  const presidioEntities: string[] = [];
  const promptInjectionRules: string[] = [];

  if (cats.has("secrets")) sources.push("gitleaks");
  if (cats.has("shadow_mcp")) sources.push("shadow_mcp");
  if (cats.has("destructive_tool")) sources.push("destructive_tool");
  if (cats.has("cli_destructive")) sources.push("cli_destructive");
  if (cats.has("account_identity")) sources.push("account_identity");
  if (cats.has("prompt_injection")) sources.push("prompt_injection");
  for (const cat of PRESIDIO_CATEGORIES) {
    if (cats.has(cat)) {
      for (const rule of DETECTION_RULES[cat]) {
        if (disabledRules.has(rule.id)) continue;
        // Hidden rules (deprecated / unreliable upstream) are never newly
        // serialized into the Presidio query just because their category is
        // selected. We only keep one if the policy being edited already
        // pinned it, so an edit round-trips without silently dropping it.
        if ("hidden" in rule && rule.hidden && !pinnedHidden.has(rule.id)) {
          continue;
        }
        presidioEntities.push(ruleIdToPresidioEntity(rule.id));
      }
    }
  }
  if (presidioEntities.length > 0) sources.push("presidio");

  // Persist disabled ids only for currently-selected categories. If a user
  // unselects a category they shouldn't carry over its per-rule overrides.
  const persistedDisabled: string[] = [];
  for (const cat of cats) {
    for (const rule of DETECTION_RULES[cat] ?? []) {
      if (disabledRules.has(rule.id)) persistedDisabled.push(rule.id);
    }
  }

  return {
    sources,
    presidioEntities,
    promptInjectionRules,
    disabledRules: persistedDisabled,
  };
}

/** Parse the comma-separated approved-domains input into the array the API
 *  expects. Splits on commas and whitespace; the server normalizes each entry
 *  (lowercase, strips a leading '@') and rejects implausible domains. */
export function parseApprovedEmailDomains(raw: string): string[] {
  return raw
    .split(/[,\s]+/)
    .map((domain) => domain.trim())
    .filter((domain) => domain.length > 0);
}

/** Canonical ids of hidden rules an existing policy already pins via its
 *  presidioEntities. Lets an edit preserve a deprecated entity the policy
 *  carried before it was hidden, without ever newly adding one. */
export function pinnedHiddenRuleIds(presidioEntities?: string[]): Set<string> {
  const pinned = new Set<string>();
  if (!presidioEntities) return pinned;
  for (const cat of PRESIDIO_CATEGORIES) {
    for (const rule of DETECTION_RULES[cat]) {
      if (
        "hidden" in rule &&
        rule.hidden &&
        presidioEntities.includes(ruleIdToPresidioEntity(rule.id))
      ) {
        pinned.add(rule.id);
      }
    }
  }
  return pinned;
}

export function policyMessageTypesForForm(
  messageTypes?: string[],
): Set<PolicyMessageType> {
  if (!messageTypes?.length) {
    return new Set(ALL_POLICY_MESSAGE_TYPES);
  }

  return new Set(
    messageTypes.filter((type): type is PolicyMessageType =>
      ALL_POLICY_MESSAGE_TYPES.includes(type as PolicyMessageType),
    ),
  );
}

/** Example scope CEL snippets offered beneath the include field — narrow a
 *  policy to a subset of messages. Lives in cel-examples.json so the celenv
 *  Go test compile-checks every snippet against the real engine. */
export const SCOPE_INCLUDE_CEL_EXAMPLES: { label: string; expr: string }[] =
  celExamples.scope_include;

/** Example scope CEL snippets offered beneath the exempt field — take matching
 *  messages out of the policy entirely (an allowlist). */
export const SCOPE_EXEMPT_CEL_EXAMPLES: { label: string; expr: string }[] =
  celExamples.scope_exempt;

export const ACTION_OPTIONS: {
  value: PolicyAction;
  title: string;
  description: string;
}[] = [
  {
    value: "flag",
    title: "Log for review",
    description: "Log findings for review without interrupting the session",
  },
  {
    value: "warn",
    title: "Warn & confirm",
    description:
      "Warn the user and require them to acknowledge before the action proceeds. Falls back to blocking where confirmation isn't possible.",
  },
  {
    value: "block",
    title: "Deny the request",
    description: "Deny prompts and tool calls that match detection rules",
  },
];
