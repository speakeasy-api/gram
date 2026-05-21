import { useMemo, useState } from "react";
import { useQueries } from "@tanstack/react-query";
import type { Chat, ChatMessage } from "@gram/client/models/components";
import {
  buildLoadChatQuery,
  useGramContext,
  useLoadChat,
} from "@gram/client/react-query";

const PARALLELISM = 3;

export interface LoadChatAllGenerationsResult {
  chat: Chat | undefined;
  messages: ChatMessage[];
  isLoading: boolean;
  isFullyLoaded: boolean;
  hasErrors: boolean;
}

// Loads a chat by paging across generations: latest first, then older
// generations with up to three concurrent in-flight requests. The window of
// queries we materialize grows as earlier ones settle, so a chat with many
// generations never pre-registers them all in React Query at once.
export function useLoadChatAllGenerations(
  chatId: string,
): LoadChatAllGenerationsResult {
  const client = useGramContext();
  const { data: latest, isLoading: latestLoading } = useLoadChat(
    { id: chatId },
    undefined,
    {},
  );

  const maxGeneration = latest?.maxGeneration ?? 0;

  const [trackedChatId, setTrackedChatId] = useState(chatId);
  const [windowSize, setWindowSize] = useState(0);

  if (trackedChatId !== chatId) {
    setTrackedChatId(chatId);
    setWindowSize(0);
  }

  const queries = useQueries({
    queries: Array.from({ length: windowSize }, (_, i) => ({
      ...buildLoadChatQuery(client, {
        id: chatId,
        generation: maxGeneration - 1 - i,
      }),
    })),
  });

  const successCount = queries.filter((q) => q.isSuccess).length;
  const hasErrors = queries.some((q) => q.isError);

  const desiredWindow = hasErrors
    ? windowSize
    : Math.min(successCount + PARALLELISM, maxGeneration);
  if (desiredWindow > windowSize) {
    setWindowSize(desiredWindow);
  }

  const messages = useMemo(() => {
    if (!latest) return [];
    const merged: ChatMessage[] = [];
    for (let i = queries.length - 1; i >= 0; i--) {
      const data = queries[i]?.data;
      if (data) merged.push(...data.messages);
    }
    merged.push(...latest.messages);
    return merged;
  }, [latest, queries]);

  return {
    chat: latest,
    messages,
    isLoading: latestLoading,
    isFullyLoaded: !!latest && successCount === maxGeneration,
    hasErrors,
  };
}
