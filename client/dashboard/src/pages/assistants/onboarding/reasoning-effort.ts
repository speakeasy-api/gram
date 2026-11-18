import type { ReasoningEffort as CreateReasoningEffort } from "@gram/client/models/components/createassistantform.js";

export type ReasoningEffort = CreateReasoningEffort;

export function reasoningEffortOrDefault(
  value: string | undefined,
): ReasoningEffort {
  return value === "none" || value === "low" || value === "high"
    ? value
    : "medium";
}

export const REASONING_EFFORT_LABELS = {
  none: "Off",
  low: "Low",
  medium: "Medium",
  high: "High",
} satisfies Record<ReasoningEffort, string>;

export const REASONING_EFFORT_HELP =
  "Controls how much reasoning the model uses. Off is only supported by models that permit disabling reasoning. Unsupported models may reject requests.";

export const KIT_REASONING_EFFORT_HELP =
  "Kit runtime supports Low, Medium, and High reasoning effort only. Off requires the Default runtime and a model that permits disabling reasoning.";
