import type { Assistant } from "@gram/client/models/components/assistant.js";
import { createContext, useContext } from "react";

export type DraftContextValue = {
  assistantId: string | null;
  setAssistant: (assistant: Assistant) => void;
  assistant: Assistant | undefined;
  refetchAssistant: () => Promise<unknown>;
  invalidateAll: () => void;
  invalidateSkillAttachments: () => void;
};

export const AssistantDraftCtx = createContext<DraftContextValue | null>(null);

/** The assistant the detail page shows, and how its sections refresh it. */
export function useAssistantDraft(): DraftContextValue {
  const v = useContext(AssistantDraftCtx);
  if (!v) {
    throw new Error(
      "useAssistantDraft must be used inside an AssistantDraftProvider",
    );
  }
  return v;
}
