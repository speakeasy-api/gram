import { Assistant } from "@gram/client/models/components/assistant.js";
import { createContext, useContext } from "react";

type PendingResolver = (result: unknown) => void;

export type AssistantEnv = { id: string; slug: string };

export type DraftContextValue = {
  assistantId: string | null;
  setAssistantId: (id: string) => void;
  setAssistant: (assistant: Assistant) => void;
  assistant: Assistant | undefined;
  refetchAssistant: () => Promise<unknown>;
  invalidateAll: () => void;
  registerPending: (toolCallId: string, resolver: PendingResolver) => void;
  resolvePending: (toolCallId: string, result: unknown) => boolean;
  assistantEnv: AssistantEnv | null;
  setAssistantEnv: (env: AssistantEnv | null) => void;
};

export type { PendingResolver };

export const AssistantDraftCtx = createContext<DraftContextValue | null>(null);

export function useAssistantDraft(): DraftContextValue {
  const v = useContext(AssistantDraftCtx);
  if (!v) {
    throw new Error(
      "useAssistantDraft must be used inside an AssistantDraftProvider",
    );
  }
  return v;
}
