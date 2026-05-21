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
}

// Loads a chat by paging across generations: latest first, then older
// generations with up to three concurrent in-flight requests. Server returns
// one generation per request to keep query cost bounded for long chats.
export function useLoadChatAllGenerations(
  chatId: string,
): LoadChatAllGenerationsResult {
  const client = useGramContext();
  const { data: latest, isLoading: latestLoading } = useLoadChat(
    { id: chatId },
    undefined,
    {},
  );

  const olderGenerations = useMemo(() => {
    if (!latest) return [];
    const maxGen = latest.maxGeneration;
    return Array.from({ length: maxGen }, (_, i) => maxGen - 1 - i);
  }, [latest]);

  const [enabledUpTo, setEnabledUpTo] = useState(0);

  useEffect(() => {
    setEnabledUpTo(0);
  }, [chatId]);

  const queries = useQueries({
    queries: olderGenerations.map((generation, idx) => ({
      ...buildLoadChatQuery(client, { id: chatId, generation }),
      enabled: idx < enabledUpTo,
    })),
  });

  const settledCount = queries.filter((q) => q.isSuccess || q.isError).length;

  useEffect(() => {
    if (!latest || olderGenerations.length === 0) return;
    const desired = Math.min(
      settledCount + PARALLELISM,
      olderGenerations.length,
    );
    if (desired > enabledUpTo) {
      setEnabledUpTo(desired);
    }
  }, [latest, settledCount, enabledUpTo, olderGenerations.length]);

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

  const isFullyLoaded =
    !!latest &&
    enabledUpTo === olderGenerations.length &&
    queries.every((q) => q.isSuccess || q.isError);

  return {
    chat: latest,
    messages,
    isLoading: latestLoading,
    isFullyLoaded,
  };
}
