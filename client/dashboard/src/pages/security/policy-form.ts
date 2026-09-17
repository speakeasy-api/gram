import celExamples from "./cel-examples.json";
import {
  DETECTION_RULES,
  type DetectorMode,
  type PolicyAction,
  type RuleCategory,
} from "./policy-data";
import { ruleIdToPresidioEntity } from "./rule-ids";

export type { DetectorMode } from "./policy-data";

/** Presidio-backed categories */
export const PRESIDIO_CATEGORIES: RuleCategory[] = [
  "financial",
  "pii",
  "government_ids",
  "healthcare",
];

/** The personal-data categories the editor offers under `mode`. The LLM
 *  analyzer decides the category per finding, so it exposes a single `pii`
 *  detector in place of the four Presidio-backed ones. */
export function personalDataCategories(
  mode: DetectorMode = "presidio",
): RuleCategory[] {
  return mode === "llm" ? ["pii"] : PRESIDIO_CATEGORIES;
}

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

const LLM_AVAILABLE_CATEGORIES: Set<RuleCategory> = new Set([
  "secrets",
  ...personalDataCategories("llm"),
  "shadow_mcp",
  "destructive_tool",
  "cli_destructive",
  "account_identity",
  "prompt_injection",
  "custom",
]);

/** Categories that are currently available under `mode`. */
export function availableCategories(
  mode: DetectorMode = "presidio",
): Set<RuleCategory> {
  return mode === "llm" ? LLM_AVAILABLE_CATEGORIES : AVAILABLE_CATEGORIES;
}

/** All rule categories in display order. */
export const ALL_CATEGORIES: RuleCategory[] = [
  "secrets",
  ...PRESIDIO_CATEGORIES,
  "off_policy",
  "shadow_mcp",
  "destructive_tool",
  "cli_destructive",
  "account_identity",
  "prompt_injection",
];

/** Display order under the LLM analyzer: one `pii` card, and no `off_policy`
 *  placeholder (the model has no such risk). */
const LLM_ALL_CATEGORIES: RuleCategory[] = [
  "secrets",
  ...personalDataCategories("llm"),
  "shadow_mcp",
  "destructive_tool",
  "cli_destructive",
  "account_identity",
  "prompt_injection",
];

/** All rule categories in display order under `mode`. */
export function allCategories(mode: DetectorMode = "presidio"): RuleCategory[] {
  return mode === "llm" ? LLM_ALL_CATEGORIES : ALL_CATEGORIES;
}

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

const LLM_CATEGORY_LEVEL_DETECTORS: Set<RuleCategory> = new Set([
  ...CATEGORY_LEVEL_DETECTORS,
  "pii",
]);

/** Category-level detectors under `mode`. Under the LLM analyzer `pii` has no
 *  rule list of its own, so selecting it enables the policy by itself. */
export function categoryLevelDetectors(
  mode: DetectorMode = "presidio",
): Set<RuleCategory> {
  return mode === "llm"
    ? LLM_CATEGORY_LEVEL_DETECTORS
    : CATEGORY_LEVEL_DETECTORS;
}

/** The Presidio-backed categories that have no card of their own under the
 *  LLM analyzer. */
const LEGACY_PERSONAL_DATA_CATEGORIES = PRESIDIO_CATEGORIES.filter(
  (c) => c !== "pii",
);

/** The selected category set expressed in the vocabulary of `mode`. The flag
 *  that picks the mode resolves asynchronously, so a form seeded under the
 *  legacy engine can find itself under the LLM analyzer with one of the
 *  Presidio-backed categories selected and no `pii` card to show it on; fold
 *  those into `pii` so the card and the saved sources agree. Returns `cats`
 *  itself when nothing needs to change. */
