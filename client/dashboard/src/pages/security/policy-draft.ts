import type { RiskPreset } from "@gram/client/models/components/riskpreset.js";
import type { SuggestRiskPolicyResult } from "@gram/client/models/components/suggestriskpolicyresult.js";
import type { PolicyAction } from "./policy-data";

/** A prefilled starting point for the create wizard, carried in router state
 * from the intent screen. Mirrors the server's suggestion result so a preset
 * and a described draft feed the editors the same way. */
export type PolicyDraft = {
  presetId: string;
  policyType: "standard" | "prompt_based";
  name: string;
  action: PolicyAction;
  score: number;
  sources: string[];
  presidioEntities: string[];
  prompt: string;
  userMessage: string;
};

const ACTIONS: ReadonlySet<string> = new Set([
  "flag",
  "warn",
  "block",
  "quarantine",
]);

function toAction(value: string): PolicyAction {
  return ACTIONS.has(value) ? (value as PolicyAction) : "flag";
}

export function draftFromPreset(preset: RiskPreset): PolicyDraft {
  return {
    presetId: preset.id,
    policyType: preset.policyType,
    name: preset.label,
    action: toAction(preset.action),
    score: preset.score,
    sources: [...preset.sources],
    presidioEntities: [...preset.presidioEntities],
    prompt: preset.prompt,
    userMessage: preset.userMessage,
  };
}

export function draftFromSuggestion(
  suggestion: SuggestRiskPolicyResult,
): PolicyDraft {
  return {
    presetId: suggestion.presetId,
    policyType: suggestion.policyType,
    name: suggestion.name,
    action: toAction(suggestion.action),
    score: suggestion.score,
    sources: [...suggestion.sources],
    presidioEntities: [...suggestion.presidioEntities],
    prompt: suggestion.prompt,
    userMessage: suggestion.userMessage,
  };
}

/** The wizard kind a draft opens in. */
export function draftKind(draft: PolicyDraft): "standard" | "prompt" {
  return draft.policyType === "prompt_based" ? "prompt" : "standard";
}

/** Router state is untyped; accept only a shape the editors can seed from. */
export function readPolicyDraft(state: unknown): PolicyDraft | null {
  if (!state || typeof state !== "object") return null;
  const candidate = (state as { draft?: unknown }).draft;
  if (!candidate || typeof candidate !== "object") return null;
  const draft = candidate as Partial<PolicyDraft>;
  if (
    (draft.policyType !== "standard" && draft.policyType !== "prompt_based") ||
    typeof draft.name !== "string"
  ) {
    return null;
  }
  return {
    presetId: typeof draft.presetId === "string" ? draft.presetId : "",
    policyType: draft.policyType,
    name: draft.name,
    action: toAction(typeof draft.action === "string" ? draft.action : "flag"),
    score: typeof draft.score === "number" ? draft.score : 5,
    sources: Array.isArray(draft.sources) ? draft.sources : [],
    presidioEntities: Array.isArray(draft.presidioEntities)
      ? draft.presidioEntities
      : [],
    prompt: typeof draft.prompt === "string" ? draft.prompt : "",
    userMessage: typeof draft.userMessage === "string" ? draft.userMessage : "",
  };
}
