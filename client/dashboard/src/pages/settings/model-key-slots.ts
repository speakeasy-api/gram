import type { ModelProviderKey } from "@gram/client/models/components/modelproviderkey.js";

const PROJECT_DEFAULT_SLOT = "default";
export const MODEL_KEY_PROVIDER = "openrouter";

export type ModelKeySlot = {
  slot: string;
  name: string;
  description: string;
};

// The completion surfaces a customer key can cover. The 'default' slot covers
// every other slot that has no dedicated override; the server rejects slots
// outside this list.
export const MODEL_KEY_SLOTS: ModelKeySlot[] = [
  {
    slot: PROJECT_DEFAULT_SLOT,
    name: "Project default",
    description: "Covers every surface below without a dedicated key.",
  },
  {
    slot: "playground",
    name: "Playground",
    description: "Completions from the dashboard playground.",
  },
  {
    slot: "assistants",
    name: "Assistants",
    description: "Completions from assistant runs and triggers.",
  },
  {
    slot: "risk-policy",
    name: "Risk policy judge",
    description:
      "Prompt-based risk policy evaluations of observed agent traffic.",
  },
  {
    slot: "prompt-injection",
    name: "Prompt injection classifier",
    description: "Prompt injection scanning of observed agent traffic.",
  },
];

export type KeySource = "custom" | "inherited" | "platform";

// Mirrors the server's resolution order: an enabled slot override wins, then
// an enabled project-default key, then the platform-provisioned key.
export function keySourceForSlot(
  slot: string,
  keysBySlot: Map<string, ModelProviderKey>,
): KeySource {
  const own = keysBySlot.get(slot);
  if (own?.enabled) return "custom";
  if (
    slot !== PROJECT_DEFAULT_SLOT &&
    keysBySlot.get(PROJECT_DEFAULT_SLOT)?.enabled
  ) {
    return "inherited";
  }
  return "platform";
}