export function normalizeCategoriesForMode(
  cats: Set<RuleCategory>,
  mode: DetectorMode,
): Set<RuleCategory> {
  if (
    mode !== "llm" ||
    !LEGACY_PERSONAL_DATA_CATEGORIES.some((c) => cats.has(c))
  ) {
    return cats;
  }
  const next = new Set(cats);
  for (const c of LEGACY_PERSONAL_DATA_CATEGORIES) next.delete(c);
  next.add("pii");
  return next;
}

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
  mode: DetectorMode = "presidio",
): Set<RuleCategory> {
  const cats = new Set<RuleCategory>();
  if (sources.includes("gitleaks")) cats.add("secrets");
  if (sources.includes("shadow_mcp")) cats.add("shadow_mcp");
  if (sources.includes("destructive_tool")) cats.add("destructive_tool");
  if (sources.includes("cli_destructive")) cats.add("cli_destructive");
  if (sources.includes("account_identity")) cats.add("account_identity");
  if (sources.includes("prompt_injection")) cats.add("prompt_injection");
  if (mode === "llm") {
    // The analyzer ignores the entity list: any presidio-sourced policy is a
    // personal-data policy, entities or not.
    if (sources.includes("presidio")) cats.add("pii");
    return cats;
  }
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
 *   feature flag, not by the policy author.
 * - Under the `llm` mode the entity list is not consulted at all: `pii` on
 *   means `sources` carries `presidio` with an empty `presidioEntities`, and
 *   the model picks the personal-data category per finding. */
export function categoriesToPayload(
  cats: Set<RuleCategory>,
  disabledRules: Set<string>,
  pinnedHidden: Set<string> = new Set(),
  mode: DetectorMode = "presidio",
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
  if (mode === "llm") {
    const llmCats = normalizeCategoriesForMode(cats, mode);
    if (llmCats.has("pii")) sources.push("presidio");
    return {
      sources,
      presidioEntities,
      promptInjectionRules,
      disabledRules: persistedDisabledRules(llmCats, disabledRules, mode),
    };
  }
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

  return {
    sources,
    presidioEntities,
    promptInjectionRules,
    disabledRules: persistedDisabledRules(cats, disabledRules, mode),
  };
}

/** The categories whose stored per-category settings (rule overrides,
 *  detection scopes) an edit keeps: the selected ones, so unselecting a
 *  category drops its settings. Under the LLM analyzer `pii` stands in for
 *  every Presidio-backed category, so a stored setting on any of them
 *  survives an edit (the analyzer ignores it; turning the flag off restores
 *  it). */
export function persistedCategories(
  cats: Set<RuleCategory>,
  mode: DetectorMode,
): Set<RuleCategory> {
  if (mode !== "llm" || !cats.has("pii")) return cats;
  // `off_policy` rides along: the legacy engine resolves it for an
  // entity-less presidio policy, so it can carry a stored scope too.
  return new Set([...cats, ...PRESIDIO_CATEGORIES, "off_policy"]);
}

/** Disabled ids worth persisting, see `persistedCategories`. */
function persistedDisabledRules(
  cats: Set<RuleCategory>,
  disabledRules: Set<string>,
  mode: DetectorMode,
): string[] {
  const persisted: string[] = [];
  for (const cat of persistedCategories(cats, mode)) {
    for (const rule of DETECTION_RULES[cat] ?? []) {
      if (disabledRules.has(rule.id)) persisted.push(rule.id);
    }
  }
  return persisted;
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

/** Categories a policy detects on, for scope resolution. Broader than
 *  `policyToCategories`: a presidio policy with no entity list detects every
 *  presidio-backed category, and custom rules carry the `custom` category.
 *  Under the LLM analyzer any presidio policy detects the single `pii`
 *  category, whatever its entity list says. */
export function policyDetectionCategories(
  policy: {
    policyType?: string;
    sources?: string[];
    presidioEntities?: string[];
    customRuleIds?: string[];
  },
  mode: DetectorMode = "presidio",
): Set<RuleCategory> {
  if (policy.policyType === "prompt_based") return new Set(["prompt_policy"]);

  const sources = policy.sources ?? [];
  const categories = policyToCategories(sources, policy.presidioEntities, mode);
  if (
    mode === "presidio" &&
    sources.includes("presidio") &&
    !policy.presidioEntities?.length
  ) {
    for (const category of [...PRESIDIO_CATEGORIES, "off_policy" as const]) {
      categories.add(category);
    }
  }
  if (policy.customRuleIds?.length) categories.add("custom");
  return categories;
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
  {
    value: "quarantine",
    title: "Quarantine session",
    description:
      "Deny the matching event and freeze prompts and tool calls in that session until an admin releases it.",
  },
];
