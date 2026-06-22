import { useCallback, useEffect, useRef, useState } from "react";
import type { Chat, ChatMessage } from "@gram/client/models/components";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { useLoadChat } from "@gram/client/react-query";
import { useSdkClient } from "@/contexts/Sdk";
import { TRANSCRIPT_PAGE_SIZE } from "./useChatTranscript";

// Initial risk window is generous so most findings + context arrive in one
// request; gaps between disjoint findings are expanded on demand.
const RISK_INITIAL_LIMIT = 200;

export type RiskLoadKey = "before" | "after" | `gap:${number}`;

export interface ChatRiskTranscript {
  chat: Chat | undefined;
  messages: ChatMessage[];
  /** seqs after which an un-loaded gap remains, between disjoint risk windows. */
  gaps: Set<number>;
  hasMoreBefore: boolean;
  hasMoreAfter: boolean;
  loadBefore: () => void;
  loadAfter: () => void;
  loadGap: (afterSeq: number) => void;
  loadingKey: RiskLoadKey | null;
  isLoading: boolean;
  isError: boolean;
}

interface RiskState {
  messages: ChatMessage[];
  gaps: Set<number>;
  hasMoreBefore: boolean;
  hasMoreAfter: boolean;
}

// mergeSorted unions two message lists, dedupes by seq, and keeps ascending order.
function mergeSorted(a: ChatMessage[], b: ChatMessage[]): ChatMessage[] {
  const bySeq = new Map<number, ChatMessage>();
  for (const m of a) bySeq.set(m.seq, m);
  for (const m of b) bySeq.set(m.seq, m);
  return Array.from(bySeq.values()).sort((x, y) => x.seq - y.seq);
}

// buildInitial seeds state from a risk_only response: the flat windowed message
// list plus one gap marker per boundary between disjoint segments.
function buildInitial(chat: Chat): RiskState {
  const segments = chat.riskSegments ?? [];
  const gaps = new Set<number>();
  for (let i = 0; i < segments.length - 1; i++) {
    gaps.add(segments[i]!.lastSeq);
  }
  return {
    messages: chat.messages,
    gaps,
    hasMoreBefore: segments[0]?.hasMoreBefore ?? false,
    hasMoreAfter: segments[segments.length - 1]?.hasMoreAfter ?? false,
  };
}

// useChatRiskTranscript loads only risk findings plus surrounding context, then
// lets the user expand the edges and fill the gaps between disjoint findings.
export function useChatRiskTranscript(
  chatId: string,
  enabled: boolean,
): ChatRiskTranscript {
  const client = useSdkClient();
  const base = useLoadChat(
    { id: chatId, riskOnly: true, limit: RISK_INITIAL_LIMIT },
    undefined,
    {
      enabled,
      throwOnError: (error) =>
        !(error instanceof GramError && error.statusCode === 404),
    },
  );

  const [state, setState] = useState<RiskState | null>(null);
  const [loadingKey, setLoadingKey] = useState<RiskLoadKey | null>(null);
  const [loadError, setLoadError] = useState(false);

  // Tracks the chat the hook currently represents so a slow in-flight page from
  // a previous chat can't apply its response (or error) after a switch.
  const chatIdRef = useRef(chatId);
  chatIdRef.current = chatId;

  // Re-seed whenever a fresh base response arrives (chat switch or refetch).
  // User expansions and any prior incremental-load error are reset.
  useEffect(() => {
    if (base.data) {
      setState(buildInitial(base.data));
      setLoadError(false);
    }
  }, [base.data]);

  // Runs one incremental page load: ignores the result if the chat changed
  // mid-flight, and records failures so they surface via isError instead of
  // silently no-opping.
  const runIncrementalLoad = useCallback(
    (
      key: RiskLoadKey,
      request: { beforeSeq?: number; afterSeq?: number },
      apply: (prev: RiskState, page: Chat) => RiskState,
    ) => {
      if (loadingKey) return;
      const requestChatId = chatId;
      setLoadingKey(key);
      void client.chat
        .load({ id: chatId, limit: TRANSCRIPT_PAGE_SIZE, ...request })
        .then((page) => {
          if (chatIdRef.current !== requestChatId) return; // chat switched
          setState((prev) => (prev ? apply(prev, page) : prev));
        })
        .catch(() => {
          if (chatIdRef.current === requestChatId) setLoadError(true);
        })
        .finally(() => {
          if (chatIdRef.current === requestChatId) setLoadingKey(null);
        });
    },
    [client, chatId, loadingKey],
  );

  const loadBefore = useCallback(() => {
    if (!state || !state.hasMoreBefore) return;
    const oldest = state.messages[0];
    if (!oldest) return;
    runIncrementalLoad("before", { beforeSeq: oldest.seq }, (prev, page) => ({
      ...prev,
      messages: mergeSorted(page.messages, prev.messages),
      hasMoreBefore: page.hasMoreBefore,
    }));
  }, [state, runIncrementalLoad]);

  const loadAfter = useCallback(() => {
    if (!state || !state.hasMoreAfter) return;
    const newest = state.messages[state.messages.length - 1];
    if (!newest) return;
    runIncrementalLoad("after", { afterSeq: newest.seq }, (prev, page) => ({
      ...prev,
      messages: mergeSorted(prev.messages, page.messages),
      hasMoreAfter: page.hasMoreAfter,
    }));
  }, [state, runIncrementalLoad]);

  const loadGap = useCallback(
    (afterSeq: number) => {
      if (!state) return;
      runIncrementalLoad(`gap:${afterSeq}`, { afterSeq }, (prev, page) => {
        // The message immediately after the gap, by seq (messages stay sorted
        // ascending via mergeSorted, so find() is enough).
        const nextSeq = prev.messages.find((m) => m.seq > afterSeq)?.seq;
        const maxLoaded = page.messages.reduce(
          (mx, m) => Math.max(mx, m.seq),
          afterSeq,
        );
        const gaps = new Set(prev.gaps);
        gaps.delete(afterSeq);
        // Gap stays open (advanced to the new edge) only if the page didn't
        // reach the next already-loaded message.
        if (nextSeq !== undefined && maxLoaded < nextSeq) {
          gaps.add(maxLoaded);
        }
        return {
          ...prev,
          messages: mergeSorted(prev.messages, page.messages),
          gaps,
        };
      });
    },
    [state, runIncrementalLoad],
  );

  return {
    chat: base.data,
    messages: state?.messages ?? [],
    gaps: state?.gaps ?? new Set(),
    hasMoreBefore: state?.hasMoreBefore ?? false,
    hasMoreAfter: state?.hasMoreAfter ?? false,
    loadBefore,
    loadAfter,
    loadGap,
    loadingKey,
    isLoading: base.isLoading,
    isError: base.isError || loadError,
  };
}
