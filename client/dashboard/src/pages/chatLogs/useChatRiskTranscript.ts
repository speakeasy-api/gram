import { useCallback, useEffect, useState } from "react";
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

  // Re-seed whenever a fresh base response arrives (chat switch or refetch).
  // User expansions are intentionally reset on refetch.
  useEffect(() => {
    if (base.data) setState(buildInitial(base.data));
  }, [base.data]);

  const loadBefore = useCallback(() => {
    if (!state || loadingKey || !state.hasMoreBefore) return;
    const oldest = state.messages[0];
    if (!oldest) return;
    setLoadingKey("before");
    void client.chat
      .load({ id: chatId, beforeSeq: oldest.seq, limit: TRANSCRIPT_PAGE_SIZE })
      .then((page) =>
        setState((prev) =>
          prev
            ? {
                ...prev,
                messages: mergeSorted(page.messages, prev.messages),
                hasMoreBefore: page.hasMoreBefore,
              }
            : prev,
        ),
      )
      .finally(() => setLoadingKey(null));
  }, [client, chatId, state, loadingKey]);

  const loadAfter = useCallback(() => {
    if (!state || loadingKey || !state.hasMoreAfter) return;
    const newest = state.messages[state.messages.length - 1];
    if (!newest) return;
    setLoadingKey("after");
    void client.chat
      .load({ id: chatId, afterSeq: newest.seq, limit: TRANSCRIPT_PAGE_SIZE })
      .then((page) =>
        setState((prev) =>
          prev
            ? {
                ...prev,
                messages: mergeSorted(prev.messages, page.messages),
                hasMoreAfter: page.hasMoreAfter,
              }
            : prev,
        ),
      )
      .finally(() => setLoadingKey(null));
  }, [client, chatId, state, loadingKey]);

  const loadGap = useCallback(
    (afterSeq: number) => {
      if (!state || loadingKey) return;
      setLoadingKey(`gap:${afterSeq}`);
      void client.chat
        .load({ id: chatId, afterSeq, limit: TRANSCRIPT_PAGE_SIZE })
        .then((page) =>
          setState((prev) => {
            if (!prev) return prev;
            // The message immediately after the gap, by seq.
            const nextSeq = prev.messages
              .map((m) => m.seq)
              .filter((s) => s > afterSeq)
              .sort((x, y) => x - y)[0];
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
          }),
        )
        .finally(() => setLoadingKey(null));
    },
    [client, chatId, state, loadingKey],
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
    isError: base.isError,
  };
}
