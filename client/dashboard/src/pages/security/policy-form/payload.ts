// Pure payload builders + constants for the policy create/edit form.
//
// Extracted from PolicyCenter.tsx (AGE-2704) so the create/edit wizard and the
// draft-eval candidate builder share one serialization and can never drift.
// This module is pure (no React, no page state) and is a leaf consumer of
// `policy-data`, `rule-ids`, and `policy-summary`.

import type { PolicyEvalCandidateConfig } from "@gram/client/models/components/policyevalcandidateconfig.js";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import {
  DETECTION_RULES,
  type PolicyMessageType,
  type RuleCategory,
} from "../policy-data";
import {
  ALL_POLICY_MESSAGE_TYPES,
  PRESIDIO_CATEGORIES,
  policyMessageTypesForForm,
  policyToCategories,
} from "../policy-display";
import { ruleIdToPresidioEntity } from "../rule-ids";

/** Categories that are currently available */
export const AVAILABLE_CATEGORIES: Set<RuleCategory> = new Set([
  "secrets",
  ...PRESIDIO_CATEGORIES,
  "shadow_mcp",
  "destructive_tool",
  "cli_destructive",
  "prompt_injection",
  "custom",
]);

/** All rule categories in display order */
export const ALL_CATEGORIES: RuleCategory[] = [
  "secrets",
  ...PRESIDIO_CATEGORIES,
  "shadow_mcp",
  "destructive_tool",
  "cli_destructive",
  "prompt_injection",
  "off_policy",
];

/** Categories whose source the server rejects with action=block; the form
 * must force flag when any of these are selected. Mirrors validateSourceAction
 * in server/internal/risk/impl.go. */
export const FLAG_ONLY_CATEGORIES: Set<RuleCategory> = new Set([
  "destructive_tool",
  "cli_destructive",
]);

/** Built-in detector categories that only produce findings when Speakeasy hooks
 *  are installed on the agent (no rule list to customize). */
export const HOOK_REQUIRED_CATEGORIES: Set<RuleCategory> = new Set([
  "shadow_mcp",
  "destructive_tool",
]);

/** Example scope CEL snippets offered beneath the include field — narrow a
 *  policy to a subset of messages. */
export const SCOPE_INCLUDE_CEL_EXAMPLES: { label: string; expr: string }[] = [
  {
    label: "Only a GitHub server",
    expr: 'tools.exists(t, t.server.matchExact("github"))',
  },
  {
    label: "Production prompts",
    expr: 'prompt.matchText("production")',
  },
  {
    label: "Delete-style tools",
    expr: 'tools.exists(t, t.function.matchGlob("*delete*"))',
  },
];

/** Example scope CEL snippets offered beneath the exempt field — take matching
 *  messages out of the policy entirely (an allowlist). */
export const SCOPE_EXEMPT_CEL_EXAMPLES: { label: string; expr: string }[] = [
  {
    label: "Read-only tools",
    expr: 'tools.exists(t, t.function.matchGlob("*get*") || t.function.matchGlob("*list*"))',
  },
  {
    label: "A safelisted server",
    expr: 'tools.exists(t, t.server.matchExact("internal-docs"))',
  },
];

export type PolicyKind = "risk" | "prompt";
export type PolicyAudienceType = "everyone" | "targeted";
export type PolicyAudienceChoice = "everyone" | "users" | "roles";

