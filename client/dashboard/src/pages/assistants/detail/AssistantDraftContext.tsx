import type { Assistant } from "@gram/client/models/components/assistant.js";
import {
  invalidateAllAssistantsGet,
  queryKeyAssistantsGet,
  useAssistantsGet,
} from "@gram/client/react-query/assistantsGet.js";
import { invalidateAllAssistantsList } from "@gram/client/react-query/assistantsList.js";
import { invalidateAllListEnvironments } from "@gram/client/react-query/listEnvironments.js";
import { invalidateAllListToolsets } from "@gram/client/react-query/listToolsets.js";
import { invalidateAllTrigger } from "@gram/client/react-query/trigger.js";
import { invalidateAllTriggers } from "@gram/client/react-query/triggers.js";
import { invalidateAllSkill } from "@gram/client/react-query/skill.js";
import { useQueryClient } from "@tanstack/react-query";
import { useProject } from "@/contexts/Auth";
import type { ReactNode } from "react";
import { useCallback, useMemo } from "react";
import type { DraftContextValue } from "./useAssistantDraft";
import { AssistantDraftCtx } from "./useAssistantDraft";

export function AssistantDraftProvider({
  initialAssistantId,
  children,
}: {
  initialAssistantId: string;
  children: ReactNode;
}): JSX.Element {
  const queryClient = useQueryClient();
  const project = useProject();
  const assistantId = initialAssistantId;

  const { data: assistant, refetch } = useAssistantsGet(
    { id: assistantId, gramProject: project.slug },
    undefined,
    {
      enabled: !!assistantId,
      retry: false,
      throwOnError: false,
      refetchOnWindowFocus: false,
    },
  );

  const setAssistant = useCallback(
    (next: Assistant) => {
      queryClient.setQueryData(
        queryKeyAssistantsGet({ id: next.id, gramProject: project.slug }),
        next,
      );
    },
    [queryClient, project.slug],
  );

  const invalidateAll = useCallback(() => {
    void invalidateAllAssistantsGet(queryClient);
    void invalidateAllAssistantsList(queryClient);
    void invalidateAllListEnvironments(queryClient);
    void invalidateAllListToolsets(queryClient);
    void invalidateAllTriggers(queryClient);
    void invalidateAllTrigger(queryClient);
  }, [queryClient]);

  const invalidateSkillAttachments = useCallback(() => {
    void invalidateAllAssistantsList(queryClient);
    void invalidateAllSkill(queryClient);
  }, [queryClient]);

  const value = useMemo<DraftContextValue>(
    () => ({
      assistantId,
      setAssistant,
      assistant,
      refetchAssistant: refetch,
      invalidateAll,
      invalidateSkillAttachments,
    }),
    [
      assistantId,
      setAssistant,
      assistant,
      refetch,
      invalidateAll,
      invalidateSkillAttachments,
    ],
  );

  return (
    <AssistantDraftCtx.Provider value={value}>
      {children}
    </AssistantDraftCtx.Provider>
  );
}
