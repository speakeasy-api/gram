import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { RULE_CATEGORY_META, type RuleCategory } from "./policy-data";
import { policyToCategories } from "./policy-form";

/** Words that pad out a category label without distinguishing it, so a name
 *  that leaves them out ("Healthcare Scanner" for "Healthcare Information")
 *  still says everything the label says. */
const GENERIC_LABEL_WORDS = new Set([
  "information",
  "identifiable",
  "content",
  "data",
]);

/** Short forms a policy name may use in place of the full category label. */
const CATEGORY_LABEL_ALIASES: Partial<Record<RuleCategory, string>> = {
  pii: "PII",
  government_ids: "Government IDs",
};

/** Prefix the server puts on prompt-policy names it derives from the guardrail
 *  itself (see fallbackPromptPolicyName in server/internal/risk/impl.go). */
const DERIVED_PROMPT_NAME_PREFIX = ["prompt", "policy"];

/** A short final prefix word only counts as a partial match once enough words
 *  before it matched exactly; on its own it is more likely a coincidence than a
 *  truncation. */
const MIN_PARTIAL_TOKEN_LENGTH = 4;
const MIN_ANCHORING_TOKENS = 3;

export type PolicySummaryPolicy = Pick<
  RiskPolicy,
  | "name"
  | "policyType"
  | "prompt"
  | "sources"
  | "presidioEntities"
  | "customRuleIds"
>;

export type PolicySummary = {
  kind: "prompt" | "categories";
  text: string;
};

/** Crude plural fold so "Destructive Tools" matches "Destructive Tool Flagger".
 *  Both sides of every comparison run through it, so the odd mangled token
 *  ("ops" -> "op") still lines up. */
function singularize(token: string): string {
  return token.length > 2 && token.endsWith("s") ? token.slice(0, -1) : token;
}

function tokenize(value: string): string[] {
  return value
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, " ")
    .split(" ")
    .filter((token) => token.length > 0)
    .map(singularize);
}

function labelIsCoveredByName(label: string, nameTokens: Set<string>): boolean {
  const tokens = tokenize(label).filter(
    (token) => !GENERIC_LABEL_WORDS.has(token),
  );
  return tokens.length > 0 && tokens.every((token) => nameTokens.has(token));
}

function categoryIsCoveredByName(
  category: RuleCategory,
  nameTokens: Set<string>,
): boolean {
  const alias = CATEGORY_LABEL_ALIASES[category];
  return (
    labelIsCoveredByName(RULE_CATEGORY_META[category].label, nameTokens) ||
    (alias !== undefined && labelIsCoveredByName(alias, nameTokens))
  );
}

/** True when `tokens` opens with the words in `prefix`. The final prefix word
 *  only has to start the word it lines up with, so an excerpt cut mid-word
 *  ("… destructive a") still counts. */
function tokensStartWith(tokens: string[], prefix: string[]): boolean {
  if (prefix.length === 0 || prefix.length > tokens.length) {
    return false;
  }
  return prefix.every((token, index) => {
    const candidate = tokens[index]!;
    if (candidate === token) {
      return true;
    }
    if (index !== prefix.length - 1 || !candidate.startsWith(token)) {
      return false;
    }
    return (
      token.length >= MIN_PARTIAL_TOKEN_LENGTH ||
      prefix.length >= MIN_ANCHORING_TOKENS
    );
  });
}

/** The category list worth showing next to `name`, or null when the name
 *  already names every category the policy detects. Policy names are
 *  auto-generated from the detectors ("Secrets Exposure Flagger" for the
 *  Secrets category), so repeating the labels adds nothing to the row. */
export function categoriesSummaryForName(
  name: string,
  categories: RuleCategory[],
): string | null {
  if (categories.length === 0) {
    return null;
  }

  const nameTokens = new Set(tokenize(name));
  if (
    categories.every((category) =>
      categoryIsCoveredByName(category, nameTokens),
    )
  ) {
    return null;
  }

  return categories
    .map((category) => RULE_CATEGORY_META[category].label)
    .join(", ");
}

/** The guardrail prompt worth showing next to `name`, or null when the name is
 *  just an excerpt of that prompt. Prompt-policy names are summarized from the
 *  guardrail, so a name like "Flag only tool calls that perform" repeats the
 *  opening of the prompt it came from. */
export function promptSummaryForName(
  name: string,
  prompt: string,
): string | null {
  const trimmedPrompt = prompt.trim();
  if (trimmedPrompt === "") {
    return null;
  }

  let nameTokens = tokenize(name);
  if (tokensStartWith(nameTokens, DERIVED_PROMPT_NAME_PREFIX)) {
    nameTokens = nameTokens.slice(DERIVED_PROMPT_NAME_PREFIX.length);
  }

  return tokensStartWith(tokenize(trimmedPrompt), nameTokens)
    ? null
    : trimmedPrompt;
}

/** The categories a standard policy detects, in display order. */
function policyCategories(policy: PolicySummaryPolicy): RuleCategory[] {
  const categories = [
    ...policyToCategories(policy.sources, policy.presidioEntities),
  ];
  if (policy.customRuleIds?.length) {
    categories.push("custom");
  }
  return categories;
}

/** What a policy detects, for the second line of its row in the policy table:
 *  the guardrail prompt for prompt-based policies, the detection categories
 *  otherwise. Null when the policy name already conveys it — names are
 *  generated from these same inputs, so most rows would otherwise say the same
 *  thing twice. */
export function policySummary(
  policy: PolicySummaryPolicy,
): PolicySummary | null {
  if (policy.policyType === "prompt_based") {
    const prompt = promptSummaryForName(policy.name, policy.prompt ?? "");
    return prompt === null
      ? null
      : { kind: "prompt", text: prompt.replace(/\s+/g, " ") };
  }

  const categories = categoriesSummaryForName(
    policy.name,
    policyCategories(policy),
  );
  return categories === null ? null : { kind: "categories", text: categories };
}