// DEFAULT_JUDGE_TEMPERATURE mirrors riskjudge.defaultJudgeTemperature: the
// benchmark ran at 0 (deterministic verdicts), which is the effective default.
export const DEFAULT_JUDGE_TEMPERATURE = 0;

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
): {
  sources: string[];
  presidioEntities: string[];
  promptInjectionRules: string[];
  disabledRules: string[];
} {
  const sources: string[] = [];
  const presidioEntities: string[] = [];
  const promptInjectionRules: string[] = [];

  if (cats.has("secrets")) sources.push("gitleaks");
  if (cats.has("shadow_mcp")) sources.push("shadow_mcp");
  if (cats.has("destructive_tool")) sources.push("destructive_tool");
  if (cats.has("cli_destructive")) sources.push("cli_destructive");
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

export function policyMessageTypesForPayload(
  selectedMessageTypes: Set<PolicyMessageType>,
): PolicyMessageType[] {
  const orderedTypes = ALL_POLICY_MESSAGE_TYPES.filter((type) =>
    selectedMessageTypes.has(type),
  );
  if (orderedTypes.length === ALL_POLICY_MESSAGE_TYPES.length) {
    return [];
  }
  return orderedTypes;
}

export function policyAudienceChoiceForSelection(
  audienceType: PolicyAudienceType,
  principalUrns: Set<string>,
): PolicyAudienceChoice {
  if (audienceType === "everyone") {
    return "everyone";
  }

  const hasUser = [...principalUrns].some((urn) => urn.startsWith("user:"));
  if (hasUser) {
    return "users";
  }

  const hasRole = [...principalUrns].some((urn) => urn.startsWith("role:"));
  return hasRole ? "roles" : "users";
}

export function filterAudiencePrincipalsForChoice(
  principalUrns: Set<string>,
  choice: PolicyAudienceChoice,
): Set<string> {
  if (choice === "everyone") {
    return new Set<string>();
  }

  const prefix = choice === "users" ? "user:" : "role:";
  return new Set([...principalUrns].filter((urn) => urn.startsWith(prefix)));
}

export function isPromptPolicy(policy: RiskPolicy): boolean {
  return policy.policyType === "prompt_based";
}

export function promptPolicyName(prompt: string): string {
  return prompt.trim().replace(/\s+/g, " ").slice(0, 60) || "Prompt Policy";
}

/** The author-settable form fields that define a policy's detection behavior.
 * `serializeConfig` reads these to produce the wire detection fields shared by
 * both the save payload and the draft-eval candidate, so the two cannot drift. */
export type SerializableConfigInput = {
  formPolicyKind: PolicyKind;
  formPromptInstruction: string;
  selectedCategories: Set<RuleCategory>;
  disabledRules: Set<string>;
  selectedCustomRuleIds: Set<string>;
  selectedMessageTypes: Set<PolicyMessageType>;
  scopeMode: "messageTypes" | "cel";
  scopeInclude: string;
  scopeExempt: string;
  // Prompt-policy judge config.
  formModel: string;
  formTemperature: number;
  formFailOpen: boolean;
  // Carried so an edit round-trips a pinned hidden presidio entity, and so a
  // model_config that already existed keeps being emitted.
  editingPolicy: RiskPolicy | null;
};

/** Detection fields shared by the save payload and the eval candidate. Returns
 * canonical wire-shape arrays (sources, presidio entities, etc.) for risk
 * policies, and the prompt + judge config for prompt policies. Enforcement-only
 * fields (action/audience/userMessage/name/enabled) are NOT included here — eval
 * ignores them and the save path layers them on separately. */
export function serializeConfig(state: SerializableConfigInput): {
  policyType: "standard" | "prompt_based";
  sources: string[];
  presidioEntities: string[];
  promptInjectionRules: string[];
  disabledRules: string[];
  customRuleIds: string[];
  messageTypes: PolicyMessageType[];
  scopeInclude: string;
  scopeExempt: string;
  prompt: string;
  modelConfig:
    | { model?: string; temperature?: number; failOpen: boolean }
    | undefined;
} {
  // Fine-grained scope predicates (both kinds). The include applies only in CEL
  // scope mode; the exempt is additive and always considered.
  const scopeInclude =
    state.scopeMode === "cel" ? state.scopeInclude.trim() : "";
  const scopeExempt = state.scopeExempt.trim();
  const messageTypes = policyMessageTypesForPayload(state.selectedMessageTypes);

  if (state.formPolicyKind === "prompt") {
    const temperatureIsCustom =
      state.formTemperature !== DEFAULT_JUDGE_TEMPERATURE;
    const hasModelConfig =
      !!state.editingPolicy?.modelConfig ||
      !!state.formModel ||
      temperatureIsCustom ||
      !state.formFailOpen;
    const modelConfig = hasModelConfig
      ? {
          ...(state.formModel ? { model: state.formModel } : {}),
          ...(temperatureIsCustom
            ? { temperature: state.formTemperature }
            : {}),
          failOpen: state.formFailOpen,
        }
      : undefined;
    return {
      policyType: "prompt_based",
      sources: [],
      presidioEntities: [],
      promptInjectionRules: [],
      disabledRules: [],
      customRuleIds: [],
      messageTypes,
      scopeInclude,
      scopeExempt,
      prompt: state.formPromptInstruction.trim(),
      modelConfig,
    };
  }

  const {
    sources,
    presidioEntities,
    promptInjectionRules,
    disabledRules: payloadDisabled,
  } = categoriesToPayload(
    state.selectedCategories,
    state.disabledRules,
    pinnedHiddenRuleIds(
      state.editingPolicy ? state.editingPolicy.presidioEntities : undefined,
    ),
  );

  return {
    policyType: "standard",
    sources,
    presidioEntities,
    promptInjectionRules,
    disabledRules: payloadDisabled,
    customRuleIds: [...state.selectedCustomRuleIds],
    messageTypes,
    scopeInclude,
    scopeExempt,
    prompt: "",
    modelConfig: undefined,
  };
}

/** Build the draft-eval `candidate` from the current form. Reuses
 * `serializeConfig`, so the candidate is complete by construction for every
 * author-settable detection field. Empty arrays/strings are omitted to keep the
 * normalized dirty-check comparison stable (see `candidateFromPolicy`). */
export function buildCandidatePayload(
  state: SerializableConfigInput,
): PolicyEvalCandidateConfig {
  const cfg = serializeConfig(state);
  return normalizeCandidate({
    policyType: cfg.policyType,
    sources: cfg.sources,
    presidioEntities: cfg.presidioEntities,
    disabledRules: cfg.disabledRules,
    customRuleIds: cfg.customRuleIds,
    messageTypes: cfg.messageTypes,
    scopeInclude: cfg.scopeInclude,
    scopeExempt: cfg.scopeExempt,
    prompt: cfg.prompt,
    modelConfig: cfg.modelConfig,
  });
}

/** Build a candidate from a saved policy, in the same normalized shape as
 * `buildCandidatePayload`, so the two can be deep-compared for the dirty check.
 * Reconstructs the form's category/disabled-rule view of the policy first so
 * the serialization matches what an unedited form would emit. */
export function candidateFromPolicy(
  policy: RiskPolicy,
): PolicyEvalCandidateConfig {
  const isPrompt = isPromptPolicy(policy);
  // Must mirror serializeConfig exactly: it collapses "all types" to [] via
  // policyMessageTypesForPayload. Using the full expansion here made every
  // default-scope policy compare unequal -> falsely "dirty" -> always evaluated
  // as a candidate, so runs were never bound to the policy or listed.
  const messageTypes = policyMessageTypesForPayload(
    policyMessageTypesForForm(policy.messageTypes),
  );
  if (isPrompt) {
    const temperature = policy.modelConfig?.temperature;
    const model = policy.modelConfig?.model ?? "";
    const failOpen = policy.modelConfig?.failOpen ?? true;
    const temperatureIsCustom =
      temperature != null && temperature !== DEFAULT_JUDGE_TEMPERATURE;
    const hasModelConfig =
      !!policy.modelConfig || !!model || temperatureIsCustom || !failOpen;
    const modelConfig = hasModelConfig
      ? {
          ...(model ? { model } : {}),
          ...(temperatureIsCustom ? { temperature } : {}),
          failOpen,
        }
      : undefined;
    return normalizeCandidate({
      policyType: "prompt_based",
      messageTypes,
      scopeInclude: policy.scopeInclude?.trim() ?? "",
      scopeExempt: policy.scopeExempt?.trim() ?? "",
      prompt: policy.prompt?.trim() ?? "",
      modelConfig,
    });
  }

  const categories = policyToCategories(
    policy.sources,
    policy.presidioEntities,
  );
  const customRuleIds = policy.customRuleIds ?? [];
  if (customRuleIds.length > 0) {
    categories.add("custom");
  }
  const disabledRules = new Set(policy.disabledRules ?? []);
  const {
    sources,
    presidioEntities,
    disabledRules: payloadDisabled,
  } = categoriesToPayload(
    categories,
    disabledRules,
    pinnedHiddenRuleIds(policy.presidioEntities),
  );
  return normalizeCandidate({
    policyType: "standard",
    sources,
    presidioEntities,
    disabledRules: payloadDisabled,
    customRuleIds: [...customRuleIds],
    messageTypes,
    scopeInclude: policy.scopeInclude?.trim() ?? "",
    scopeExempt: policy.scopeExempt?.trim() ?? "",
  });
}

/** Normalize a candidate into a canonical shape for deep-equal comparison:
 * sort arrays, drop empty arrays/strings, drop undefined model_config fields.
 * Order-insensitive so a re-ordered selection isn't a false dirty. */
function normalizeCandidate(c: {
  policyType: "standard" | "prompt_based";
  sources?: string[];
  presidioEntities?: string[];
  disabledRules?: string[];
  customRuleIds?: string[];
  messageTypes?: string[];
  scopeInclude?: string;
  scopeExempt?: string;
  prompt?: string;
  modelConfig?:
    | { model?: string; temperature?: number; failOpen: boolean }
    | undefined;
}): PolicyEvalCandidateConfig {
  const out: PolicyEvalCandidateConfig = { policyType: c.policyType };
  const sortedNonEmpty = (a?: string[]) =>
    a && a.length > 0 ? [...a].sort() : undefined;
  const sources = sortedNonEmpty(c.sources);
  if (sources) out.sources = sources;
  const presidioEntities = sortedNonEmpty(c.presidioEntities);
  if (presidioEntities) out.presidioEntities = presidioEntities;
  const disabledRules = sortedNonEmpty(c.disabledRules);
  if (disabledRules) out.disabledRules = disabledRules;
  const customRuleIds = sortedNonEmpty(c.customRuleIds);
  if (customRuleIds) out.customRuleIds = customRuleIds;
  const messageTypes = sortedNonEmpty(c.messageTypes);
  if (messageTypes) out.messageTypes = messageTypes;
  if (c.scopeInclude && c.scopeInclude.trim() !== "")
    out.scopeInclude = c.scopeInclude;
  if (c.scopeExempt && c.scopeExempt.trim() !== "")
    out.scopeExempt = c.scopeExempt;
  if (c.prompt && c.prompt.trim() !== "") out.prompt = c.prompt;
  if (c.modelConfig) {
    out.modelConfig = {
      ...(c.modelConfig.model ? { model: c.modelConfig.model } : {}),
      ...(c.modelConfig.temperature != null
        ? { temperature: c.modelConfig.temperature }
        : {}),
      failOpen: c.modelConfig.failOpen,
    };
  }
  return out;
}

/** Stable deep-equal over the normalized candidates: a saved policy is "dirty"
 * when the candidate built from the current form differs from the candidate
 * built from the loaded policy. Both inputs must already be normalized. */
export function candidatesEqual(
  a: PolicyEvalCandidateConfig,
  b: PolicyEvalCandidateConfig,
): boolean {
  return stableStringify(a) === stableStringify(b);
}

function stableStringify(value: unknown): string {
  if (value === null || typeof value !== "object") {
    return JSON.stringify(value);
  }
  if (Array.isArray(value)) {
    return `[${value.map(stableStringify).join(",")}]`;
  }
  const obj = value as Record<string, unknown>;
  const keys = Object.keys(obj).sort();
  return `{${keys
    .map((k) => `${JSON.stringify(k)}:${stableStringify(obj[k])}`)
    .join(",")}}`;
}
