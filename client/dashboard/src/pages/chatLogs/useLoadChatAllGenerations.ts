import { useEffect, useMemo, useState } from "react";
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
// generations with up to three concurrent in-flight requests. Server returns
// one generation per request to keep query cost bounded for long chats; the
// window of queries we materialize grows as earlier ones settle so a chat with
// many generations never pre-registers them all in React Query at once.
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

  const [windowSize, setWindowSize] = useState(0);

  useEffect(() => {
    setWindowSize(0);
  }, [chatId]);

  useEffect(() => {
    if (latest && windowSize === 0 && maxGeneration > 0) {
      setWindowSize(Math.min(PARALLELISM, maxGeneration));
    }
  }, [latest, windowSize, maxGeneration]);

  const generationsToLoad = useMemo(() => {
    if (!latest) return [];
    return Array.from({ length: windowSize }, (_, i) => maxGeneration - 1 - i);
  }, [latest, maxGeneration, windowSize]);

  const queries = useQueries({
    queries: generationsToLoad.map((generation) => ({
      ...buildLoadChatQuery(client, { id: chatId, generation }),
    })),
  });

  const successCount = queries.filter((q) => q.isSuccess).length;
  const hasErrors = queries.some((q) => q.isError);

  useEffect(() => {
    if (!latest || windowSize === 0 || windowSize >= maxGeneration) return;
    if (hasErrors) return;
    const desired = Math.min(successCount + PARALLELISM, maxGeneration);
    if (desired > windowSize) {
      setWindowSize(desired);
    }
  }, [latest, successCount, windowSize, maxGeneration, hasErrors]);

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

  const isFullyLoaded = !!latest && successCount === maxGeneration;

  return {
    chat: latest,
    messages,
    isLoading: latestLoading,
    isFullyLoaded,
    hasErrors,
  };
}
